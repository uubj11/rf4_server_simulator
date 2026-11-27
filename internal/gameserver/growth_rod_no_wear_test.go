package gameserver

import (
	"context"
	"math"
	"testing"

	"rf4server/internal/logging"
	"rf4server/internal/proto"
	"rf4server/internal/store"
)

// 成长竿「永不磨损」的核心行为：
// 1) 竿体耐久掉到 0.10 后，下一次渔获直接回满 1.0（玩家不用去工坊修，也没有修复入口）；
// 2) 已经满耐久时不再产生磨损写入，避免每竿鱼都刷一条无变化的 wear 日志。
func TestGrowthRodNoWearTopsUpOnCatch(t *testing.T) {
	t.Parallel()
	row := &store.Item{GUID: "0123456789abcdef0123456789abcdef", Durability: 0.10, WireClass: "gear"}

	cw, err := catchWearForItem(row, "rod", 0.2, true, true, gearCatRod, 0)
	if err != nil {
		t.Fatalf("永不磨损口径不应报错: %v", err)
	}
	if cw == nil {
		t.Fatal("0.10 的竿应产生一次回满写入")
	}
	if math.Abs(cw.durability-1.0) > 1e-9 {
		t.Fatalf("耐久应回满 1.0，实得 %v", cw.durability)
	}

	full := &store.Item{GUID: "fedcba9876543210fedcba9876543210", Durability: 1.0, WireClass: "gear"}
	cw2, err := catchWearForItem(full, "rod", 0.2, true, true, gearCatRod, 0)
	if err != nil {
		t.Fatalf("满耐久不应报错: %v", err)
	}
	if cw2 != nil {
		t.Fatalf("满耐久不该再写磨损，实得 %+v", cw2)
	}
}

// noWear=false 时必须保持旧口径（按鱼重回血，而不是直接回满），
// 否则非成长件/开关关闭时会被这条新口径连带改掉。
func TestGrowthRodNoWearOffKeepsHealRule(t *testing.T) {
	t.Parallel()
	row := &store.Item{GUID: "0123456789abcdef0123456789abcdef", Durability: 0.10, WireClass: "gear"}

	cw, err := catchWearForItem(row, "rod", 0.165, true, false, gearCatRod, 0)
	if err != nil {
		t.Fatal(err)
	}
	want := growthDurabilityAfterCatch(0.10, wearPerCatch["rod"], 0.165, true)
	if cw == nil || math.Abs(cw.durability-want) > 1e-9 {
		t.Fatalf("旧口径应得 %v，实得 %+v", want, cw)
	}
	if want >= 0.11 {
		t.Fatalf("旧口径单尾小鱼只该回 1%% 保底，用例前提不成立: %v", want)
	}
}

// 把 growth_rod_no_wear 关掉后必须回到旧口径：17/9 超载只把耐久压到下限 0.10，
// 钓组仍豁免保留（growth_item_indestructible 单独生效）。
func TestGrowthRodBreakSwitchOffKeepsFloor(t *testing.T) {
	t.Parallel()
	srv, db := newRigTestServer(t)
	ctx := context.Background()
	const uid = int64(1000001)

	rod, err := srv.CreateItem(ctx, uid, "tele_10166_BK8", "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	line, err := srv.CreateItem(ctx, uid, "braid_15120", "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	hook, err := srv.CreateItem(ctx, uid, "clas_20472", "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.GrowthItemInsert(ctx, store.GrowthItem{
		GUID: rod.GUID, UID: uid, SetKey: "growth_pole", Role: growthRoleRod,
	}); err != nil {
		t.Fatal(err)
	}
	setTunableForTest(t, srv, tunableGrowthRodNoWear, "0")

	sess := newProbeSess(srv)
	mountGearForTest(t, srv, sess, rod.GUID, line.GUID)
	mountGearForTest(t, srv, sess, rod.GUID, hook.GUID)
	setTestLineLengthM(t, db, line.GUID, 50)

	tree := rigRequestTree(t, proto.Guid(rod.GUID), proto.Guid(rod.GUID), proto.U32(1), proto.Null())
	hctx := &Ctx{Sess: sess, Main: 17, Sub: 9, Tree: tree,
		Body:     lineBreakBodyWithLineOut(0),
		DecodeOK: true, ExpectResponse: true, Log: logging.Discard()}
	if _, ok := hDynamicRodBreak(hctx); !ok {
		t.Fatal("17/9 断竿未处理")
	}
	if got := mustItem(t, db, rod.GUID).Durability; math.Abs(got-growthDurabilityFloor) > 1e-9 {
		t.Fatalf("关掉永不磨损后耐久应停在下限 %.2f，实得 %.4f", growthDurabilityFloor, got)
	}
}

// 只有「手杆成长套装」那根竿命中永不磨损；其它套装的竿（海钓/底钓/路亚/浮漂 tele）
// 必须落在旧口径：识别函数直接返回 false，17/9 超载后耐久压到下限 0.10。
func TestOnlyGrowthPoleRodIsNoWearScope(t *testing.T) {
	t.Parallel()
	srv, db := newRigTestServer(t)
	ctx := context.Background()
	const uid = int64(1000001)

	poleRod, err := srv.CreateItem(ctx, uid, "tele_10166_BK8", "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	spinRod, err := srv.CreateItem(ctx, uid, "spin_10105_SPXF98H", "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range []store.GrowthItem{
		{GUID: poleRod.GUID, UID: uid, SetKey: growthPoleRodNoWearSetKey, Role: growthRoleRod},
		{GUID: spinRod.GUID, UID: uid, SetKey: "growth_spin", Role: growthRoleRod},
	} {
		if err := db.GrowthItemInsert(ctx, it); err != nil {
			t.Fatal(err)
		}
	}
	if !srv.isGrowthPoleRod(ctx, poleRod.GUID) {
		t.Fatal("手杆套的竿应命中永不磨损口径")
	}
	if srv.isGrowthPoleRod(ctx, spinRod.GUID) {
		t.Fatal("路亚套的竿不得命中永不磨损口径")
	}

	// 路亚竿走 17/9：仍是旧口径（耐久 0.10），竿与组件按成长件豁免保留。
	line, err := srv.CreateItem(ctx, uid, "braid_15048", "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	hook, err := srv.CreateItem(ctx, uid, "clas_20472", "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	sess := newProbeSess(srv)
	mountGearForTest(t, srv, sess, spinRod.GUID, line.GUID)
	mountGearForTest(t, srv, sess, spinRod.GUID, hook.GUID)
	setTestLineLengthM(t, db, line.GUID, 50)

	tree := rigRequestTree(t, proto.Guid(spinRod.GUID), proto.Guid(spinRod.GUID), proto.U32(1), proto.Null())
	hctx := &Ctx{Sess: sess, Main: 17, Sub: 9, Tree: tree,
		Body:     lineBreakBodyWithLineOut(0),
		DecodeOK: true, ExpectResponse: true, Log: logging.Discard()}
	if _, ok := hDynamicRodBreak(hctx); !ok {
		t.Fatal("17/9 断竿未处理")
	}
	if got := mustItem(t, db, spinRod.GUID).Durability; math.Abs(got-growthDurabilityFloor) > 1e-9 {
		t.Fatalf("非手杆套装仍应压到下限 %.2f，实得 %.4f", growthDurabilityFloor, got)
	}
}
