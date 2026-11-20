package gameserver

import (
	"bytes"
	"encoding/json"
	"reflect"
	"slices"
	"testing"

	"rf4server/internal/logging"
	"rf4server/internal/proto"
)

const craftLureTestUID int64 = 2000404

func craftLureTestSetup(t *testing.T) (*Server, []craftSlot) {
	t.Helper()
	s := newSkillsCraftServer(t, openStaticTestDB(t))
	if _, err := s.DB.EnsureProfile(t.Context(), craftLureTestUID); err != nil {
		t.Fatal(err)
	}
	var slots []craftSlot
	for _, material := range []struct{ slot, model string }{
		{"plug_sandpap", "material_najdak"},
		{"plug_provoloka", "material_steel_wire"},
		{"plug_wood", "material_birch_bar"},
	} {
		row, err := s.CreateItem(t.Context(), craftLureTestUID, material.model, "backpack", 1)
		if err != nil {
			t.Fatal(err)
		}
		s.alignItemRowQty(t.Context(), row)
		slots = append(slots, craftSlot{ItemID: material.slot, GUID: row.GUID, Species: row.ItemID})
	}
	return s, slots
}

func craftLureTestRequest(t *testing.T, s *Server, slots []craftSlot, success bool) *proto.Value {
	t.Helper()
	return craftLureTestRecipeRequest(t, s, "craft_lure_woodcrank_65", slots, success)
}

func craftLureTestRecipeRequest(t *testing.T, s *Server, recipe string, slots []craftSlot, success bool) *proto.Value {
	t.Helper()
	ctx := &Ctx{Sess: &Session{ID: 1, Userid: craftLureTestUID, Srv: s, log: logging.Discard()},
		Main: 9, Sub: 1, CallID: 1, DecodeOK: true, Log: logging.Discard(),
		Tree: proto.Args524(craftObj8(recipe, slots, 1))}
	body, ok := hCraft(ctx)
	if !ok {
		t.Fatal("craft request not handled")
	}
	result := decodeTreeValue(t, s, body)
	if got := result.Fields[0].U32() != 0; got != success {
		t.Fatalf("craft success=%t, want %t", got, success)
	}
	return result
}

func craftLureTestHook(t *testing.T, s *Server, slot, model string) craftSlot {
	t.Helper()
	row, err := s.CreateItem(t.Context(), craftLureTestUID, model, "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	return craftSlot{ItemID: slot, GUID: row.GUID, Species: model}
}

func assertLurePositions(t *testing.T, s *Server, guid string, want []string) {
	t.Helper()
	raw, ok, err := s.DB.KVGet(t.Context(), lureHookPositionsKey+guid)
	if err != nil || !ok {
		t.Fatalf("positions missing: %v", err)
	}
	var got []string
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, want) {
		t.Fatalf("positions=%v want %v", got, want)
	}
}

func assertCraftLureHooks(t *testing.T, tree *proto.Value, want []string) {
	t.Helper()
	list := tree.Fields[lureHookFieldIndex(tree.Class)].Val
	if list == nil || list.Kind != proto.KindArgs || list.Code == nil || *list.Code != "60" {
		t.Fatalf("expected explicit args(60), got %+v", list)
	}
	if got := lureHookItemGUIDs(list.Items); !slices.Equal(got, want) || len(list.Items) != len(want) {
		t.Fatalf("hooks=%v want %v", got, want)
	}
	for _, hook := range list.Items {
		if hook == nil || hook.Kind != proto.KindObj || hook.Class != "agedjipcnpe" {
			t.Fatalf("invalid hook element: %+v", hook)
		}
	}
}

