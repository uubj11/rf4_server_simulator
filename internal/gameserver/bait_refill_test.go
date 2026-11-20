package gameserver

import (
	"fmt"
	"reflect"
	"testing"

	"rf4server/internal/store"
)

func refillFixture(t *testing.T, fish bool, quantity int64) (*Server, string, string, string, string) {
	t.Helper()
	var s *Server
	var rod, lure, id string
	if fish {
		s, _, rod, lure = marineAttractantRig(t, "pilkr_10212_90340", 50)
		id = "fish_pieces"
	} else {
		s, _ = newRigTestServer(t)
		rod = baitTestRod(t, s)
		baitTestMount(t, s, rod, "feeder_30037", 1, 200)
		mountHookForBaitTest(t, s, rod, 0)
		id = "feed_lesh"
	}
	source, child := baitTestMount(t, s, rod, id, quantity, 0)
	return s, rod, lure, source, child
}

func TestSlotBaitRefillInventoryAndPersistence(t *testing.T) {
	for _, fish := range []bool{false, true} {
		t.Run(fmt.Sprintf("fish=%v", fish), func(t *testing.T) {
			s, rod, lure, source, child := refillFixture(t, fish, 4)
			ctx := t.Context()
			before := s.storedRigComponents(ctx, rod)
			_, original, ok := rigCompKeyForGUID(before, child)
			if !ok {
				t.Fatal("missing mounted bait")
			}
			var hooks []string
			if lure != "" {
				hooks = lureTestHooks(t, s, lure)
			}
			if _, err := s.consumeBaitEvent(ctx, 1000001, rod, "use", []string{child}, !fish); err != nil {
				t.Fatal(err)
			}
			// A fresh server reads pending requests and f5 entirely from persistence.
			restarted := newFishingRecoveryServer(t, s.DB)
			change, err := restarted.reloadBaitForCast(ctx, 1000001, rod)
			if err != nil {
				t.Fatal(err)
			}
			if !containsGUID(change.Removed, source) || !containsGUID(change.Updated, rod) {
				t.Fatalf("missing client refresh: %+v", change)
			}
			after := restarted.storedRigComponents(ctx, rod)
			var mounted string
			for _, c := range after {
				if c.Category == original.Category && c.Extra == original.Extra {
					mounted = c.GUID
				}
			}
			if mounted == "" || mounted == child {
				t.Fatal("same slot was not replenished")
			}
			row, err := s.DB.GetItemByGUID(ctx, source)
			if err != nil || row == nil || row.Qty != 2 || baitQuantity(row) != 2 {
				t.Fatalf("source quantity: %+v %v", row, err)
			}
			row, err = s.DB.GetItemByGUID(ctx, mounted)
			if err != nil || row == nil || row.Qty != 1 || row.RigGUID != rod || row.Container != "rig" {
				t.Fatalf("mounted ownership: %+v %v", row, err)
			}
			for _, c := range before {
				if c.GUID == child {
					continue
				}
				_, got, ok := rigCompKeyForGUID(after, c.GUID)
				if !ok || got != c {
					t.Fatalf("other component changed: %+v => %+v", c, got)
				}
			}
			if lure != "" && !reflect.DeepEqual(hooks, lureTestHooks(t, s, lure)) {
				t.Fatal("lure hooks changed")
			}
			projection, err := s.DB.ListRigComponents(ctx, rod)
			if err != nil || len(projection) != len(after) {
				t.Fatalf("projection: %+v %v", projection, err)
			}
			again, err := restarted.reloadBaitForCast(ctx, 1000001, rod)
			if err != nil || len(again.Updated) != 0 || len(again.Removed) != 0 {
				t.Fatalf("repeated refill: %+v %v", again, err)
			}
			if _, err := s.consumeBaitEvent(ctx, 1000001, rod, "use", []string{mounted}, !fish); err != nil {
				t.Fatal(err)
			}
			row, _ = s.DB.GetItemByGUID(ctx, mounted)
			if row == nil || row.Qty != 1 {
				t.Fatal("event replay consumed replacement")
			}
		})
	}
}

func TestDetachElectricReelAfterMarineBaitRefill(t *testing.T) {
	s, rod, _, _, bait := refillFixture(t, true, 4)
	ctx := t.Context()
	const uid = 1000001
	_, reel := baitTestMount(t, s, rod, "elcon_5999", 1, 0)
	_, line := baitTestMount(t, s, rod, "braid_40888", 1, 0)
	if _, err := s.consumeBaitEvent(ctx, uid, rod, "use", []string{bait}, false); err != nil {
		t.Fatal(err)
	}
	if _, err := s.reloadBaitForCast(ctx, uid, rod); err != nil {
		t.Fatal(err)
	}
	if got := rigSlotGUIDs(t, s, rod)[catLine]; got != line {
		t.Errorf("mounted line = %q, want %q", got, line)
	}
	if res := s.rigRemoveComponentInstance(ctx, uid, rod, catReel, reel, 0); res == nil || res.ReturnedTree == nil {
		t.Fatal("electric reel missing from detach result")
	}
	old, err := s.DB.GetItemByGUID(ctx, reel)
	if err != nil || old == nil || old.Container != "backpack" || old.RigGUID != "" {
		t.Fatalf("original reel did not return to backpack: %v", err)
	}
}

