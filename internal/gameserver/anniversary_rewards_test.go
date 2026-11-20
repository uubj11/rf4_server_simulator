package gameserver

import (
	"database/sql"
	"encoding/hex"
	"errors"
	"os"
	"reflect"
	"sync"
	"testing"
	"time"

	"rf4server/internal/proto"
)

func TestAnniversaryCompletionRewards(t *testing.T) {
	srv, db := newStaticRigTestServer(t)
	defer db.Close()
	uid := newProbeSess(srv).Userid
	if _, err := db.EnsureProfile(t.Context(), uid); err != nil {
		t.Fatal(err)
	}
	raw := `{"19":{"0":[{"item":"premium_3d","qty":1},{"item":"tele_10175_5_9","qty":1}],"1":[{"item":"pb_m_choc_pack10_g","qty":1}]}}`
	setEventStageGifts(t, srv, db, raw)
	completeQualificationTasks(t, db, uid, eventStageRuleIDs(19, 1))
	if got := srv.ensureEventStageGifts(t.Context(), uid); len(got) != 1 || got[0].ItemID != "pb_m_choc_pack10_g" {
		t.Fatalf("stage 1 gifts: %+v", got)
	}
	for stage := int64(2); stage <= 7; stage++ {
		completeQualificationTasks(t, db, uid, eventStageRuleIDs(19, stage))
	}
	// A previously granted cup must not prevent newly configured completion rewards.
	srv.ensureQualificationCups(t.Context(), uid)
	before := time.Now().Unix()
	gifts := srv.ensureEventStageGifts(t.Context(), uid)
	if len(gifts) != 1 || gifts[0].ItemID != "tele_10175_5_9" {
		t.Fatalf("completion gifts: %+v", gifts)
	}
	p, err := db.GetProfile(t.Context(), uid)
	if err != nil || p.Membership != 1 || p.MembershipExp < before+3*86400 || p.MembershipExp > time.Now().Unix()+3*86400 {
		t.Fatalf("three days not activated: %+v %v", p, err)
	}
	if giftBoxItemCount(t, srv, uid, "premium_3d") != 0 {
		t.Fatal("membership must not be an inert inventory item")
	}
	if got := srv.ensureEventStageGifts(t.Context(), uid); len(got) != 0 {
		t.Fatal("replayed rewards")
	}
	again, _ := db.GetProfile(t.Context(), uid)
	if again.MembershipExp != p.MembershipExp {
		t.Fatal("replayed membership")
	}
	if got := srv.ensureQualificationCups(t.Context(), uid); len(got) != 0 {
		t.Fatal("duplicated cup")
	}
	node := eventBranchNode(19, eventTaskCounts[19], 6, srv.eventStageGiftPlans())
	encoded, err := proto.EncodeValue(node, srv.Table())
	if err != nil {
		t.Fatal(err)
	}
	decoded, _, err := proto.DecodeValue(encoded, 0, srv.Table())
	if err != nil {
		t.Fatal(err)
	}
	var keys []string
	for _, v := range decoded.Fields[4].Val.Items {
		b, e := hex.DecodeString(v.Hex)
		if e != nil {
			t.Fatal(e)
		}
		keys = append(keys, string(b))
	}
	if !reflect.DeepEqual(keys, []string{"premium_3d", "tele_10175_5_9", "reward_bday26"}) {
		t.Fatalf("completion preview: %v", keys)
	}
}

