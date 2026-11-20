package gameserver

import (
	"maps"
	"testing"

	"rf4server/internal/proto"
)

func TestStoreRejectsUnprojectableGoodsBeforePayment(t *testing.T) {
	for _, tc := range []struct {
		name          string
		gold          bool
		encodeFailure bool
	}{
		{"silver-projection", false, false},
		{"gold-projection", true, false},
		{"silver-encoding", false, true},
		{"gold-encoding", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, db := newEconomyTxTestServer(t)
			const uid = int64(2099)
			const model = "invalid_attractant"
			seedEconomyItemDef(t, db, 9199, model, 17)
			if !tc.encodeFailure {
				if _, err := db.SQL.Exec(`UPDATE item_def SET attrs_json=? WHERE item_id=?`, `{"gpoaffnjbdo":"invalid"}`, model); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := db.EnsureProfile(t.Context(), uid); err != nil {
				t.Fatal(err)
			}
			if _, err := db.SQL.Exec(`UPDATE profile SET silver=200000,gold=200000 WHERE uid=?`, uid); err != nil {
				t.Fatal(err)
			}
			if _, err := db.SQL.Exec(`INSERT INTO shop_item(store_id,item_id,silver,gold,stock) VALUES('shop',?,1000,1000,10)`, model); err != nil {
				t.Fatal(err)
			}
			// 确认异常出现在物品投影，而不是更早的物品创建阶段。
			items, _, err := s.prepareStoreGoods(t.Context(), uid, model)
			if err != nil || len(items) != 1 {
				t.Fatalf("prepare goods: items=%d err=%v", len(items), err)
			}
			tree, err := s.ItemToTree(t.Context(), items[0])
			if tc.encodeFailure {
				if err != nil {
					t.Fatal(err)
				}
				ops := maps.Clone(s.table.Ops)
				ops[tree.Class] = nil
				s.table = proto.NewOpsTable(maps.Clone(s.table.TypeToClass), ops)
				if _, err := proto.EncodeValue(tree, s.table); err == nil {
					t.Fatal("incompatible protocol table must fail encoding")
				}
			} else if err == nil {
				t.Fatal("invalid hook configuration must fail projection")
			}
			c := economyTestCtx(s, uid, proto.Args524(proto.StrVal("shop"), proto.StrVal(model), proto.U64(1)))
			c.ExpectResponse = true
			c.replyCapture = &ctxReplyCapture{}
			hStoreBuy(tc.gold)(c)
			if _, _, _, failure := c.replyCapture.snapshot(); !failure {
				t.Error("unprojectable goods must be rejected")
			}
			var silver, goldBalance, itemsCount, txns int64
			if err := db.SQL.QueryRow(`SELECT silver,gold FROM profile WHERE uid=?`, uid).Scan(&silver, &goldBalance); err != nil {
				t.Fatal(err)
			}
			if err := db.SQL.QueryRow(`SELECT COUNT(*) FROM item WHERE uid=?`, uid).Scan(&itemsCount); err != nil {
				t.Fatal(err)
			}
			if err := db.SQL.QueryRow(`SELECT COUNT(*) FROM txn WHERE uid=? AND kind='buy'`, uid).Scan(&txns); err != nil {
				t.Fatal(err)
			}
			if silver != 200000 || goldBalance != 200000 || itemsCount != 0 || txns != 0 {
				t.Fatalf("rejected purchase changed state: silver=%d gold=%d items=%d txns=%d", silver, goldBalance, itemsCount, txns)
			}
		})
	}
}

