package gameserver

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"

	"rf4server/internal/proto"
	"rf4server/internal/store"
)

const duplicateCoffeeGifts = `{"13":{"7":[{"item":"fcb_coffee_can2","qty":2},{"item":"fcb_coffee_can2","qty":3}]}}`

func seedEventGiftCoffee(t *testing.T, srv *Server, db *store.DB, uid, qty int64) {
	t.Helper()
	if qty == 0 {
		return
	}
	item, err := srv.prepareNewItemWithCount(t.Context(), uid, "fcb_coffee_can2", "backpack", qty, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.InsertItem(t.Context(), item); err != nil {
		t.Fatal(err)
	}
}

func TestEventStageGiftDuplicateStacks(t *testing.T) {
	for _, tc := range []struct {
		name              string
		initial           int64
		multipleStages    bool
		wantRows, wantQty int64
	}{
		{"empty-inventory", 0, false, 1, 5},
		{"existing-stack", 7, false, 1, 12},
		{"multiple-stages", 7, true, 1, 16},
		// Coffee's quantity field is U32: the first reward fits, the next needs a new stack.
		{"stack-overflow", 4294967292, false, 2, 4294967297},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, db := newStaticRigTestServer(t)
			defer db.Close()
			uid := newProbeSess(srv).Userid
			seedEventGiftCoffee(t, srv, db, uid, tc.initial)
			config := duplicateCoffeeGifts
			if tc.multipleStages {
				config = `{"13":{"6":[{"item":"fcb_coffee_can2","qty":4}],"7":[{"item":"fcb_coffee_can2","qty":2},{"item":"fcb_coffee_can2","qty":3}]}}`
				completeQualificationTasks(t, db, uid, eventStageRuleIDs(13, 6))
			}
			setEventStageGifts(t, srv, db, config)
			completeQualificationTasks(t, db, uid, eventStageRuleIDs(13, 7))
			gifts := srv.ensureEventStageGifts(t.Context(), uid)
			if int64(len(gifts)) != tc.wantRows {
				t.Fatalf("reward rows=%d, want %d", len(gifts), tc.wantRows)
			}
			seen := map[string]bool{}
			for _, row := range gifts {
				if seen[row.GUID] {
					t.Fatalf("duplicate reward GUID %s", row.GUID)
				}
				seen[row.GUID] = true
				stored, err := db.GetItemByGUID(t.Context(), row.GUID)
				if err != nil || stored == nil || row.Qty != stored.Qty || row.FieldsJSON != stored.FieldsJSON {
					t.Fatalf("reward does not contain final inventory state: row=%+v stored=%+v err=%v", row, stored, err)
				}
			}
			if rows, qty := countContainerItem(t, db, uid, "fcb_coffee_can2", "backpack"); rows != tc.wantRows || qty != tc.wantQty {
				t.Fatalf("inventory=%d/%d, want %d/%d", rows, qty, tc.wantRows, tc.wantQty)
			}
			if _, ok, err := db.KVGet(t.Context(), eventStageGiftKey(uid, 13, 7)); err != nil || !ok {
				t.Fatalf("missing committed reward marker: %v", err)
			}
			if got := srv.ensureEventStageGifts(t.Context(), uid); len(got) != 0 {
				t.Fatal("repeated inspection granted rewards again")
			}
		})
	}
}