func TestAnniversaryMembershipRenewalAndRollback(t *testing.T) {
	for _, tc := range []struct {
		name          string
		kind, expires int64
		rollback      bool
	}{
		{"new", 0, 0, false}, {"active", 1, 2000000000, false}, {"expired", 1, 1600000000, false}, {"permanent", 2, 0, false}, {"rollback", 1, 2000000000, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, db := newStaticRigTestServer(t)
			defer db.Close()
			uid := newProbeSess(srv).Userid
			if _, err := db.EnsureProfile(t.Context(), uid); err != nil {
				t.Fatal(err)
			}
			if _, err := db.SQL.Exec(`UPDATE profile SET membership=?,membership_exp=?,gold=123,silver=456 WHERE uid=?`, tc.kind, tc.expires, uid); err != nil {
				t.Fatal(err)
			}
			if tc.rollback {
				if _, err := db.SQL.Exec(`CREATE TRIGGER fail_event_item BEFORE INSERT ON item BEGIN SELECT RAISE(ABORT,'test failure'); END`); err != nil {
					t.Fatal(err)
				}
			}
			plan := &eventStageGiftPlan{Branch: 19, Stage: 0, Gifts: []eventStageGift{{ItemID: "premium_3d", Qty: 1}, {ItemID: "tele_10175_5_9", Qty: 1}}}
			before := time.Now().Unix()
			rows, _ := srv.grantEventStageGift(t.Context(), uid, plan)
			p, err := db.GetProfile(t.Context(), uid)
			if err != nil {
				t.Fatal(err)
			}
			if p.Gold != 123 || p.Silver != 456 {
				t.Fatal("reward charged the wallet")
			}
			var notifications, ledger int
			if err := db.SQL.QueryRow(`SELECT COUNT(*) FROM admin_queue`).Scan(&notifications); err != nil {
				t.Fatal(err)
			}
			if err := db.SQL.QueryRow(`SELECT COUNT(*) FROM txn WHERE uid=? AND kind='membership_reward'`, uid).Scan(&ledger); err != nil {
				t.Fatal(err)
			}
			_, marked, err := db.KVGet(t.Context(), eventStageGiftKey(uid, 19, 0))
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				t.Fatal(err)
			}
			if tc.rollback {
				if len(rows) != 0 || marked || p.Membership != tc.kind || p.MembershipExp != tc.expires || notifications != 0 || ledger != 0 {
					t.Fatalf("partial grant after rollback: %+v mark=%v rows=%d queue=%d ledger=%d", p, marked, len(rows), notifications, ledger)
				}
				if _, err := db.SQL.Exec(`DROP TRIGGER fail_event_item`); err != nil {
					t.Fatal(err)
				}
				if rows, _ := srv.grantEventStageGift(t.Context(), uid, plan); len(rows) != 1 {
					t.Fatal("retry failed")
				}
			} else {
				if len(rows) != 1 || !marked {
					t.Fatal("reward not committed")
				}
				if tc.kind == 2 {
					if p.Membership != 2 || p.MembershipExp != 0 || notifications != 0 || ledger != 0 {
						t.Fatal("permanent membership changed")
					}
				} else {
					base := before
					if tc.kind == 1 && tc.expires > base {
						base = tc.expires
					}
					if p.Membership != 1 || p.MembershipExp < base+3*86400 || p.MembershipExp > base+3*86400+2 || notifications != 1 || ledger != 1 {
						t.Fatalf("renewal: %+v queue=%d ledger=%d", p, notifications, ledger)
					}
				}
			}
			first, _ := db.GetProfile(t.Context(), uid)
			if rows, names := srv.grantEventStageGift(t.Context(), uid, plan); len(rows) != 0 || len(names) != 0 {
				t.Fatal("replayed grant")
			}
			after, _ := db.GetProfile(t.Context(), uid)
			if after.MembershipExp != first.MembershipExp {
				t.Fatal("replayed membership extension")
			}
		})
	}
}

func TestAnniversaryCompletedBranchConcurrentAndExpired(t *testing.T) {
	srv, db := newStaticRigTestServer(t)
	defer db.Close()
	uid := newProbeSess(srv).Userid
	if _, err := db.EnsureProfile(t.Context(), uid); err != nil {
		t.Fatal(err)
	}
	setEventStageGifts(t, srv, db, `{"19":{"0":[{"item":"premium_3d","qty":1},{"item":"tele_10175_5_9","qty":1},{"item":"reward_bday26","qty":1}]}}`)
	for stage := int64(1); stage <= 7; stage++ {
		completeQualificationTasks(t, db, uid, eventStageRuleIDs(19, stage))
	}
	if err := db.TunableSet(t.Context(), eventEndKey, "1", "test"); err != nil {
		t.Fatal(err)
	}
	srv.tunMu.Lock()
	srv.tunCache = nil
	srv.tunMu.Unlock()
	if rows := srv.ensureEventStageGifts(t.Context(), uid); len(rows) != 0 {
		t.Fatal("expired event granted rewards")
	}
	if _, ok, _ := db.KVGet(t.Context(), eventStageGiftKey(uid, 19, 0)); ok {
		t.Fatal("expired event marked claimed")
	}
	if err := db.TunableSet(t.Context(), eventEndKey, "253402300799", "test"); err != nil {
		t.Fatal(err)
	}
	srv.tunMu.Lock()
	srv.tunCache = nil
	srv.tunMu.Unlock()
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() { defer wg.Done(); srv.ensureEventStageGifts(t.Context(), uid) }()
	}
	wg.Wait()
	if rows, qty := countContainerItem(t, db, uid, "tele_10175_5_9", "backpack"); rows != 1 || qty != 1 {
		t.Fatalf("duplicate completion item: %d/%d", rows, qty)
	}
	if rows, _ := countContainerItem(t, db, uid, "reward_bday26", "backpack"); rows != 0 {
		t.Fatal("completion plan duplicated automatic cup in backpack")
	}
	var ledger int
	if err := db.SQL.QueryRow(`SELECT COUNT(*) FROM txn WHERE uid=? AND kind='membership_reward'`, uid).Scan(&ledger); err != nil || ledger != 1 {
		t.Fatalf("membership issued %d times: %v", ledger, err)
	}
}