func TestStoreExistingEggStackWireReceipt(t *testing.T) {
	for _, mode := range []string{"corrupt", "encoding", "response-encoding", "normal"} {
		t.Run(mode, func(t *testing.T) {
			corrupt := mode != "normal"
			s, db := newEconomyTxTestServer(t)
			const uid = int64(2098)
			if _, err := db.EnsureProfile(t.Context(), uid); err != nil {
				t.Fatal(err)
			}
			if _, err := db.SQL.Exec(`UPDATE profile SET silver=200000 WHERE uid=?`, uid); err != nil {
				t.Fatal(err)
			}
			if _, err := db.SQL.Exec(`INSERT INTO shop_item(store_id,item_id,silver,gold,stock) VALUES('egg_store','box_food_egg_10',1000,0,100)`); err != nil {
				t.Fatal(err)
			}
			old := economyTestItem(t, s, uid, "food_egg", "backpack", 1)
			if mode == "corrupt" {
				if _, err := db.SQL.Exec(`UPDATE item SET fields_json='broken' WHERE guid=?`, old.GUID); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "encoding" {
				// 仅旧堆使用不可编码类；新产出预检无法覆盖它。
				if _, err := db.SQL.Exec(`UPDATE item SET wire_class='invalid_egg_wire' WHERE guid=?`, old.GUID); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "response-encoding" {
				ops := maps.Clone(s.table.Ops)
				ops["jfafgmcfjkc"] = nil
				s.table = proto.NewOpsTable(maps.Clone(s.table.TypeToClass), ops)
			}
			c := economyTestCtx(s, uid, proto.Args524(proto.StrVal("egg_store"), proto.StrVal("box_food_egg_10"), proto.U64(0)))
			c.Main, c.Sub, c.ExpectResponse = 22, 2, true
			c.Body, _ = proto.EncodeValue(c.Tree, s.table)
			s.dispatchRequest(c)
			key, _ := requestReceiptKey(c)
			rec, err := db.GetRequestReceipt(t.Context(), key)
			if err != nil {
				t.Fatal(err)
			}
			if rec.Status != "completed" || !rec.HasResponse || len(rec.ResponseBody) == 0 || (rec.ErrorText == nativeFailureReceipt) != corrupt {
				t.Errorf("receipt: %+v", rec)
			}
			if !corrupt {
				root, _, err := proto.DecodeValue(rec.ResponseBody, 0, s.table)
				if err != nil {
					t.Fatal(err)
				}
				goods := root.Fields[2].Val
				if goods == nil || len(goods.Items) != 1 {
					t.Fatalf("goods=%+v", goods)
				}
				wire := goods.Items[0]
				qty, ok := itemStackQtyValue(wire.Class, wire.Fields)
				if !ok || qty != 11 || wire.Fields[fieldIdxByCS(wire.Class, csGUID)].Hex != old.GUID {
					t.Fatalf("wire qty=%d guid=%s", qty, wire.Fields[fieldIdxByCS(wire.Class, csGUID)].Hex)
				}
			}
			// 重放必须不调用 handler，而不是仅重复生成相同回执。
			s.dispatch.Register(22, 2, func(*Ctx) ([]byte, bool) {
				t.Fatal("receipt replay must not execute purchase handler")
				return nil, false
			})
			s.dispatchRequest(c)
			replay, err := db.GetRequestReceipt(t.Context(), key)
			if err != nil {
				t.Fatal(err)
			}
			if string(rec.ResponseBody) != string(replay.ResponseBody) || rec.Status != replay.Status || rec.ErrorText != replay.ErrorText || rec.HasResponse != replay.HasResponse {
				t.Fatal("replay changed")
			}
			var balance, txns int64
			if err := db.SQL.QueryRow(`SELECT silver FROM profile WHERE uid=?`, uid).Scan(&balance); err != nil {
				t.Fatal(err)
			}
			if err := db.SQL.QueryRow(`SELECT COUNT(*) FROM txn WHERE uid=? AND kind='buy'`, uid).Scan(&txns); err != nil {
				t.Fatal(err)
			}
			rows, qty, guid := stackingTestStats(t, db, uid, "food_egg", "backpack")
			wantQty, wantBalance, wantTxns := int64(11), int64(199000), int64(1)
			if corrupt {
				wantQty, wantBalance, wantTxns = 1, 200000, 0
			}
			if rows != 1 || qty != wantQty || guid != old.GUID || balance != wantBalance || txns != wantTxns {
				t.Fatalf("rows=%d qty=%d guid=%s balance=%d txns=%d", rows, qty, guid, balance, txns)
			}
		})
	}
}
