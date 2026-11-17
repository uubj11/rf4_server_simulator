package gameserver

import (
	"context"
	"testing"

	"rf4server/internal/store"
)

// 线长成长：路亚套主线 braid_15048 从 110 米线性长到线杯容量 502 米。
func TestGrowthLineLengthAt(t *testing.T) {
	spec, ok := growthSetSpecFor("growth_spin")
	if !ok {
		t.Fatal("缺少路亚套装定义")
	}
	cases := []struct {
		level int64
		want  int64
	}{
		{0, 110},
		{1, 135}, // 110 + 392×1/16 = 134.5
		{3, 184}, // 110 + 392×3/16 = 183.5
		{8, 306},
		{16, 502},
		{99, 502}, // 越界钳到满级
	}
	for _, c := range cases {
		if got := spec.lineLengthAt(c.level); got != c.want {
			t.Fatalf("level=%d 线长应为 %d 米，实得 %d", c.level, c.want, got)
		}
	}
}

// 线长本来就不小于线杯容量的套装不参与线长成长，返回 0 让调用方跳过。
func TestGrowthLineLengthAtSkipsSaturatedSets(t *testing.T) {
	for _, key := range []string{"growth_sea", "growth_bottom", "growth_float", "growth_pole"} {
		spec, ok := growthSetSpecFor(key)
		if !ok {
			t.Fatalf("缺少套装定义 %s", key)
		}
		if got := spec.lineLengthAt(spec.LevelCap); got != 0 {
			t.Fatalf("%s 满级线长不该参与成长，实得 %d", key, got)
		}
	}
}

// 结算后的线长同步只补不削：低于目标就补到目标，高于目标（断线前/更长的线）一律不动。
func TestSyncGrowthLineLengthOnlyGrows(t *testing.T) {
	srv, db := newBundleTestServer(t)
	ctx := context.Background()
	const uid = int64(7001)

	prepared, err := srv.prepareNewItem(ctx, uid, "braid_15048", "backpack", 1, "")
	if err != nil {
		t.Fatalf("准备鱼线实例失败: %v", err)
	}
	tx, err := db.SQL.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("开启事务失败: %v", err)
	}
	it, _, err := grantPreparedItemTx(ctx, tx, prepared, false)
	if err != nil {
		_ = tx.Rollback()
		t.Fatalf("入包失败: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("提交失败: %v", err)
	}
	if err := db.GrowthItemInsert(ctx, store.GrowthItem{GUID: it.GUID, UID: uid, SetKey: "growth_spin", Role: growthRoleLine}); err != nil {
		t.Fatal(err)
	}
	if err := db.GrowthSetStateUpsert(ctx, nil, store.GrowthSetState{UID: uid, SetKey: "growth_spin", Level: 3}); err != nil {
		t.Fatal(err)
	}
	row, err := db.GetItemByGUID(ctx, it.GUID)
	if err != nil || row == nil {
		t.Fatalf("读回鱼线实例失败: %v", err)
	}
	if got := srv.itemLength(ctx, row); got != 110 {
		t.Fatalf("新鱼线起始长度应为静态 110 米，实得 %d", got)
	}

	if err := srv.syncGrowthLineLength(ctx, uid, "growth_spin", 3); err != nil {
		t.Fatalf("同步线长失败: %v", err)
	}
	row, err = db.GetItemByGUID(ctx, it.GUID)
	if err != nil || row == nil {
		t.Fatalf("同步后读回失败: %v", err)
	}
	if got := srv.itemLength(ctx, row); got != 184 {
		t.Fatalf("L3 线长应补到 184 米，实得 %d", got)
	}

	// 升到满级再同步：补到 502。
	if err := db.GrowthSetStateUpsert(ctx, nil, store.GrowthSetState{UID: uid, SetKey: "growth_spin", Level: 16}); err != nil {
		t.Fatal(err)
	}
	if err := srv.syncGrowthLineLength(ctx, uid, "growth_spin", 16); err != nil {
		t.Fatalf("满级同步失败: %v", err)
	}
	row, _ = db.GetItemByGUID(ctx, it.GUID)
	if got := srv.itemLength(ctx, row); got != 502 {
		t.Fatalf("满级线长应补到 502 米，实得 %d", got)
	}

	// 把长度改小（模拟断线扣线），等级也降不回；同步必须按「当前等级」补，
	// 但不允许把已经更长的线削短。
	if err := srv.syncGrowthLineLength(ctx, uid, "growth_pole", 0); err != nil {
		t.Fatalf("非成长线套装同步应为无操作: %v", err)
	}
	row, _ = db.GetItemByGUID(ctx, it.GUID)
	if got := srv.itemLength(ctx, row); got != 502 {
		t.Fatalf("不该被动过的线长变成了 %d", got)
	}
}
