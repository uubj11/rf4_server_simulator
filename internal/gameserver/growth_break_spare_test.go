package gameserver

import (
	"context"
	"math"
	"testing"
	"time"

	"rf4server/internal/logging"
	"rf4server/internal/proto"
	"rf4server/internal/store"
)

// setTunableForTest 写一条 tunable 并清掉缓存，等价于后台改参数后 reload。
func setTunableForTest(t *testing.T, s *Server, key, value string) {
	t.Helper()
	if err := s.DB.TunableSet(context.Background(), key, value, "test"); err != nil {
		t.Fatal(err)
	}
	s.tunMu.Lock()
	s.tunAt = time.Time{}
	s.tunMu.Unlock()
}

func durabilityFieldOf(t *testing.T, row *store.Item) float64 {
	t.Helper()
	fields, err := FieldsFromJSON(row.FieldsJSON)
	if err != nil {
		t.Fatal(err)
	}
	idx := fieldIdxByCS(row.WireClass, csDurability)
	if idx < 0 || idx >= len(fields) || fields[idx].Num == nil {
		t.Fatalf("耐久字段缺失: class=%s idx=%d", row.WireClass, idx)
	}
	return float64(proto.F32FromBits(uint32(u64Of(fields[idx].Num))))
}

// 成长套装的竿被 17/9 判超载时，只扣耐久到下限，不得彻底损坏：
// 竿仍在、钓组与组件原样保留、11028 的移除列表为空。
func TestGrowthRodBreakSparesRodAndRig(t *testing.T) {
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
	bobber, err := srv.CreateItem(ctx, uid, "bob_25084", "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range []store.GrowthItem{
		{GUID: rod.GUID, UID: uid, SetKey: "growth_pole", Role: growthRoleRod},
		{GUID: hook.GUID, UID: uid, SetKey: "growth_pole", Role: growthRoleHook},
	} {
		if err := db.GrowthItemInsert(ctx, it); err != nil {
			t.Fatal(err)
		}
	}

	sess := newProbeSess(srv)
	mountGearForTest(t, srv, sess, rod.GUID, line.GUID)
	mountGearForTest(t, srv, sess, rod.GUID, hook.GUID)
	mountGearForTest(t, srv, sess, rod.GUID, bobber.GUID)
	setTestLineLengthM(t, db, line.GUID, 50)

	tree := rigRequestTree(t, proto.Guid(rod.GUID), proto.Guid(rod.GUID), proto.U32(1), proto.Null())
	hctx := &Ctx{Sess: sess, Main: 17, Sub: 9, Tree: tree,
		Body:     lineBreakBodyWithLineOut(12.5),
		DecodeOK: true, ExpectResponse: true, Log: logging.Discard()}
	body, ok := hDynamicRodBreak(hctx)
	if !ok || len(body) == 0 {
		t.Fatal("17/9 断竿未处理")
	}

	freshRod := mustItem(t, db, rod.GUID)
	if freshRod == nil {
		t.Fatal("成长件断竿后竿本体不得被删")
	}
	if math.Abs(freshRod.Durability-growthDurabilityFloor) > 1e-9 {
		t.Fatalf("成长竿耐久应停在下限 %.2f，实得 %.4f", growthDurabilityFloor, freshRod.Durability)
	}
	if got := durabilityFieldOf(t, freshRod); math.Abs(got-growthDurabilityFloor) > 1e-6 {
		t.Fatalf("耐久字段应写 %.2f，实得 %.4f", growthDurabilityFloor, got)
	}
	for _, guid := range []string{hook.GUID, bobber.GUID} {
		if row, _ := db.GetItemByGUID(ctx, guid); row == nil {
			t.Fatalf("成长套组件 %s 不得被销毁", guid)
		}
	}
	if _, hasHook := rigCompsOf(rigStoredF5(t, freshRod))[catHook]; !hasHook {
		t.Fatal("成长件断竿后钓组 f5 应原样保留钩")
	}

	dec, _, err := proto.DecodeValue(body, 0, srv.Table())
	if err != nil {
		t.Fatal(err)
	}
	if f4 := dec.Fields[4].Val; f4 != nil && f4.Kind == proto.KindArgs && len(f4.Items) > 0 {
		t.Fatalf("成长件断竿不应把组件放进移除列表: %+v", f4.Items)
	}

	logs, _ := db.ListItemDamageLogs(ctx, uid, 50)
	seen := map[string]int{}
	for _, entry := range logs {
		seen[entry.Event]++
	}
	if seen[damageEventLostOnRodBreak] != 0 {
		t.Fatalf("成长件断竿不应记 lost_on_rod_break，实得 %d", seen[damageEventLostOnRodBreak])
	}
}

// 关掉豁免开关后必须完全回到官服口径：耐久 0、组件销毁。
func TestGrowthRodBreakSwitchOffKeepsOfficialShape(t *testing.T) {
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
	bobber, err := srv.CreateItem(ctx, uid, "bob_25084", "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.GrowthItemInsert(ctx, store.GrowthItem{
		GUID: rod.GUID, UID: uid, SetKey: "growth_pole", Role: growthRoleRod,
	}); err != nil {
		t.Fatal(err)
	}
	setTunableForTest(t, srv, tunableGrowthItemIndestructible, "0")
	if got := srv.tunableF(tunableGrowthItemIndestructible, 1); got != 0 {
		t.Fatalf("开关未生效：tunableF=%.2f", got)
	}

	sess := newProbeSess(srv)
	mountGearForTest(t, srv, sess, rod.GUID, line.GUID)
	mountGearForTest(t, srv, sess, rod.GUID, hook.GUID)
	mountGearForTest(t, srv, sess, rod.GUID, bobber.GUID)
	setTestLineLengthM(t, db, line.GUID, 50)

	tree := rigRequestTree(t, proto.Guid(rod.GUID), proto.Guid(rod.GUID), proto.U32(1), proto.Null())
	hctx := &Ctx{Sess: sess, Main: 17, Sub: 9, Tree: tree,
		Body:     lineBreakBodyWithLineOut(0),
		DecodeOK: true, ExpectResponse: true, Log: logging.Discard()}
	if _, ok := hDynamicRodBreak(hctx); !ok {
		t.Fatal("17/9 断竿未处理")
	}
	if got := mustItem(t, db, rod.GUID).Durability; got != 0 {
		t.Fatalf("关掉豁免后耐久应归零，实得 %.4f", got)
	}
	if row, _ := db.GetItemByGUID(ctx, hook.GUID); row != nil {
		t.Fatal("关掉豁免后钩应照官服口径销毁")
	}
}