func TestEventStageGiftOverflowRollback(t *testing.T) {
	srv, db := newStaticRigTestServer(t)
	defer db.Close()
	uid := newProbeSess(srv).Userid
	seedEventGiftCoffee(t, srv, db, uid, 4294967292)
	setEventStageGifts(t, srv, db, duplicateCoffeeGifts)
	completeQualificationTasks(t, db, uid, eventStageRuleIDs(13, 7))
	// Fail the second stack insertion after the first reward has merged into the existing stack.
	if _, err := db.SQL.Exec(`CREATE TEMP TRIGGER reject_event_gift_insert BEFORE INSERT ON item
		WHEN NEW.item_id='fcb_coffee_can2' BEGIN SELECT RAISE(ABORT, 'test inventory failure'); END`); err != nil {
		t.Fatal(err)
	}
	if got := srv.ensureEventStageGifts(t.Context(), uid); len(got) != 0 {
		t.Fatal("failed transaction returned rewards")
	}
	if rows, qty := countContainerItem(t, db, uid, "fcb_coffee_can2", "backpack"); rows != 1 || qty != 4294967292 {
		t.Fatalf("failed transaction left partial rewards: %d/%d", rows, qty)
	}
	if _, ok, err := db.KVGet(t.Context(), eventStageGiftKey(uid, 13, 7)); ok || (err != nil && !errors.Is(err, sql.ErrNoRows)) {
		t.Fatalf("failed transaction left a reward marker: present=%v err=%v", ok, err)
	}
	if _, err := db.SQL.Exec(`DROP TRIGGER reject_event_gift_insert`); err != nil {
		t.Fatal(err)
	}
	if got := srv.ensureEventStageGifts(t.Context(), uid); len(got) != 2 {
		t.Fatalf("retry returned %d rewards, want 2", len(got))
	}
	if rows, qty := countContainerItem(t, db, uid, "fcb_coffee_can2", "backpack"); rows != 2 || qty != 4294967297 {
		t.Fatalf("retry inventory=%d/%d", rows, qty)
	}
	if got := srv.ensureEventStageGifts(t.Context(), uid); len(got) != 0 {
		t.Fatal("successful retry granted rewards again")
	}
}

// assertEventGiftCoffeeTree checks both uniqueness and the final quantity, including stale intermediate trees.
func assertEventGiftCoffeeTree(t *testing.T, items *proto.Value, guid string, qty uint64) {
	t.Helper()
	if items == nil || items.Kind != proto.KindArgs || items.ArgCodeStr() != "61" {
		t.Fatalf("expected args(61): %v", items)
	}
	count := 0
	for _, tree := range items.Items {
		if tree.TypeID != 128 || tree.Fields[2].Hex != guid {
			continue
		}
		count++
		if got := u64Of(tree.Fields[1].Num); got != qty {
			t.Errorf("coffee quantity=%d, want final quantity %d", got, qty)
		}
	}
	if count != 1 {
		t.Errorf("coffee GUID appears %d times, want 1", count)
	}
}

func TestEventStageGiftDuplicateSettlement(t *testing.T) {
	srv, db := newStaticRigTestServer(t)
	defer db.Close()
	sess := newProbeSess(srv)
	if _, err := db.EnsureProfile(t.Context(), sess.Userid); err != nil {
		t.Fatal(err)
	}
	seedEventGiftCoffee(t, srv, db, sess.Userid, 7)
	setEventStageGifts(t, srv, db, duplicateCoffeeGifts)
	completeQualificationTasks(t, db, sess.Userid, eventStageRuleIDs(13, 7))
	fish := &RolledFish{FishGUID: newFishGUID(), Species: "g_carp", WeightG: 7000, Grade: 1, LocLevel: evLocMedvezhye}
	obj := wlSettle(t, srv, sess, fish, 1)
	var guid string
	if err := db.SQL.QueryRow(`SELECT guid FROM item WHERE uid=? AND item_id='fcb_coffee_can2' AND container='backpack'`, sess.Userid).Scan(&guid); err != nil {
		t.Fatal(err)
	}
	assertEventGiftCoffeeTree(t, obj.Fields[8].Val, guid, 12)
	rows := obj.Fields[4].Val
	if rows == nil || rows.Kind != proto.KindArgs || rows.ArgCodeStr() != "173" {
		t.Fatalf("expected args(173) gift rows: %v", rows)
	}
	eventRows := 0
	for _, row := range rows.Items {
		if row.Kind != proto.KindObj || row.TypeID != 811 {
			t.Fatalf("nested gift container: %v", row)
		}
		if u64Of(row.Fields[0].Num) == uint64(giftSourceGameEvent) {
			eventRows++
			assertEventGiftCoffeeTree(t, row.Fields[2].Val, guid, 12)
		}
	}
	if eventRows != 1 {
		t.Fatalf("event gift rows=%d", eventRows)
	}
	replay := wlSettle(t, srv, sess, fish, 2)
	for _, tree := range replay.Fields[8].Val.Items {
		if tree.TypeID == 128 && tree.Fields[2].Hex == guid {
			t.Fatal("replay contains the same stage reward")
		}
	}
	if rows, qty := countContainerItem(t, db, sess.Userid, "fcb_coffee_can2", "backpack"); rows != 1 || qty != 12 {
		t.Fatalf("inventory after replay=%d/%d", rows, qty)
	}
}

