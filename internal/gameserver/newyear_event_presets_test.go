package gameserver

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"testing"

	"rf4server/internal/proto"
)

// Exercise the shipped presets through task completion, inventory materialization
// and box use, including the ordinary/extra branches' different final stages.
func TestNewYearPresetsGrantAndOpenOnce(t *testing.T) {
	for _, tc := range []struct {
		year                               int
		branch                             int64
		rod, reel, coffee, wine, chocolate string
	}{
		{2025, 13, "cast_10159_F70HH_nye", "bcr_5048_nye", "big_coffee_can25", "rd_ice_vine25", "pb_m_choc25"},
		{2026, 17, "cast_10148_CF77XH_NY26", "bcr_6049", "big_coffee_can26", "rd_ice_vine26", "pb_m_choc26"},
	} {
		t.Run(fmt.Sprint(tc.year), func(t *testing.T) {
			srv, db := newStaticRigTestServer(t)
			defer db.Close()
			uid := newProbeSess(srv).Userid
			if _, err := db.EnsureProfile(t.Context(), uid); err != nil {
				t.Fatal(err)
			}
			raw, err := os.ReadFile(fmt.Sprintf("../admin/web/event-reward-presets/newyear-%d.json", tc.year))
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
			boxes := map[string]bool{}
			for _, track := range []struct{ branch, last int64 }{{tc.branch, 7}, {tc.branch + 1, 10}} {
				for stage := int64(1); stage <= track.last; stage++ {
					ids := eventStageRuleIDs(track.branch, stage)
					if len(ids) == 0 {
						t.Fatalf("no tasks: %d/%d", track.branch, stage)
					}
					completeQualificationTasks(t, db, uid, ids)
					srv.ensureEventStageGifts(t.Context(), uid)
					if _, ok, err := db.KVGet(t.Context(), eventStageGiftKey(uid, track.branch, stage)); err != nil || !ok {
						t.Fatalf("not granted %d/%d: %v", track.branch, stage, err)
					}
					for _, gift := range preset[track.branch][stage] {
						def, err := db.GetItemDefByItemID(t.Context(), gift.Item)
						if err != nil || def == nil {
							t.Fatalf("missing item %s: %v", gift.Item, err)
						}
						if _, ok := resolveWireClass(def); !ok {
							t.Fatalf("unsupported reward %s", gift.Item)
						}
						if _, qty := countContainerItem(t, db, uid, gift.Item, "backpack"); qty != gift.Qty {
							t.Fatalf("%d/%d %s: qty=%d want=%d", track.branch, stage, gift.Item, qty, gift.Qty)
						}
						if def.Category == bundleCategory {
							boxes[gift.Item] = true
						}
					}
					_, completed, err := db.KVGet(t.Context(), eventStageGiftKey(uid, track.branch, 0))
					if (err != nil && !errors.Is(err, sql.ErrNoRows)) || completed != (stage == track.last) {
						t.Fatalf("completion timing %d/%d: completed=%v err=%v", track.branch, stage, completed, err)
					}
					if track.branch == tc.branch || stage < track.last {
						for _, id := range []string{tc.rod, tc.reel} {
							if giftBoxItemCount(t, srv, uid, id) != 0 {
								t.Fatalf("premature extra completion: %s", id)
							}
						}
					}
					if gifts := srv.ensureEventStageGifts(t.Context(), uid); len(gifts) != 0 {
						t.Fatal("duplicate stage gifts")
					}
				}
			}
			for id, want := range map[string]int64{tc.rod: 1, tc.reel: 1, tc.coffee: 50, tc.wine: 2} {
				if id == "rd_ice_vine25" {
					want = 4
				} // GameKee: two bottles, two servings each.
				if _, qty := countContainerItem(t, db, uid, id, "backpack"); qty != want {
					t.Fatalf("%s: got %d want %d", id, qty, want)
				}
			}
			var grants int
			if tc.year == 2026 {
				rows := giftBoxRows(t, srv, uid, "ny_mitts")
				if len(rows) != 1 || rows[0].wireClass != "bdbcnhkogcc" || rows[0].typeID != 113 {
					t.Fatalf("mittens must use the glove instance, got %+v", rows)
				}
				it, err := db.GetItemByGUID(t.Context(), rows[0].guid)
				if err != nil {
					t.Fatal(err)
				}
				fields, err := FieldsFromJSON(it.FieldsJSON)
				if err != nil || len(fields) != 19 {
					t.Fatalf("glove fields: %d, %v", len(fields), err)
				}
				if !isItemTypeID(113) || u64Of(fields[0].Num) != 1036831949 || u64Of(fields[1].Num) != 1028443341 {
					t.Fatal("glove protocol contract or definition attributes lost")
				}
			}
			if err := db.SQL.QueryRow(`SELECT COUNT(*) FROM txn WHERE uid=? AND kind='membership_reward'`, uid).Scan(&grants); err != nil || grants != 1 {
				t.Fatalf("membership grants=%d: %v", grants, err)
			}
			// All configured boxes must remain boxed until opened and be consumed once.
			for box := range boxes {
				rows := giftBoxRows(t, srv, uid, box)
				if len(rows) != 1 {
					t.Fatalf("missing box %s: %+v", box, rows)
				}
				def, _ := db.GetItemDefByItemID(t.Context(), box)
				if !bundleOpenable(def) {
					t.Fatalf("reward box cannot be opened: %s", box)
				}
				contents, err := srv.prepareBundleItems(t.Context(), uid, box, "backpack", 1)
				if err != nil || len(contents) == 0 {
					t.Fatalf("invalid contents %s: %v", box, err)
				}
				totals := map[string]int64{}
				for _, it := range contents {
					totals[it.ItemID] += it.Qty
				}
				before := map[string]int64{}
				for id := range totals {
					_, before[id] = countContainerItem(t, db, uid, id, "backpack")
				}
				c := giftBoxTestCtx(t, srv, uid, proto.Args524(proto.U64(0), proto.Guid(rows[0].guid)))
				if _, ok := hItemUse(c); !ok {
					t.Fatalf("cannot open %s", box)
				}
				hItemUse(c)
				if giftBoxItemCount(t, srv, uid, box) != 0 {
					t.Fatalf("box not consumed: %s", box)
				}
				for id, want := range totals {
					if _, qty := countContainerItem(t, db, uid, id, "backpack"); qty-before[id] != want {
						t.Fatalf("%s -> %s got %d want %d", box, id, qty-before[id], want)
					}
				}
			}
			// Independent quantities catch unit multiplication and duplicate opening.
			for id, want := range map[string]int64{"lim_ny25_bottle": 6, tc.chocolate: 10} {
				if _, qty := countContainerItem(t, db, uid, id, "backpack"); qty != want {
					t.Fatalf("opened %s: got %d want %d", id, qty, want)
				}
			}
			if tc.year == 2025 {
				for _, id := range []string{"thermos_tea", "thermos_coffee", "thermos_cacao", "thermos_gwein"} {
					if _, qty := countContainerItem(t, db, uid, id, "backpack"); qty != 60 {
						t.Fatalf("hot drink %s: got %d want 60", id, qty)
					}
				}
			}
			if gifts := srv.ensureEventStageGifts(t.Context(), uid); len(gifts) != 0 {
				t.Fatal("opening boxes must not regrant stage rewards")
			}
		})
	}
}