func TestCraftLureSelectedHooksAndReload(t *testing.T) {
	for _, tc := range []struct {
		name          string
		first, second bool
	}{
		{"none", false, false}, {"first", true, false},
		{"second", false, true}, {"both", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, materials := craftLureTestSetup(t)
			wantPositions := []string{"", ""}
			var wantHooks []string
			for i, selected := range []bool{tc.first, tc.second} {
				if selected {
					slot := craftLureTestHook(t, s, []string{"plug_hook", "plug_hook_2"}[i], []string{"triple_20124", "triple_20125"}[i])
					materials = append(materials, slot)
					wantPositions[i] = slot.GUID
					wantHooks = append(wantHooks, slot.GUID)
				}
			}
			result := craftLureTestRequest(t, s, materials, true)
			produced := result.Fields[1].Val.Items[0]
			guid := treeRootGUID(produced)
			assertLureWireSlots(t, produced, wantPositions)
			if !tc.first && !tc.second {
				raw, err := proto.EncodeValue(produced.Fields[0].Val, s.Table())
				if err != nil || !bytes.Equal(raw, []byte{3, 2, '6', '0', 2, 0, 0, 0}) {
					t.Fatalf("two empty args(60) slots encoded as %x: %v", raw, err)
				}
			}
			assertLurePositions(t, s, guid, wantPositions)
			found := false
			for _, tree := range result.Fields[6].Val.Items {
				if treeRootGUID(tree) == guid {
					assertLureWireSlots(t, tree, wantPositions)
					found = true
				}
			}
			if !found {
				t.Fatal("803 inventory update missing lure")
			}
			rows, err := s.DB.ListItemsByContainers(t.Context(), craftLureTestUID, []string{"backpack", "rig"})
			if err != nil {
				t.Fatal(err)
			}
			hooks := 0
			for _, row := range rows {
				if row.WireClass == "agedjipcnpe" {
					hooks++
					if !slices.Contains(wantHooks, row.GUID) || row.Container != "rig" || row.RigGUID != guid || row.Qty != 1 {
						t.Fatalf("incorrect hook ownership: %+v", row)
					}
				}
			}
			if hooks != len(wantHooks) {
				t.Fatalf("extra hook inventory rows: %d", hooks)
			}
			// Close and reopen the database and server, not just an in-memory tree.
			var seq int
			var name, path string
			if err := s.DB.SQL.QueryRow(`PRAGMA database_list`).Scan(&seq, &name, &path); err != nil {
				t.Fatal(err)
			}
			if err := s.DB.Close(); err != nil {
				t.Fatal(err)
			}
			s = newSkillsCraftServer(t, openStaticDBAtPath(t, path))
			if _, err := s.repairLureHookSlots(t.Context()); err != nil {
				t.Fatal(err)
			}
			row, err := s.DB.GetItemByGUID(t.Context(), guid)
			if err != nil {
				t.Fatal(err)
			}
			fields, err := FieldsFromJSON(row.FieldsJSON)
			if err != nil {
				t.Fatal(err)
			}
			assertCraftLureHooks(t, proto.Obj(row.WireClass, uint32(row.TypeID), fields...), wantHooks)
			tree, err := s.ItemToTree(t.Context(), row)
			if err != nil {
				t.Fatal(err)
			}
			body, err := proto.EncodeValue(tree, s.Table())
			if err != nil {
				t.Fatal(err)
			}
			assertLureWireSlots(t, decodeTreeValue(t, s, body), wantPositions)
			assertLurePositions(t, s, guid, wantPositions)
			if tc.second && !tc.first {
				// The second slot retains index 1 even when the first slot is empty.
				replacement := craftLureTestHook(t, s, "plug_hook_2", "triple_20124")
				res := s.rigSetComponent(t.Context(), craftLureTestUID, guid, catHook, replacement.GUID, 1,
					rigSetOpts{ParentType: familyLure, EnforceCategory: true, SplitStack: true})
				if res.Rejected {
					t.Fatal("replacement of second-slot hook rejected")
				}
				assertLurePositions(t, s, guid, []string{"", replacement.GUID})
			}
		})
	}
}

func TestCraftLureManualHookTransfers(t *testing.T) {
	for _, removedIndex := range []int{0, 1} {
		t.Run([]string{"remove-first", "remove-second"}[removedIndex], func(t *testing.T) {
			s, materials := craftLureTestSetup(t)
			guid := treeRootGUID(craftLureTestRequest(t, s, materials, true).Fields[1].Val.Items[0])
			hooks := []string{
				craftLureTestHook(t, s, "plug_hook", "triple_20124").GUID,
				craftLureTestHook(t, s, "plug_hook_2", "triple_20125").GUID,
			}
			for i, hook := range hooks {
				res := s.rigSetComponent(t.Context(), craftLureTestUID, guid, catHook, hook, int64(i),
					rigSetOpts{ParentType: familyLure, EnforceCategory: true, SplitStack: true})
				if res.Rejected {
					t.Fatal("manual installation rejected")
				}
			}
			assertLurePositions(t, s, guid, hooks)
			if res := s.rigRemoveComponentInstance(t.Context(), craftLureTestUID, guid, catHook, hooks[removedIndex], int64(removedIndex)); res == nil {
				t.Fatal("manual removal rejected")
			}
			positions := slices.Clone(hooks)
			positions[removedIndex] = ""
			assertLurePositions(t, s, guid, positions)
			returned, err := s.DB.GetItemByGUID(t.Context(), hooks[removedIndex])
			if err != nil || returned.Container != "backpack" || returned.RigGUID != "" {
				t.Fatalf("refund: %+v %v", returned, err)
			}
			s = newSkillsCraftServer(t, s.DB)
			if got := lureTestHooks(t, s, guid); !slices.Equal(got, []string{hooks[1-removedIndex]}) {
				t.Fatal(got)
			}
			assertLurePositions(t, s, guid, positions)
			// Reinstall into the original empty slot without replacing its neighbor.
			res := s.rigSetComponent(t.Context(), craftLureTestUID, guid, catHook, returned.GUID, int64(removedIndex),
				rigSetOpts{ParentType: familyLure, EnforceCategory: true, SplitStack: true})
			if res.Rejected {
				t.Fatal("reinstallation rejected")
			}
			assertLurePositions(t, s, guid, hooks)
		})
	}
}