func TestAnniversaryPresetAndGiftBoxes(t *testing.T) {
	srv, db := newStaticRigTestServer(t)
	defer db.Close()
	uid := newProbeSess(srv).Userid
	if _, err := db.EnsureProfile(t.Context(), uid); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile("../admin/web/event-reward-presets/anniversary-2026.json")
	if err != nil {
		t.Fatal(err)
	}
	setEventStageGifts(t, srv, db, string(raw))
	if plans := srv.eventStageGiftPlans(); len(plans) != 16 {
		t.Fatalf("expected 14 stages + 2 completions, got %d", len(plans))
	}
	for _, branch := range []int64{19, 20} {
		for stage := int64(1); stage <= 7; stage++ {
			completeQualificationTasks(t, db, uid, eventStageRuleIDs(branch, stage))
		}
	}
	srv.ensureEventStageGifts(t.Context(), uid)
	for _, id := range []string{"camp_lamp_03g", "spinreel_pelengas_2026", "tele_10175_5_9", "big_coffee_can_g", "ab_tea_can_g", "spin_10252_M70ML", "spin_6115_g"} {
		if rows, _ := countContainerItem(t, db, uid, id, "backpack"); rows != 1 {
			t.Fatalf("%s: %d rows", id, rows)
		}
	}
	for _, id := range []string{"big_coffee_can_g", "ab_tea_can_g"} {
		if _, qty := countContainerItem(t, db, uid, id, "backpack"); qty != 50 {
			t.Fatalf("one full can %s must contain 50 servings, got %d", id, qty)
		}
	}
	boxes := map[string]map[string]int64{
		"pb_m_choc_pack10_g": {"pb_m_choc_g": 10}, "pb_d_choc_pack10_g": {"pb_d_choc_g": 10},
		"lr_dr_pack_6g": {"lr_dr_g": 6}, "kvarta_pack6_g": {"kvarta_bot_g": 6}, "pm_medovuha_alcfree_pack6g": {"pm_medovuha_alcfree_g": 12},
		"pom_mono_box_g":        {"pom_mono_01": 1, "pom_mono_013": 1, "pom_mono_015": 1, "pom_mono_017": 1, "pom_mono_02": 1},
		"goldimp_12pack_g":      {"goldimp_bot_g": 24},
		"hm_spoon_box_g":        {"hm_spoon_ws_bra_hq": 1, "hm_spoon_ws_cop_hq": 1, "hm_spoon_ws_gsil_hq": 1, "hm_spoon_ws_bl_hq": 1, "hm_spoon_ws_or_r_hq": 1, "hm_spoon_ws_purp_hq": 1},
		"hvz_feeder_bell_box_g": {"hvz_feeder_bell_y": 1, "hvz_feeder_bell_g": 1, "hvz_feeder_bell_r": 1},
		"waggler_box_g":         {"hvz_waggler_yl": 1, "hvz_waggler_gl": 1, "hvz_waggler_pl": 1},
	}
	for box, contents := range boxes {
		rows := giftBoxRows(t, srv, uid, box)
		if len(rows) != 1 {
			t.Fatalf("%s: %+v", box, rows)
		}
		c := giftBoxTestCtx(t, srv, uid, proto.Args524(proto.U64(0), proto.Guid(rows[0].guid)))
		body, ok := hItemUse(c)
		if !ok || len(body) == 0 {
			t.Fatalf("cannot open %s", box)
		}
		hItemUse(c) // Reusing the consumed GUID must not grant the contents again.
		for id, want := range contents {
			_, qty := countContainerItem(t, db, uid, id, "backpack")
			if qty != want {
				t.Errorf("%s/%s: %d want %d", box, id, qty, want)
			}
		}
		if giftBoxItemCount(t, srv, uid, box) != 0 {
			t.Fatalf("unconsumed box %s", box)
		}
	}
}
