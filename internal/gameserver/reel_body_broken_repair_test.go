package gameserver

import "testing"

// 本体被"硬拉爆"后，官方口径是把耐久直接归 0（damageReelLockOverload），
// 但轮内零件并不会一起归零——往往还留着几个百分点的微磨损。
// 此时 repairPartsOf 如果只因为"还有零件没满"就不补本体修理行，
// 客户端修理面板就只剩几个几乎全新的零件，玩家看着"没有地方需要修"，
// 而本体耐久 0 永远修不回去 —— 渔轮彻底死锁。
func TestWorkshopOffersReelBodyRowWhenBodyBrokenDespiteMinorPartWear(t *testing.T) {
	srv, db := newStaticRigTestServer(t)
	// gear 0.988 / 轴承 0.988 都还有不到 1.3% 的磨损，本体已被拉爆归 0。
	reel, _ := seedWornWorkshopReel(t, srv, db, .988, 1, .988, 0)
	row := mustItem(t, db, reel.GUID)

	parts := srv.repairPartsOf(t.Context(), row)
	foundBody := false
	for _, p := range parts {
		if p.Cat == uint32(gearCatReel) && p.Cond == 0 {
			foundBody = true
			break
		}
	}
	if !foundBody {
		t.Fatalf("本体耐久归零时必须提供本体修理行，否则玩家无处可修；实际零件=%d 条", len(parts))
	}
}

// 反向保护：本体只是掉到 0.5（没被拉爆）、且内部还有磨损零件时，
// 仍然只列零件行，不要把本体行一起塞进去挤乱零件修理界面。
func TestWorkshopKeepsPartOnlyRowsWhenBodyWornButNotBroken(t *testing.T) {
	srv, db := newStaticRigTestServer(t)
	reel, _ := seedWornWorkshopReel(t, srv, db, .5, .5, .5, .5)
	row := mustItem(t, db, reel.GUID)

	for _, p := range srv.repairPartsOf(t.Context(), row) {
		if p.Cat == uint32(gearCatReel) && p.GUID == row.GUID {
			t.Fatal("本体未归零时不应额外提供本体修理行")
		}
	}
}
