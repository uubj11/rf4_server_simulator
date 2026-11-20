package gameserver

// 渔轮损坏回归：
// TestReelSlipWearsFrictionWasherOut 验证泄力打滑磨的是摩擦片(cat9)制动力
// csDragForce，本体耐久作为零件综合一起降；磨穿后泄力失效（最大泄力归零）。
// TestBrokenReelBlocksCastRig 验证 rigBrokenReelGUID 只对已装配且耐久归零的轮子生效。

import (
	"context"
	"testing"
	"time"

	"rf4server/internal/logging"
	"rf4server/internal/proto"
)

// mountReelOnRod 用 17/2 把轮子装到竿上，返回装配后的轮子行。
func mountReelOnRod(t *testing.T, srv *Server, rodGUID, reelGUID string) {
	t.Helper()
	sess := newProbeSess(srv)
	tree := rigRequestTree(t, proto.U32(0), proto.Guid(rodGUID), proto.U32(0),
		proto.Guid(reelGUID), proto.U32(0))
	body, ok := hRodSetComp(&Ctx{Sess: sess, Main: 17, Sub: 2, Tree: tree,
		DecodeOK: true, Log: logging.Discard()})
	if !ok || len(body) == 0 {
		t.Fatal("17/2 装渔轮未处理")
	}
}

func TestReelSlipWearsFrictionWasherOut(t *testing.T) {
	t.Parallel()
	srv, db := newStaticRigTestServer(t)
	ctx := context.Background()
	const uid = int64(1000001)

	rod, err := srv.CreateItem(ctx, uid, "feeder_10125_FD360", "backpack", 1)
	if err != nil {
		t.Fatalf("造竿失败: %v", err)
	}
	reel, err := srv.CreateItem(ctx, uid, "bclp_5004", "backpack", 1)
	if err != nil {
		t.Fatalf("造轮失败: %v", err)
	}
	mountReelOnRod(t, srv, rod.GUID, reel.GUID)

	initial, perr := srv.syncReelPerformance(ctx, reel)
	if perr != nil || initial.BaseDragKg <= 0 {
		t.Fatalf("轮子应有摩擦片制动力: %+v err=%v", initial, perr)
	}

	// 泄力打滑磨的是摩擦片（基准 reelSlipWear=0.01%），本体耐久作为零件综合一起降。
	// 预置到只差两次，仍走真实的连续磨损链。
	seedFrictionWasherNearWorn(t, srv, ctx, reel.GUID)
	first, derr := srv.damageReelSlip(ctx, uid, rod.GUID, reel.GUID)
	if derr != nil {
		t.Fatalf("第 1 次打滑失败: %v", derr)
	}
	if first.FrictionWorn {
		t.Fatal("预置后第一次打滑不应提前磨穿")
	}
	last, derr := srv.damageReelSlip(ctx, uid, rod.GUID, reel.GUID)
	if derr != nil {
		t.Fatalf("第 2 次打滑失败: %v", derr)
	}
	if !last.FrictionWorn {
		t.Fatal("连续打滑后摩擦片仍未磨穿")
	}
	if last == nil || last.ReelBroken {
		t.Fatalf("摩擦片磨穿只应标记 FrictionWorn，不应标记 ReelBroken: %+v", last)
	}

	row, err := db.GetItemByGUID(ctx, reel.GUID)
	if err != nil || row == nil {
		t.Fatalf("读取轮子失败: %v", err)
	}
	// 摩擦片是熔断件：烧穿之后本体耐久必须归零，客户端才会给红框、
	// 轮子才会收不了线、才必须去换片或进修理。
	if row.Durability != 0 {
		t.Fatalf("摩擦片烧穿后本体耐久应归零，实得 %v", row.Durability)
	}

	// 泄力失效：基础刹车力与锁轮承力都归零，重新同步后仍为 0。
	// 注意用重新从库读取的 row（Fresh FieldsJSON），不能用 CreateItem 的旧指针。
	perf, perr := srv.syncReelPerformance(ctx, row)
	if perr != nil {
		t.Fatalf("重建轮子物理参数失败: %v", perr)
	}
	if perf.BaseDragKg > 1e-9 || perf.LockedDragKg > 1e-9 {
		t.Fatalf("摩擦片磨穿后泄力应失效，实得 base=%v locked=%v", perf.BaseDragKg, perf.LockedDragKg)
	}
}

