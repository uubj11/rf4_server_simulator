package gameserver

import "testing"

func TestSlotBaitRefillClearedByRemoveAll(t *testing.T) {
	s, _ := newRigTestServer(t)
	ctx := t.Context()
	rod := baitTestRod(t, s)
	_, child := baitTestMount(t, s, rod, "pva_stick", 1, 0)
	if _, err := s.consumeBaitEvent(ctx, 1000001, rod, "pva-clear", []string{child}, true); err != nil {
		t.Fatal(err)
	}
	// 库存为空时补装请求会保留；用户随后明确清空钓组，这个旧意图必须失效。
	if change, err := s.reloadBaitForCast(ctx, 1000001, rod); err != nil || len(change.Updated) != 0 {
		t.Fatalf("empty inventory refill: %+v %v", change, err)
	}
	s.rigRemoveAll(ctx, 1000001, rod)
	stock, err := s.CreateItem(ctx, 1000001, "pva_stick", "backpack", 2)
	if err != nil {
		t.Fatal(err)
	}
	s.setStackCount(ctx, stock, 2)
	change, err := s.reloadBaitForCast(ctx, 1000001, rod)
	if err != nil {
		t.Fatal(err)
	}
	if len(change.Updated) != 0 || len(change.Removed) != 0 {
		t.Fatalf("cleared rig was refilled after restock: %+v", change)
	}
	row, err := s.DB.GetItemByGUID(ctx, stock.GUID)
	if err != nil || row == nil || row.Qty != 2 {
		t.Fatalf("stock changed after remove-all: %+v %v", row, err)
	}
}