func TestReplaceElectricReelAfterBaitOrdering(t *testing.T) {
	s, _ := newRigTestServer(t)
	rod, _, _ := selectedHookMarineRig(t, s, []string{"worm_01", "muckworm"})
	_, reel := baitTestMount(t, s, rod, "elcon_5999", 1, 0)
	if err := s.orderBaitsByHook(t.Context(), 1000001, rod); err != nil {
		t.Fatal(err)
	}
	_, replacement := baitTestMount(t, s, rod, "elcon_6003", 1, 0)
	if got := rigSlotGUIDs(t, s, rod)[catReel]; got != replacement {
		t.Fatalf("replacement reel = %q, want %q", got, replacement)
	}
	old, err := s.DB.GetItemByGUID(t.Context(), reel)
	if err != nil || old == nil || old.Container != "backpack" || old.RigGUID != "" {
		t.Fatalf("original reel did not return to backpack: %v", err)
	}
}

func TestSlotBaitRefillEmptyOccupiedAndRollback(t *testing.T) {
	for _, fish := range []bool{false, true} {
		for _, scenario := range []string{"empty", "occupied", "insert", "quantity", "parent", "request"} {
			t.Run(fmt.Sprintf("fish=%v/%s", fish, scenario), func(t *testing.T) {
				qty := int64(4)
				if scenario == "empty" {
					qty = 1
				}
				s, rod, _, source, child := refillFixture(t, fish, qty)
				ctx := t.Context()
				if _, err := s.consumeBaitEvent(ctx, 1000001, rod, "use", []string{child}, !fish); err != nil {
					t.Fatal(err)
				}
				if scenario == "occupied" {
					id := "feed_karp"
					if fish {
						id = "worm_01"
					}
					baitTestMount(t, s, rod, id, 1, 0)
				}
				before, _ := s.DB.GetItemByGUID(ctx, rod)
				var inventory int64
				if err := s.DB.SQL.QueryRow(`SELECT COALESCE(SUM(qty),0) FROM item WHERE uid=1000001`).Scan(&inventory); err != nil {
					t.Fatal(err)
				}
				trigger := ""
				switch scenario {
				case "insert":
					trigger = `BEFORE INSERT ON item`
				case "quantity":
					trigger = fmt.Sprintf(`BEFORE UPDATE OF qty ON item WHEN OLD.guid='%s'`, source)
				case "parent":
					trigger = fmt.Sprintf(`BEFORE UPDATE ON item WHEN OLD.guid='%s'`, rod)
				case "request":
					trigger = `BEFORE DELETE ON rod_setting WHEN OLD.key LIKE 'refill:%'`
				}
				if trigger != "" {
					if _, err := s.DB.SQL.Exec(`CREATE TRIGGER fail_refill ` + trigger + ` BEGIN SELECT RAISE(ABORT,'injected refill failure'); END`); err != nil {
						t.Fatal(err)
					}
				}
				change, err := s.reloadBaitForCast(ctx, 1000001, rod)
				if trigger != "" && err == nil || trigger == "" && err != nil {
					t.Fatalf("unexpected result: %+v %v", change, err)
				}
				if len(change.Updated) != 0 || len(change.Removed) != 0 {
					t.Fatalf("unexpected partial update: %+v", change)
				}
				after, _ := s.DB.GetItemByGUID(ctx, rod)
				if before.FieldsJSON != after.FieldsJSON {
					t.Fatal("rig changed")
				}
				var afterInventory int64
				if err := s.DB.SQL.QueryRow(`SELECT COALESCE(SUM(qty),0) FROM item WHERE uid=1000001`).Scan(&afterInventory); err != nil {
					t.Fatal(err)
				}
				if inventory != afterInventory {
					t.Fatal("quantity changed")
				}
				if trigger != "" {
					if _, err := s.DB.SQL.Exec(`DROP TRIGGER fail_refill`); err != nil {
						t.Fatal(err)
					}
					if change, err := s.reloadBaitForCast(ctx, 1000001, rod); err != nil || len(change.Updated) == 0 {
						t.Fatalf("retry failed: %+v %v", change, err)
					}
				}
			})
		}
	}
}