func TestBrokenReelBlocksCastRig(t *testing.T) {
	t.Parallel()
	srv, db := newStaticRigTestServer(t)
	ctx := context.Background()
	const uid = int64(1000001)

	rod, err := srv.CreateItem(ctx, uid, "feeder_10125_FD360", "backpack", 1)
	if err != nil {
		t.Fatalf("造竿失败: %v", err)
	}
	reel, err := srv.CreateItem(ctx, uid, "bclp_5004", "backpack", 1)
	if err != nil {
		t.Fatalf("造轮失败: %v", err)
	}
	mountReelOnRod(t, srv, rod.GUID, reel.GUID)

	// 完好轮子不拦抛竿。
	if got := srv.rigBrokenReelGUID(ctx, uid, rod.GUID); got != "" {
		t.Fatalf("完好轮子不应拦抛竿，实得 %q", got)
	}
	// 未装轮子的竿也不拦（手竿没有轮）。
	handRod, err := srv.CreateItem(ctx, uid, "tele_10012_tl16", "backpack", 1)
	if err != nil {
		t.Fatalf("造手竿失败: %v", err)
	}
	if got := srv.rigBrokenReelGUID(ctx, uid, handRod.GUID); got != "" {
		t.Fatalf("无轮竿不应拦抛竿，实得 %q", got)
	}

	if err := db.SetItemDurability(ctx, reel.GUID, 0); err != nil {
		t.Fatalf("写耐久失败: %v", err)
	}
	if got := srv.rigBrokenReelGUID(ctx, uid, rod.GUID); got != reel.GUID {
		t.Fatalf("耐久归零的轮子应拦抛竿，实得 %q", got)
	}
	// 别人的竿不认。
	if got := srv.rigBrokenReelGUID(ctx, int64(1000002), rod.GUID); got != "" {
		t.Fatalf("非本人竿不应判为损坏，实得 %q", got)
	}
}

func TestBrokenReelBlocksBiteScheduling(t *testing.T) {
	t.Parallel()
	srv, db := newStaticRigTestServer(t)
	ctx := context.Background()
	const uid = int64(1000001)

	rod, err := srv.CreateItem(ctx, uid, "feeder_10125_FD360", "backpack", 1)
	if err != nil {
		t.Fatalf("造竿失败: %v", err)
	}
	reel, err := srv.CreateItem(ctx, uid, "bclp_5004", "backpack", 1)
	if err != nil {
		t.Fatalf("造轮失败: %v", err)
	}
	mountReelOnRod(t, srv, rod.GUID, reel.GUID)
	if err := db.SetItemDurability(ctx, reel.GUID, 0); err != nil {
		t.Fatalf("写耐久失败: %v", err)
	}

	sess := newProbeSess(srv)
	sess.Userid = uid
	st := srv.stateFor(uid)
	st.mu.Lock()
	st.GearGuid = rod.GUID
	st.mu.Unlock()
	if srv.scheduleBiteState(sess, st, false, true) {
		t.Fatal("损坏渔轮不应继续排咬钩")
	}
	st.mu.Lock()
	armed := st.biteTimer != nil
	st.mu.Unlock()
	if armed {
		t.Fatal("损坏渔轮不应留下咬钩定时器")
	}
}