func TestCraftLureRollbackAndInvalidHooks(t *testing.T) {
	for _, mode := range []string{"grant-failure", "position-failure", "duplicate-guid", "duplicate-slot", "foreign", "already-mounted", "broken", "model-mismatch", "unknown-slot", "non-hook", "second-invalid"} {
		t.Run(mode, func(t *testing.T) {
			s, materials := craftLureTestSetup(t)
			slot := craftLureTestHook(t, s, "plug_hook", "triple_20124")
			materials = append(materials, slot)
			var err error
			switch mode {
			case "grant-failure":
				_, err = s.DB.SQL.Exec(`CREATE TRIGGER fail_crafted_lure BEFORE INSERT ON item WHEN NEW.item_id='hm_crank_65s_hq' BEGIN SELECT RAISE(ABORT,'injected'); END`)
			case "position-failure":
				_, err = s.DB.SQL.Exec(`CREATE TRIGGER fail_lure_positions BEFORE INSERT ON kv WHEN NEW.k LIKE 'lure_hook_positions:%' BEGIN SELECT RAISE(ABORT,'injected'); END`)
			case "duplicate-guid":
				slot.ItemID = "plug_hook_2"
				materials = append(materials, slot)
			case "duplicate-slot":
				materials = append(materials, craftLureTestHook(t, s, "plug_hook", "triple_20125"))
			case "foreign":
				_, err = s.DB.SQL.Exec(`UPDATE item SET uid=2000405 WHERE guid=?`, slot.GUID)
			case "already-mounted":
				_, err = s.DB.SQL.Exec(`UPDATE item SET container='rig',rig_guid='other' WHERE guid=?`, slot.GUID)
			case "broken":
				_, err = s.DB.SQL.Exec(`UPDATE item SET durability=0 WHERE guid=?`, slot.GUID)
			case "model-mismatch":
				materials[len(materials)-1].Species = "triple_20125"
			case "unknown-slot":
				materials[len(materials)-1].ItemID = "plug_hook_3"
			case "non-hook":
				materials[len(materials)-1].GUID = materials[0].GUID
			case "second-invalid":
				materials = append(materials, craftSlot{ItemID: "plug_hook_2", GUID: "00112233445566778899aabbccddeeff"})
			}
			if err != nil {
				t.Fatal(err)
			}
			before, err := s.DB.ListItemsByContainers(t.Context(), craftLureTestUID, []string{"backpack", "rig"})
			if err != nil {
				t.Fatal(err)
			}
			craftLureTestRequest(t, s, materials, false)
			after, err := s.DB.ListItemsByContainers(t.Context(), craftLureTestUID, []string{"backpack", "rig"})
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(before, after) {
				t.Fatal("failed craft changed inventory")
			}
			var n int
			if err := s.DB.SQL.QueryRow(`SELECT COUNT(*) FROM kv WHERE k LIKE 'lure_hook_positions:%'`).Scan(&n); err != nil || n != 0 {
				t.Fatalf("position record survived rollback: %d %v", n, err)
			}
		})
	}
}

func TestPurchasedLureKeepsFactoryHooks(t *testing.T) {
	db := openRecipeTestDB(t)
	s := newSkillsCraftServer(t, db)
	if _, err := db.EnsureProfile(t.Context(), craftLureTestUID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL.Exec(`UPDATE profile SET silver=200000 WHERE uid=?`, craftLureTestUID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL.Exec(`INSERT INTO shop_item(store_id,item_id,silver,gold,stock) VALUES('shop','hm_crank_65s_hq',1000,0,10)`); err != nil {
		t.Fatal(err)
	}
	lure := buyReturnTestItem(t, s, craftLureTestUID, "shop", "hm_crank_65s_hq", false)
	want := []string{uuidLike(lure.GUID, lureFactoryHookKey(0)), uuidLike(lure.GUID, lureFactoryHookKey(1))}
	if got := lureTestHooks(t, s, lure.GUID); !slices.Equal(got, want) {
		t.Fatalf("factory hooks=%v", got)
	}
	// Removing the first factory hook preserves the second hook's original slot.
	if res := s.rigRemoveComponentInstance(t.Context(), craftLureTestUID, lure.GUID, catHook, want[0], 0); res == nil {
		t.Fatal("factory removal failed")
	}
	assertLurePositions(t, s, lure.GUID, []string{"", want[1]})
}

func TestCraftLureWithoutConfiguredHookSlots(t *testing.T) {
	s := newSkillsCraftServer(t, openStaticTestDB(t))
	if _, err := s.DB.EnsureProfile(t.Context(), craftLureTestUID); err != nil {
		t.Fatal(err)
	}
	var materials []craftSlot
	for _, id := range []string{"material_foam_rubber", "material_colorsbox"} {
		row, err := s.CreateItem(t.Context(), craftLureTestUID, id, "backpack", 1)
		if err != nil {
			t.Fatal(err)
		}
		s.alignItemRowQty(t.Context(), row)
		materials = append(materials, craftSlot{ItemID: id, GUID: row.GUID, Species: id})
	}
	result := craftLureTestRecipeRequest(t, s, "craft_lure_porolon_black", materials, true)
	lure := result.Fields[1].Val.Items[0]
	assertCraftLureHooks(t, lure, nil)
	assertLurePositions(t, s, treeRootGUID(lure), []string{})
}