func TestSlotBaitRefillRecipeAndReopen(t *testing.T) {
	s, rod, _, source, child := refillFixture(t, false, 3)
	ctx := t.Context()
	for _, g := range []string{source, child} {
		if _, err := s.DB.SQL.Exec(`INSERT INTO groundbait_recipe(instance_guid,mats_json) VALUES(?,?)`, g, `["corn"]`); err != nil {
			t.Fatal(err)
		}
	}
	other, err := s.CreateItem(ctx, 1000001, "feed_lesh", "backpack", 8)
	if err != nil {
		t.Fatal(err)
	}
	s.setStackCount(ctx, other, 8)
	if _, err := s.consumeBaitEvent(ctx, 1000001, rod, "recipe", []string{child}, true); err != nil {
		t.Fatal(err)
	}
	path := s.DB.Path
	if err := s.DB.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s.SetDB(db)
	if _, err := s.reloadBaitForCast(ctx, 1000001, rod); err != nil {
		t.Fatal(err)
	}
	row, _ := db.GetItemByGUID(ctx, source)
	if row == nil || row.Qty != 1 {
		t.Fatalf("matching recipe not used: %+v", row)
	}
	row, _ = db.GetItemByGUID(ctx, other.GUID)
	if row == nil || row.Qty != 8 {
		t.Fatalf("unrelated recipe changed: %+v", row)
	}
	for _, c := range s.storedRigComponents(ctx, rod) {
		if c.Category != catGroundbaitMix {
			continue
		}
		var recipe string
		if err := db.SQL.QueryRow(`SELECT mats_json FROM groundbait_recipe WHERE instance_guid=?`, c.GUID).Scan(&recipe); err != nil || recipe != `["corn"]` {
			t.Fatalf("recipe not cloned: %q %v", recipe, err)
		}
	}
}

func TestSlotBaitRefillStandaloneHookAndHookRemoval(t *testing.T) {
	for _, standalone := range []bool{false, true} {
		for _, remove := range []bool{false, true} {
			t.Run(fmt.Sprintf("standalone=%v/remove=%v", standalone, remove), func(t *testing.T) {
				s, rod, lure, source, child := refillFixture(t, true, 3)
				ctx := t.Context()
				hook := ""
				if standalone {
					hook = mountHookForBaitTest(t, s, rod, 0)
					if err := s.DB.UpsertRodSetting(ctx, rod, "bait_hook:"+child, hook); err != nil {
						t.Fatal(err)
					}
				} else {
					hook = s.lureActiveHookGUID(ctx, lure)
				}
				if _, err := s.consumeBaitEvent(ctx, 1000001, rod, "hook", []string{child}, false); err != nil {
					t.Fatal(err)
				}
				if remove {
					if standalone {
						s.rigRemoveItemComponent(ctx, 1000001, rod, hook)
					} else {
						s.lureHookRemove(ctx, 1000001, lure, hook, 250)
					}
				}
				change, err := s.reloadBaitForCast(ctx, 1000001, rod)
				if err != nil {
					t.Fatal(err)
				}
				want := int64(1)
				if remove {
					want = 2
					if len(change.Updated) != 0 {
						t.Fatal("refilled without hook")
					}
				}
				row, _ := s.DB.GetItemByGUID(ctx, source)
				if row == nil || row.Qty != want {
					t.Fatalf("quantity: %+v want %d", row, want)
				}
			})
		}
	}
}

func TestSlotBaitRefillPVAAndNoSubstitution(t *testing.T) {
	s, _ := newRigTestServer(t)
	ctx := t.Context()
	rod := baitTestRod(t, s)
	source, child := baitTestMount(t, s, rod, "pva_stick", 2, 0)
	if _, err := s.consumeBaitEvent(ctx, 1000001, rod, "pva", []string{child}, true); err != nil {
		t.Fatal(err)
	}
	if _, err := s.reloadBaitForCast(ctx, 1000001, rod); err != nil {
		t.Fatal(err)
	}
	if row, _ := s.DB.GetItemByGUID(ctx, source); row != nil {
		t.Fatal("last source portion was duplicated")
	}
	for _, c := range s.storedRigComponents(ctx, rod) {
		if c.Category != catPVAStick {
			continue
		}
		row, _ := s.DB.GetItemByGUID(ctx, c.GUID)
		if row == nil || row.Qty != 1 || splitSourceGUID(row) != "" {
			t.Fatalf("last portion: %+v", row)
		}
		if _, err := s.consumeBaitEvent(ctx, 1000001, rod, "pva2", []string{c.GUID}, true); err != nil {
			t.Fatal(err)
		}
	}
	other, err := s.CreateItem(ctx, 1000001, "feed_lesh", "backpack", 3)
	if err != nil {
		t.Fatal(err)
	}
	s.setStackCount(ctx, other, 3)
	change, err := s.reloadBaitForCast(ctx, 1000001, rod)
	if err != nil || len(change.Updated) != 0 {
		t.Fatalf("substituted unrelated bait: %+v %v", change, err)
	}
}

func TestSlotBaitRefillSkipsRemovedFeeder(t *testing.T) {
	s, rod, _, source, child := refillFixture(t, false, 3)
	ctx := t.Context()
	if _, err := s.consumeBaitEvent(ctx, 1000001, rod, "feeder", []string{child}, true); err != nil {
		t.Fatal(err)
	}
	for _, c := range s.storedRigComponents(ctx, rod) {
		if c.Category == 31 {
			s.rigRemoveItemComponent(ctx, 1000001, rod, c.GUID)
		}
	}
	change, err := s.reloadBaitForCast(ctx, 1000001, rod)
	if err != nil || len(change.Updated) != 0 {
		t.Fatalf("refilled removed feeder: %+v %v", change, err)
	}
	row, _ := s.DB.GetItemByGUID(ctx, source)
	if row == nil || row.Qty != 2 {
		t.Fatalf("source changed: %+v", row)
	}
}