func TestBrokenReelBlocksPendingBitePush(t *testing.T) {
	t.Parallel()
	srv, db := newStaticRigTestServer(t)
	ctx := context.Background()
	const uid = int64(1000001)

	rod, err := srv.CreateItem(ctx, uid, "feeder_10125_FD360", "backpack", 1)
	if err != nil {
		t.Fatalf("造竿失败: %v", err)
	}
	reel, err := srv.CreateItem(ctx, uid, "bclp_5004", "backpack", 1)
	if err != nil {
		t.Fatalf("造轮失败: %v", err)
	}
	mountReelOnRod(t, srv, rod.GUID, reel.GUID)
	if err := db.SetItemDurability(ctx, reel.GUID, 0); err != nil {
		t.Fatalf("写耐久失败: %v", err)
	}

	sess, peer := newTunnelProbe(t, srv)
	sess.Userid = uid
	srv.mu.Lock()
	srv.sessions[sess.ID] = sess
	srv.mu.Unlock()
	t.Cleanup(func() {
		srv.mu.Lock()
		delete(srv.sessions, sess.ID)
		srv.mu.Unlock()
	})

	fish := &RolledFish{
		FishGUID: "11111111111111111111111111111111",
		Species:  "ruffe",
		WeightG:  100,
	}
	st := srv.stateFor(uid)
	st.mu.Lock()
	st.GearGuid = rod.GUID
	st.CastActive = true
	st.Pending = fish
	st.castGeneration = 1
	generation := st.castGeneration
	st.mu.Unlock()

	pushed := receiveTestPush(peer, 14, 14)
	srv.pushPendingBite(sess, st, fish, rod.GUID, generation)
	select {
	case env := <-pushed:
		t.Fatalf("损坏渔轮不应再推送 14/14，实得 %+v", env)
	case <-time.After(300 * time.Millisecond):
	}
}

// TestBurnedFrictionWasherCountsReelBreak 磨穿摩擦片也是一次损坏：轮体耐久从
// 正数跌到 0，统计页的 reel_breaks 必须涨 1，即使这条通道不标记 ReelBroken。
func TestBurnedFrictionWasherCountsReelBreak(t *testing.T) {
	t.Parallel()
	srv, db := newStaticRigTestServer(t)
	ctx := context.Background()
	const uid = int64(1000001)

	rod, err := srv.CreateItem(ctx, uid, "feeder_10125_FD360", "backpack", 1)
	if err != nil {
		t.Fatalf("造竿失败: %v", err)
	}
	reel, err := srv.CreateItem(ctx, uid, "bclp_5004", "backpack", 1)
	if err != nil {
		t.Fatalf("造轮失败: %v", err)
	}
	mountReelOnRod(t, srv, rod.GUID, reel.GUID)

	if _, perr := srv.syncReelPerformance(ctx, reel); perr != nil {
		t.Fatalf("同步轮子物理参数失败: %v", perr)
	}
	seedFrictionWasherNearWorn(t, srv, ctx, reel.GUID)

	if _, derr := srv.damageReelSlip(ctx, uid, rod.GUID, reel.GUID); derr != nil {
		t.Fatalf("第 1 次打滑失败: %v", derr)
	}
	last, derr := srv.damageReelSlip(ctx, uid, rod.GUID, reel.GUID)
	if derr != nil {
		t.Fatalf("第 2 次打滑失败: %v", derr)
	}
	if !last.FrictionWorn {
		t.Fatal("连续打滑后摩擦片仍未磨穿")
	}
	if last.ReelBroken {
		t.Fatalf("摩擦片磨穿不应标记 ReelBroken: %+v", last)
	}

	row, err := db.GetItemByGUID(ctx, reel.GUID)
	if err != nil || row == nil {
		t.Fatalf("读取轮子失败: %v", err)
	}
	if row.Durability != 0 {
		t.Fatalf("摩擦片烧穿后本体耐久应归零，实得 %v", row.Durability)
	}

	reelBreaks := func() int64 {
		t.Helper()
		var v int64
		if err := db.SQL.QueryRowContext(ctx,
			`SELECT COALESCE((SELECT value FROM player_counters WHERE uid=? AND key='reel_breaks'),0)`,
			uid).Scan(&v); err != nil {
			t.Fatal(err)
		}
		return v
	}
	if got := reelBreaks(); got != 1 {
		t.Fatalf("磨穿摩擦片后 reel_breaks=%d，期望 1", got)
	}

	// 耐久已经是 0 了，再打滑不属于「本次打到 0」，不得重复计数。
	if _, derr := srv.damageReelSlip(ctx, uid, rod.GUID, reel.GUID); derr != nil {
		t.Fatalf("第 3 次打滑失败: %v", derr)
	}
	if got := reelBreaks(); got != 1 {
		t.Fatalf("耐久已归零后再打滑 reel_breaks=%d，期望仍为 1", got)
	}
}