func TestEventStageGiftDuplicateSnapshotAndChat(t *testing.T) {
	srv, db := newStaticRigTestServer(t)
	defer db.Close()
	uid := newProbeSess(srv).Userid
	seedEventGiftCoffee(t, srv, db, uid, 7)
	setEventStageGifts(t, srv, db, duplicateCoffeeGifts)
	completeQualificationTasks(t, db, uid, eventStageRuleIDs(13, 7))
	// Finish the branch's existing cup award before attaching the session, so only the stage is pending.
	srv.ensureQualificationCups(t.Context(), uid)
	var name, guid string
	if err := db.SQL.QueryRow(`SELECT name_zh FROM item_catalog WHERE item_id='fcb_coffee_can2'`).Scan(&name); err != nil || strings.TrimSpace(name) == "" {
		t.Fatalf("missing static Chinese item name: %v", err)
	}
	if err := db.SQL.QueryRow(`SELECT guid FROM item WHERE uid=? AND item_id='fcb_coffee_can2' AND container='backpack'`, uid).Scan(&guid); err != nil {
		t.Fatal(err)
	}
	h := newMailTestSession(t, srv, 1, uid)
	if _, ok := hFog(&Ctx{Sess: h.sess}); !ok {
		t.Fatal("snapshot failed")
	}
	chat := nextMailTestPush(t, h, 24, 27)
	if got, want := chat.Items[0].Fields[1].StrOr(), fmt.Sprintf("获得奖赏：「%s」", name); got != want {
		t.Errorf("reward chat=%q, want %q", got, want)
	}
	items := nextMailTestPush(t, h, 4, 22)
	assertEventGiftCoffeeTree(t, items, guid, 12)
	assertNoMailTestPush(t, h)
	if _, ok := hFog(&Ctx{Sess: h.sess}); !ok {
		t.Fatal("repeated snapshot failed")
	}
	assertNoMailTestPush(t, h)
}

func TestEventStageGiftMultipleNamesChat(t *testing.T) {
	for _, tc := range []struct {
		name   string
		config string
		ids    []string
		qtys   []int64
	}{
		{
			name:   "two-rods",
			config: `{"13":{"7":[{"item":"spin_5140","qty":1},{"item":"cast_10059_CSF78MH","qty":1}]}}`,
			ids:    []string{"spin_5140", "cast_10059_CSF78MH"},
			qtys:   []int64{1, 1},
		},
		{
			name:   "mixed-duplicate-stacks",
			config: `{"13":{"7":[{"item":"fcb_coffee_can2","qty":2},{"item":"ab_tea_can2","qty":1},{"item":"fcb_coffee_can2","qty":3}]}}`,
			ids:    []string{"fcb_coffee_can2", "ab_tea_can2"},
			qtys:   []int64{5, 1},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, db := newStaticRigTestServer(t)
			defer db.Close()
			uid := newProbeSess(srv).Userid
			setEventStageGifts(t, srv, db, tc.config)
			completeQualificationTasks(t, db, uid, eventStageRuleIDs(13, 7))
			srv.ensureQualificationCups(t.Context(), uid)
			want := "获得奖赏："
			for _, id := range tc.ids {
				var name string
				if err := db.SQL.QueryRow(`SELECT name_zh FROM item_catalog WHERE item_id=?`, id).Scan(&name); err != nil || strings.TrimSpace(name) == "" {
					t.Fatalf("missing static Chinese name for %s: %v", id, err)
				}
				want += "「" + name + "」"
			}
			h := newMailTestSession(t, srv, 1, uid)
			if _, ok := hFog(&Ctx{Sess: h.sess}); !ok {
				t.Fatal("snapshot failed")
			}
			chat := nextMailTestPush(t, h, 24, 27)
			if got := chat.Items[0].Fields[1].StrOr(); got != want {
				t.Errorf("reward chat=%q, want %q", got, want)
			}
			for i, id := range tc.ids {
				nextMailTestPush(t, h, 4, 22)
				if rows, qty := countContainerItem(t, db, uid, id, "backpack"); rows != 1 || qty != tc.qtys[i] {
					t.Errorf("inventory %s=%d/%d, want 1/%d", id, rows, qty, tc.qtys[i])
				}
			}
			assertNoMailTestPush(t, h)
			if _, ok := hFog(&Ctx{Sess: h.sess}); !ok {
				t.Fatal("repeated snapshot failed")
			}
			assertNoMailTestPush(t, h)
		})
	}
}
