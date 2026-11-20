package gameserver

import (
	"context"
	"testing"
)

// TestFeedMixQtyMirrorHealed 成品窝料（cat 46）双数量槽：权威槽
// ljnnpokapig（csQty）+ UI 镜像 dnmfciolgep。合成路径对非 metaQtyFamily
// 家族单写 stackQty 槽，镜像槽落库为 null（存量毒行甚至两槽皆 null）；
// 树层按行数量双写补齐，只动 wire 不落库。
func TestFeedMixQtyMirrorHealed(t *testing.T) {
	srv, db := newRigTestServer(t)
	ctx := context.Background()
	const uid = int64(1000001)

	row, err := srv.CreateItem(ctx, uid, "feed_dry", "backpack", 1)
	if err != nil {
		t.Fatalf("建成品窝料行失败: %v", err)
	}
	if row.WireClass != feedMixWireClass {
		t.Fatalf("feed_dry wire 类 = %s, want %s", row.WireClass, feedMixWireClass)
	}

	// 造一行「修复前」形态的存量行：数量 37，两数量槽皆 null
	//（实机毒行 1da957943e4c119f8197a416686e4002 即此形态）。
	legacy, err := srv.prepareNewItem(ctx, uid, "feed_dry", "backpack", 1, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := setPreparedItemQuantity(legacy, 37); err != nil {
		t.Fatal(err)
	}
	fields, err := FieldsFromJSON(legacy.FieldsJSON)
	if err != nil {
		t.Fatal(err)
	}
	auth := fieldIdxByCS(legacy.WireClass, csQty)
	mirror := fieldIdxByCS(legacy.WireClass, "dnmfciolgep")
	if auth < 0 || auth >= len(fields) || mirror < 0 || mirror >= len(fields) {
		t.Fatalf("成品窝料数量槽缺位: csQty idx=%d, dnmfciolgep idx=%d", auth, mirror)
	}
	fields[auth].Num = nil
	fields[mirror].Num = nil
	fj, err := FieldsToJSON(fields)
	if err != nil {
		t.Fatal(err)
	}
	legacy.FieldsJSON = fj
	if err := db.InsertItem(ctx, legacy); err != nil {
		t.Fatal(err)
	}

	stored, err := db.GetItemByGUID(ctx, legacy.GUID)
	if err != nil || stored == nil {
		t.Fatalf("回读存量行失败: %+v err=%v", stored, err)
	}
	tree, err := srv.ItemToTree(ctx, stored)
	if err != nil {
		t.Fatal(err)
	}
	if tree.Class != feedMixWireClass {
		t.Fatalf("树 wire 类 = %s, want %s", tree.Class, feedMixWireClass)
	}
	for name, idx := range map[string]int{"权威 csQty": auth, "镜像 dnmfciolgep": mirror} {
		if got := u64Of(tree.Fields[idx].Num); got != 37 {
			t.Fatalf("存量行 %s = %d, want 对齐行数量 37", name, got)
		}
	}
	// 只动 wire 不落库：重读后 fields_json 里两槽仍为 null。
	reread, err := db.GetItemByGUID(ctx, legacy.GUID)
	if err != nil || reread == nil {
		t.Fatal(err)
	}
	rfields, err := FieldsFromJSON(reread.FieldsJSON)
	if err != nil {
		t.Fatal(err)
	}
	if rfields[auth].Num != nil {
		t.Fatalf("树层治愈不应落库: csQty = %v", *rfields[auth].Num)
	}
	if rfields[mirror].Num != nil {
		t.Fatalf("树层治愈不应落库: dnmfciolgep = %v", *rfields[mirror].Num)
	}
}