// TestReelBreakCounterFailureRollsBackDurability 计数写失败必须连耐久归零一起
// 回滚：否则轮子坏了但 reel_breaks 没涨，重试又因耐久已是 0 被幂等守卫挡住，
// 这笔账永久补不回来。
func TestReelBreakCounterFailureRollsBackDurability(t *testing.T) {
	t.Parallel()
	srv, db := newStaticRigTestServer(t)
	ctx := context.Background()
	const uid = int64(1000001)

	rod, err := srv.CreateItem(ctx, uid, "feeder_10125_FD360", "backpack", 1)
	if err != nil {
		t.Fatalf("造竿失败: %v", err)
	}
	reel, err := srv.CreateItem(ctx, uid, "bclp_5004", "backpack", 1)
	if err != nil {
		t.Fatalf("造轮失败: %v", err)
	}
	mountReelOnRod(t, srv, rod.GUID, reel.GUID)

	before, err := db.GetItemByGUID(ctx, reel.GUID)
	if err != nil || before == nil {
		t.Fatalf("读取轮子失败: %v", err)
	}
	if before.Durability <= 0 {
		t.Fatalf("新轮子耐久应大于 0，实得 %v", before.Durability)
	}

	// 直接移除计数器表，让 BumpPlayerCounter 报 no such table。不改生产代码
	// 留注入点、也不靠 authorizer（它只拦静态配置表），测的就是真实写入路径。
	if _, err := db.SQL.ExecContext(ctx, `DROP TABLE player_counters`); err != nil {
		t.Fatalf("移除计数器表失败: %v", err)
	}

	if _, err := srv.damageReelLockOverload(ctx, uid, rod.GUID, reel.GUID); err == nil {
		t.Fatal("计数器写入失败时断轮应返回错误，实际成功")
	}

	after, err := db.GetItemByGUID(ctx, reel.GUID)
	if err != nil || after == nil {
		t.Fatalf("回滚后读取轮子失败: %v", err)
	}
	if after.Durability == 0 {
		t.Fatal("计数失败后耐久仍被归零：计数写入没有和耐久同事务")
	}
	if after.Durability != before.Durability {
		t.Fatalf("计数失败必须回滚耐久，期望 %v，实得 %v", before.Durability, after.Durability)
	}

	var logs int64
	if err := db.SQL.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM item_damage_log WHERE uid=?`, uid).Scan(&logs); err != nil {
		t.Fatal(err)
	}
	if logs != 0 {
		t.Fatalf("计数失败必须同时回滚损伤日志，实得 %d 行", logs)
	}
}

// TestReelWearCounterFailureRollsBackDurability 覆盖泄力打滑磨穿路径的计数失败：
// 计数器写不进去时，轮体耐久和损伤日志也必须随事务回滚。
func TestReelWearCounterFailureRollsBackDurability(t *testing.T) {
	t.Parallel()
	srv, db := newStaticRigTestServer(t)
	ctx := context.Background()
	const uid = int64(1000001)

	rod, err := srv.CreateItem(ctx, uid, "feeder_10125_FD360", "backpack", 1)
	if err != nil {
		t.Fatalf("造竿失败: %v", err)
	}
	reel, err := srv.CreateItem(ctx, uid, "bclp_5004", "backpack", 1)
	if err != nil {
		t.Fatalf("造轮失败: %v", err)
	}
	mountReelOnRod(t, srv, rod.GUID, reel.GUID)
	if _, perr := srv.syncReelPerformance(ctx, reel); perr != nil {
		t.Fatalf("同步轮子物理参数失败: %v", perr)
	}
	seedFrictionWasherNearWorn(t, srv, ctx, reel.GUID)
	if _, derr := srv.damageReelSlip(ctx, uid, rod.GUID, reel.GUID); derr != nil {
		t.Fatalf("第一次打滑失败: %v", derr)
	}

	before, err := db.GetItemByGUID(ctx, reel.GUID)
	if err != nil || before == nil {
		t.Fatalf("读取磨损前轮子失败: %v", err)
	}
	perfBefore, err := db.GetReelPerformance(ctx, reel.GUID)
	if err != nil || perfBefore == nil {
		t.Fatalf("读取磨损前性能失败: %v", err)
	}
	var logsBefore int64
	if err := db.SQL.QueryRowContext(ctx, `SELECT COUNT(*) FROM item_damage_log WHERE uid=?`, uid).Scan(&logsBefore); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL.ExecContext(ctx, `DROP TABLE player_counters`); err != nil {
		t.Fatalf("移除计数器表失败: %v", err)
	}
	if _, err := srv.damageReelSlip(ctx, uid, rod.GUID, reel.GUID); err == nil {
		t.Fatal("计数写入失败时磨穿应返回错误，实际成功")
	}

	after, err := db.GetItemByGUID(ctx, reel.GUID)
	if err != nil || after == nil {
		t.Fatalf("回滚后读取轮子失败: %v", err)
	}
	if after.Durability != before.Durability {
		t.Fatalf("计数失败必须回滚磨损耐久，期望 %v，实得 %v", before.Durability, after.Durability)
	}
	var logsAfter int64
	if err := db.SQL.QueryRowContext(ctx, `SELECT COUNT(*) FROM item_damage_log WHERE uid=?`, uid).Scan(&logsAfter); err != nil {
		t.Fatal(err)
	}
	if logsAfter != logsBefore {
		t.Fatalf("计数失败必须回滚损伤日志，期望 %d 行，实得 %d 行", logsBefore, logsAfter)
	}
	perfAfter, err := db.GetReelPerformance(ctx, reel.GUID)
	if err != nil || perfAfter == nil {
		t.Fatalf("回滚后读取性能失败: %v", err)
	}
	if perfAfter.Durability != perfBefore.Durability || perfAfter.BaseDragKg != perfBefore.BaseDragKg {
		t.Fatalf("计数失败必须回滚性能表，before=%+v after=%+v", perfBefore, perfAfter)
	}
}

// TestFightWearCounterFailureRollsBackDurability 覆盖搏鱼受力结算磨穿路径：
// 计数失败时，轮体字段和性能表也必须随事务回滚。
func TestFightWearCounterFailureRollsBackDurability(t *testing.T) {
	t.Parallel()
	srv, db, ctx, reelGUID, perf := wearReelFixture(t)
	rated, ok := srv.reelWasherRatedFromRow(ctx, wearReelRow(t, db, ctx, reelGUID))
	if !ok || rated <= 0 {
		t.Fatal("读不到摩擦片额定制动力")
	}
	w := newFightAccount(rated, perf, reelGUID)
	w.SlipWear = 1

	before := wearReelRow(t, db, ctx, reelGUID)
	perfBefore, err := db.GetReelPerformance(ctx, reelGUID)
	if err != nil || perfBefore == nil {
		t.Fatalf("读取搏鱼磨损前性能失败: %v", err)
	}
	var logsBefore int64
	if err := db.SQL.QueryRowContext(ctx, `SELECT COUNT(*) FROM item_damage_log WHERE uid=?`, wearTestUID).Scan(&logsBefore); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL.ExecContext(ctx, `DROP TABLE player_counters`); err != nil {
		t.Fatalf("移除计数器表失败: %v", err)
	}
	if srv.applyFightReelWear(ctx, wearTestUID, w, "14/4", true) {
		t.Fatal("计数写入失败时搏鱼磨损应返回 false，实际成功")
	}

	after := wearReelRow(t, db, ctx, reelGUID)
	if after.Durability != before.Durability {
		t.Fatalf("计数失败必须回滚搏鱼磨损耐久，期望 %v，实得 %v", before.Durability, after.Durability)
	}
	var logsAfter int64
	if err := db.SQL.QueryRowContext(ctx, `SELECT COUNT(*) FROM item_damage_log WHERE uid=?`, wearTestUID).Scan(&logsAfter); err != nil {
		t.Fatal(err)
	}
	if logsAfter != logsBefore {
		t.Fatalf("计数失败必须不新增损伤日志，期望 %d 行，实得 %d 行", logsBefore, logsAfter)
	}
	perfAfter, err := db.GetReelPerformance(ctx, reelGUID)
	if err != nil || perfAfter == nil {
		t.Fatalf("回滚后读取搏鱼性能失败: %v", err)
	}
	if perfAfter.Durability != perfBefore.Durability || perfAfter.BaseDragKg != perfBefore.BaseDragKg {
		t.Fatalf("计数失败必须回滚搏鱼性能表，before=%+v after=%+v", perfBefore, perfAfter)
	}
}
