package gameserver

import (
	"os"
	"testing"

	"rf4server/internal/proto"
)

// A previously saved 2024 configuration grants every listed bag content, not one random
// alternative. Real SQLite and the normal completion path catch partial grants,
// serving/pack confusion and duplicate rewards on reconnect.
func TestNewYear2024AllBagContents(t *testing.T) {
	srv, db := newStaticRigTestServer(t)
	defer db.Close()
	uid := newProbeSess(srv).Userid
	if _, err := db.EnsureProfile(t.Context(), uid); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile("testdata/newyear-2024-saved-rewards.json")
	if err != nil {
		t.Fatal(err)
	}
	setEventStageGifts(t, srv, db, string(raw))
	if got := srv.ensureEventStageGifts(t.Context(), uid); len(got) != 0 {
		t.Fatal("unearned rewards")
	}
	wants := map[int64][]map[string]int64{
		9: {
			{"lim_ny_pack_6": 1},
			{"sd_pack_20": 1},
			{"rot_porto_pack_6": 1},
			{"mono_15397": 1},
			{"spoon_lures_1725": 1, "bh_rjs_pack_5": 1},
			{"zdn_bottle_cogn5": 2, "gorilka": 2},
			{"tele_10231_6": 1, "coupon_chuchelo": 1},
		},
		10: {
			{"var_hny_box": 1},
			{"big_coffee_can": 50},
			{"fluoro_nys_pack": 1},
			{"float_ny_1": 3},
			{"bcr_5996": 1},
			{"rd_ice_vine": 2, "conv_5994": 1},
			{"barrel_honey": 50, "spin_5995": 1},
		},
	}
	totals := map[string]int64{}
	for _, branch := range []int64{9, 10} {
		for idx, items := range wants[branch] {
			stage := int64(idx + 1)
			completeQualificationTasks(t, db, uid, eventStageRuleIDs(branch, stage))
			srv.ensureEventStageGifts(t.Context(), uid)
			if _, ok, err := db.KVGet(t.Context(), eventStageGiftKey(uid, branch, stage)); err != nil || !ok {
				t.Fatalf("missing stage %d/%d: %v", branch, stage, err)
			}
			for id, qty := range items {
				totals[id] += qty
			}
			for id, targetStage := range map[string]int64{"bcr_5996": 5, "conv_5994": 6, "spin_5995": 7} {
				if branch == 9 || stage < targetStage {
					if n := giftBoxItemCount(t, srv, uid, id); n != 0 {
						t.Fatalf("premature limited reel %s at %d/%d", id, branch, stage)
					}
				}
			}
			if branch == 9 && stage < 7 {
				if n := giftBoxItemCount(t, srv, uid, "tele_10231_6"); n != 0 {
					t.Fatal("premature limited rod")
				}
			}
			if branch == 9 && stage == 1 {
				var daysGranted int
				if err := db.SQL.QueryRow(`SELECT COUNT(*) FROM txn WHERE uid=? AND kind='membership_reward' AND note='event_reward_days=3'`, uid).Scan(&daysGranted); err != nil || daysGranted != 1 {
					t.Fatalf("stage 1 should grant three membership days once: %d %v", daysGranted, err)
				}
			}
			for id, qty := range totals {
				// A line spool is an instance; its quantity field holds metres.
				rows, actual := countContainerItem(t, db, uid, id, "backpack")
				if id == "mono_15397" {
					actual = rows
				}
				if actual != qty {
					t.Fatalf("stage %d/%d %s: got %d want %d", branch, stage, id, actual, qty)
				}
			}
			if rows := srv.ensureEventStageGifts(t.Context(), uid); len(rows) != 0 {
				t.Fatal("duplicate rewards")
			}
		}
	}
	var memberships, bags int
	if err := db.SQL.QueryRow(`SELECT COUNT(*) FROM txn WHERE uid=? AND kind='membership_reward'`, uid).Scan(&memberships); err != nil || memberships != 1 {
		t.Fatalf("membership count=%d: %v", memberships, err)
	}
	if err := db.SQL.QueryRow(`SELECT COUNT(*) FROM item WHERE uid=? AND item_id LIKE 'gift_bag_ny24%'`, uid).Scan(&bags); err != nil || bags != 0 {
		t.Fatalf("unusable bags=%d: %v", bags, err)
	}
	for box, contents := range map[string]map[string]int64{
		"fluoro_nys_pack":  {"fluoro_15798": 1, "fluoro_15804": 1, "fluoro_15808": 1},
		"var_hny_box":      {"var_hny_can": 6},
		"sd_pack_20":       {"sd_shoc_bar": 20},
		"lim_ny_pack_6":    {"lb_bottle_lim_ny": 6},
		"rot_porto_pack_6": {"rot_bottle_porto": 12},
		"bh_rjs_pack_5":    {"jhead_20596": 1, "jhead_20597": 1, "jhead_20598": 1, "jhead_20599": 1, "jhead_20600": 1},
	} {
		rows := giftBoxRows(t, srv, uid, box)
		if len(rows) != 1 {
			t.Fatalf("box %s: %+v", box, rows)
		}
		c := giftBoxTestCtx(t, srv, uid, proto.Args524(proto.U64(0), proto.Guid(rows[0].guid)))
		if _, ok := hItemUse(c); !ok {
			t.Fatalf("cannot open %s", box)
		}
		hItemUse(c)
		if giftBoxItemCount(t, srv, uid, box) != 0 {
			t.Fatalf("box not consumed: %s", box)
		}
		for id, want := range contents {
			rows, qty := countContainerItem(t, db, uid, id, "backpack")
			if box == "fluoro_nys_pack" {
				qty = rows
			}
			if qty != want {
				t.Fatalf("%s -> %s: got %d want %d", box, id, qty, want)
			}
		}
	}
}
