package gameserver

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"rf4server/internal/proto"
)

func TestHistoricalAnniversaryPresetsGrantStages(t *testing.T) {
	for _, tc := range []struct {
		year                     int
		branch                   int64
		rod, reel, wineBox, wine string
	}{
		{2024, 11, "tele_10174_8g", "spin_6020", "rd_red_vine_box", "rd_red_vine_2017"},
		{2025, 15, "tele_10175_5_8", "spin_5150_S8", "rd_red_vine_box_8", "rd_red_vine_8"},
	} {
		t.Run(fmt.Sprint(tc.year), func(t *testing.T) {
			srv, db := newStaticRigTestServer(t)
			defer db.Close()
			uid := newProbeSess(srv).Userid
			if _, err := db.EnsureProfile(t.Context(), uid); err != nil {
				t.Fatal(err)
			}
			raw, err := os.ReadFile(fmt.Sprintf("../admin/web/event-reward-presets/anniversary-%d.json", tc.year))
			if err != nil {
				t.Fatal(err)
			}
			var preset map[int64]map[int64][]struct {
				Item string
				Qty  int64
			}
			if err := json.Unmarshal(raw, &preset); err != nil {
				t.Fatal(err)
			}
			setEventStageGifts(t, srv, db, string(raw))
			if gifts := srv.ensureEventStageGifts(t.Context(), uid); len(gifts) != 0 {
				t.Fatal("unearned gifts")
			}
			for _, branch := range []int64{tc.branch, tc.branch + 1} {
				for stage := int64(1); stage <= 7; stage++ {
					ids := eventStageRuleIDs(branch, stage)
					if len(ids) == 0 {
						t.Fatalf("no tasks for %d/%d", branch, stage)
					}
					completeQualificationTasks(t, db, uid, ids)
					srv.ensureEventStageGifts(t.Context(), uid)
					if _, ok, err := db.KVGet(t.Context(), eventStageGiftKey(uid, branch, stage)); err != nil || !ok {
						t.Fatalf("unclaimed %d/%d: %v", branch, stage, err)
					}
					for _, g := range preset[branch][stage] {
						if _, qty := countContainerItem(t, db, uid, g.Item, "backpack"); qty != g.Qty {
							t.Fatalf("%d/%d %s qty=%d want=%d", branch, stage, g.Item, qty, g.Qty)
						}
					}
					if stage < 7 {
						if _, ok, _ := db.KVGet(t.Context(), eventStageGiftKey(uid, branch, 0)); ok {
							t.Fatal("premature completion gift")
						}
					}
					if gifts := srv.ensureEventStageGifts(t.Context(), uid); len(gifts) != 0 {
						t.Fatal("duplicate stage gifts")
					}
				}
			}
			for _, id := range []string{tc.rod, tc.reel} {
				if rows, qty := countContainerItem(t, db, uid, id, "backpack"); rows != 1 || qty != 1 {
					t.Fatalf("completion %s=%d/%d", id, rows, qty)
				}
			}
			var grants int
			if err := db.SQL.QueryRow(`SELECT COUNT(*) FROM txn WHERE uid=? AND kind='membership_reward'`, uid).Scan(&grants); err != nil || grants != 1 {
				t.Fatalf("membership grants=%d: %v", grants, err)
			}
			rows := giftBoxRows(t, srv, uid, tc.wineBox)
			if len(rows) != 1 {
				t.Fatalf("missing wine box: %+v", rows)
			}
			c := giftBoxTestCtx(t, srv, uid, proto.Args524(proto.U64(0), proto.Guid(rows[0].guid)))
			if _, ok := hItemUse(c); !ok {
				t.Fatal("cannot open historical wine box")
			}
			hItemUse(c)
			if _, qty := countContainerItem(t, db, uid, tc.wine, "backpack"); qty != 12 {
				t.Fatalf("six bottles must give twelve servings once, got %d", qty)
			}
		})
	}
}
