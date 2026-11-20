package gameserver

import (
	"context"
	"math"
	"strconv"
	"testing"
	"time"

	"rf4server/internal/logging"
	"rf4server/internal/proto"
	"rf4server/internal/store"
)

func TestCastBaseDistanceProfiles(t *testing.T) {
	cases := []struct {
		name    string
		rigType int64
		want    float32
	}{
		{name: "float", rigType: 1, want: 7.5461488},
		{name: "bottom", rigType: 12, want: 7.5461488},
		{name: "lure", rigType: 5, want: 7.1598907},
	}
	for _, tc := range cases {
		got := math.Float32frombits(castBaseDistanceBits(tc.rigType))
		if math.Abs(float64(got-tc.want)) > 1e-6 {
			t.Fatalf("%s: got %.7f want %.7f", tc.name, got, tc.want)
		}
	}
}

func TestReelCastFrictionImprovesWithBearingOilAndDurability(t *testing.T) {
	wornBasic := reelCastFriction(10, 1, 15, 1, 0.4)
	newBasic := reelCastFriction(10, 1, 15, 1, 1.0)
	premium := reelCastFriction(100, 1, 100, 1, 1.0)
	if !(wornBasic > newBasic && newBasic > premium) {
		t.Fatalf("摩擦系数应随轴承/油品/耐久改善而下降: worn=%f basic=%f premium=%f",
			wornBasic, newBasic, premium)
	}
	if math.Abs(premium-0.05) > 1e-9 {
		t.Fatalf("满品质轮子摩擦=%f，期望0.05", premium)
	}
	// 零件自身磨旧也要体现在抛投手感上：油干了、轴承旧了都更甩不出去。
	dryOil := reelCastFriction(100, 1, 100, 0, 1.0)
	wornBearing := reelCastFriction(100, 0, 100, 1, 1.0)
	if !(dryOil > premium && wornBearing > premium) {
		t.Fatalf("轮油干涸或轴承磨旧应更难抛: 干油=%f 旧轴承=%f 全新=%f", dryOil, wornBearing, premium)
	}
}

func TestCastWeightAndSkillFactors(t *testing.T) {
	// 竿 Test 区间 2-10g：区间内越重越远，10g 达到峰值。
	light := rodTestWeightFactor(4, 2, 10)
	mid := rodTestWeightFactor(7, 2, 10)
	peak := rodTestWeightFactor(10, 2, 10)
	if !(mid > light && peak > mid) {
		t.Fatalf("Test 区间内载荷越重应抛得越远: 4g=%f 7g=%f 10g=%f", light, mid, peak)
	}
	// 超过 Test 上限后竿梢带不动，距离必须回落。
	over := rodTestWeightFactor(20, 2, 10)
	if !(over < peak) {
		t.Fatalf("超出 Test 上限后应衰减: 10g=%f 20g=%f", peak, over)
	}
	if !(castSkillFactor(100, 0) > castSkillFactor(50, 0) &&
		castSkillFactor(50, 10) > castSkillFactor(50, 0)) {
		t.Fatal("熟练度和已学习技能节点应提高抛投效率")
	}
}

func TestCastRodWithoutTestDataUsesConservativeFactor(t *testing.T) {
	// 静态竿库中约两成竿未登记 Test 区间；缺数据时按偏保守的固定
	// 折减处理，不应与登记了合适 Test 区间的竿同等对待。
	missing := rodTestWeightFactor(8, 0, 0)
	if missing != 0.85 {
		t.Fatalf("缺 Test 区间的竿应固定折减 0.85，实得 %f", missing)
	}
	if registered := rodTestWeightFactor(8, 5, 30); registered >= missing {
		t.Fatalf("登记了合适 Test 区间的竿（系数 %.3f）应低于缺数据竿的固定折减 %.2f",
			registered, missing)
	}
	// 负值同样视为未登记。
	if rodTestWeightFactor(8, -1, 0) != 0.85 {
		t.Fatal("testMaxG 为负也应按缺数据处理")
	}
}

func TestCastOverTestPayloadShortensDistance(t *testing.T) {
	peak := rodTestWeightFactor(10, 2, 10)
	twenty := rodTestWeightFactor(20, 2, 10)
	forty := rodTestWeightFactor(40, 2, 10)
	hundred := rodTestWeightFactor(100, 2, 10)
	if !(peak > twenty && twenty > forty && forty > hundred) {
		t.Fatalf("超 Test 后应单调衰减: 10g=%.4f 20g=%.4f 40g=%.4f 100g=%.4f",
			peak, twenty, forty, hundred)
	}
	if hundred < 0.15 {
		t.Fatalf("超重 10 倍时系数过低，距离会被压到失去意义: %.4f", hundred)
	}
	// 数值锚点：衰减只与超重倍率 r=payload/testMax 有关，相对峰值恒为
	// 1/(1+0.70*(r-1))。这里故意用字面量 0.70 而不是 rodTestOverloadDecay，
	// 这样以后微调常数会立刻被断言拦住，而不是断言跟着一起漂。
	for _, tc := range []struct {
		payload float64
		want    float64
	}{
		{15, 1.0 / (1 + 0.70*0.5)}, // 1.5× → 峰值 74.1%
		{20, 1.0 / (1 + 0.70*1.0)}, // 2×   → 峰值 58.8%
		{30, 1.0 / (1 + 0.70*2.0)}, // 3×   → 峰值 41.7%
	} {
		if ratio := rodTestWeightFactor(tc.payload, 2, 10) / peak; math.Abs(ratio-tc.want) > 1e-9 {
			t.Fatalf("%.0fg 应为峰值的 %.4f，实得 %.4f", tc.payload, tc.want, ratio)
		}
	}
	// 10× 撞的是 clampFloat 的绝对下限 0.22，不是"剩峰值的 22%"。
	if math.Abs(hundred-0.22) > 1e-9 {
		t.Fatalf("超重 10 倍应落到绝对下限 0.22，实得 %.4f", hundred)
	}
	if ratio := rodOverTestRatio(25, 10); math.Abs(ratio-2.5) > 1e-9 {
		t.Fatalf("超重倍率应为 2.5，实得 %f", ratio)
	}
	if ratio := rodOverTestRatio(25, 0); ratio != 0 {
		t.Fatalf("竿未登记 Test 时应返回 0，实得 %f", ratio)
	}
	// 同一条竿（Test 5-30g）：合规载荷必须比超重载荷抛得远。
	underDistance := castCalibrationDistance(30, 5)
	overDistance := castCalibrationDistance(120, 5)
	if overDistance >= underDistance {
		t.Fatalf("120g 配 30g 上限的竿不应抛得比 30g 更远: %.2fm vs %.2fm",
			overDistance, underDistance)
	}
	for _, payload := range []float64{7, 18, 40, 400, 4000} {
		d, factors := calculateCastDistance(CastDistanceContext{
			RigType: 5, CastAction: 1, CastPower: 1,
			RodTestMinG: 2, RodTestMaxG: 10, PayloadWeightG: payload,
		})
		if math.IsNaN(d) || math.IsInf(d, 0) || d < castMinPoweredM {
			t.Fatalf("载荷 %.0fg 计算异常: distance=%f factors=%+v", payload, d, factors)
		}
	}
}

func castCalibrationDistance(payloadG float64, spoolSize int64) float64 {
	d, _ := calculateCastDistance(CastDistanceContext{
		RigType: 5, CastAction: 1, CastPower: 1,
		RodLengthM: 3.0, RodAction: 2, RodTestMinG: 5, RodTestMaxG: 30,
		PayloadWeightG: payloadG, ReelEfficiency: 0.95, ReelType: 1,
		SpoolSize: spoolSize, SpoolFill: 0.8, LineDiameterMM: 0.20,
		LineStretch: 0.001, LineMaterial: 3,
		PlayerPrimary: defaultPlayerConditionPrimary, PlayerSecondary: defaultPlayerConditionSecondary,
		SkillMastery: 50,
	})
	return d
}

func TestCastCalibrationBands(t *testing.T) {
	// 标称随竿 Test 分档（2026-11-17，specs/changes/cast-rod-test-scaling）
	// 后的区间：本 helper 竿 Test 上限 30g，标称 23.06×(30/4)^0.41 ≈ 52.9；
	// 8g 得 33.68（6000 大杯）/ 39.30（2000 小杯），10g 再高约 1%。
	for _, payload := range []float64{8, 10} {
		largeSpool := castCalibrationDistance(payload, 5) // 6000
		smallSpool := castCalibrationDistance(payload, 2) // 2000
		if largeSpool < 33.4 || largeSpool > 34.4 {
			t.Fatalf("6000线杯 %.0fg 距离=%0.2f，期望33.4～34.4米区间", payload, largeSpool)
		}
		if smallSpool < 39.0 || smallSpool > 40.0 {
			t.Fatalf("2000线杯 %.0fg 距离=%0.2f，期望39.0～40.0米区间", payload, smallSpool)
		}
		if smallSpool <= largeSpool+5 {
			t.Fatalf("小线杯应比6000线杯远至少5米: 小杯=%0.2f 大杯=%0.2f", smallSpool, largeSpool)
		}
	}
}

// 状态修正器边界：只按精力比例，0 值是真实空状态，落到 0.70 下限；
// 不再重置为默认值；饱食度不参与。
func TestCastPlayerStateFactorZeroStateUsesFloor(t *testing.T) {
	if got := castPlayerStateFactor(0); math.Abs(got-0.70) > 1e-9 {
		t.Fatalf("空状态应落到 0.70 下限: %v", got)
	}
	if got := castPlayerStateFactor(defaultPlayerConditionPrimary); math.Abs(got-1.0) > 1e-9 {
		t.Fatalf("精力满即应满抛距（饱食度不参与）: %v", got)
	}
	if got := castPlayerStateFactor(1.0); math.Abs(got-1.03) > 1e-9 {
		t.Fatalf("超上限应钳到 1.03: %v", got)
	}
	// 精力 0.60/0.85 ≈ 0.7059，仍在下限之上。
	if got := castPlayerStateFactor(0.60); math.Abs(got-0.60/defaultPlayerConditionPrimary) > 1e-9 {
		t.Fatalf("精力 0.60 应按比例 0.7059: %v", got)
	}
}

// 标称随竿 Test 分档（2026-11-17）后，4.2m/Test30 的高阶路亚装配
// + 满熟练 + 10% 技能上限约 56.8m。
func TestCastHighEndGearCeilingAfterRodTestScaling(t *testing.T) {
	d, _ := calculateCastDistance(CastDistanceContext{
		RigType: 5, CastAction: 1, CastPower: 1,
		RodLengthM: 4.2, RodAction: 4, RodTestMinG: 5, RodTestMaxG: 30,
		PayloadWeightG: 8, ReelEfficiency: 0.95, ReelType: 1,
		SpoolSize: 1, SpoolFill: 1.0, LineDiameterMM: 0.10,
		LineStretch: 0.001, LineMaterial: 3,
		PlayerPrimary: defaultPlayerConditionPrimary, PlayerSecondary: defaultPlayerConditionSecondary,
		SkillMastery: 100, SkillCastPct: 10,
	})
	if d < 56.4 || d > 57.2 {
		t.Fatalf("高阶装备上限应落在 ~56.8m，实得%0.2f", d)
	}
}

// TestCastFreshwaterLureOfficialAnchorBand 复现实抓 2026-11-13 21:28 的 UL
// 配置（1.8m 枪柄 + 水滴轮 + 3.33g 载荷，Test 1-8g，0.28 编织线，浅杯 8）
// 与满级投距技能（708×7 + 717×5 = +60%）、熟练度 95。Test 上限 8g 档标称
// 23.06×(8/4)^0.41 ≈ 30.6，乘实抓因子链 0.9085 ≈ 27.45m——与官服
// 「UL 满技能未改装约 27m」锚点一致。
func TestCastFreshwaterLureOfficialAnchorBand(t *testing.T) {
	d, factors := calculateCastDistance(CastDistanceContext{
		RigType: 5, CastAction: 1, CastPower: 1,
		RodLengthM: 1.8, RodAction: 2, RodStiffness: 2,
		RodTestMinG: 1, RodTestMaxG: 8, PayloadWeightG: 3.33,
		ReelEfficiency: 0.857, ReelType: 2, SpoolSize: 8, SpoolFill: 1,
		LineDiameterMM: 0.28, LineStretch: 0.01, LineMaterial: 3,
		LineRunningWeightG: 0.078,
		PlayerPrimary:      defaultPlayerConditionPrimary,
		SkillMastery:       95, SkillCastPct: 60,
	})
	if math.IsNaN(d) || d < 27.6 || d > 28.1 {
		t.Fatalf("Test8 档 UL 配置应落在 ~27.82m（官服 UL 锚点 27m），实得 %.2f factors=%+v",
			d, factors)
	}
}

// TestCastOfficialULAnchor 锁定官服 UL 锚点：2m/Test 0.5-4g 轻装 + 3.53g
// 拟饵 + 水滴轮（效率 0.948）+ 0.24 编织线 + 满熟练 + 满技能（+60%），
// 官服未改装约 27m（玩家实抛 100% 路亚熟练、技能点满）。
func TestCastOfficialULAnchor(t *testing.T) {
	d, factors := calculateCastDistance(CastDistanceContext{
		RigType: 7, CastAction: 1, CastPower: 1,
		RodLengthM: 2.0, RodAction: 2, RodStiffness: 2,
		RodTestMinG: 0.5, RodTestMaxG: 4, PayloadWeightG: 3.53,
		ReelEfficiency: 0.948, ReelType: 2, SpoolSize: 8, SpoolFill: 1,
		LineDiameterMM: 0.24, LineLengthM: 300, LineStretch: 0.01, LineMaterial: 3,
		LineRunningWeightG: 0.063,
		PlayerPrimary:      defaultPlayerConditionPrimary,
		PlayerSecondary:    defaultPlayerConditionSecondary,
		SkillMastery:       100, SkillCastPct: 60,
	})
	if d < 26.9 || d > 27.1 {
		t.Fatalf("UL 官服锚点应落在 27.0m，实得 %.2f factors=%+v", d, factors)
	}
}

// TestCastMHAnchorRegression 锁定中重装档的回归基线：2.7m/Test 15-49g + 63g
// 载荷 + 6000 线杯，满熟练满技能下约 50m。
//
// 注意：这个 50m **不是官服实测锚点**。它来自早期对维护者一句「49g 杆子封顶
// 50」的误读——那句说的是竿的 Test 上限（该竿 Test 15-49g，上限约 50g），
// 不是抛投距离。该档的官服实测值至今没有取得，因此本断言只作为指数曲线的
// 内部回归基线，防止后续改动意外改变这一档的落点；补齐官服实测后，需要用
// 真实锚点重新反解 castNominalTestExp，并同步调整本断言。
func TestCastMHAnchorRegression(t *testing.T) {
	d, _ := calculateCastDistance(CastDistanceContext{
		RigType: 7, CastAction: 1, CastPower: 1,
		RodLengthM: 2.7, RodAction: 2, RodStiffness: 2,
		RodTestMinG: 15, RodTestMaxG: 49, PayloadWeightG: 63,
		ReelEfficiency: 0.95, ReelType: 1, SpoolSize: 5, SpoolFill: 1,
		LineDiameterMM: 0.32, LineStretch: 0.01, LineMaterial: 3,
		LineRunningWeightG: 0.08,
		PlayerPrimary:      defaultPlayerConditionPrimary,
		PlayerSecondary:    defaultPlayerConditionSecondary,
		SkillMastery:       100, SkillCastPct: 60,
	})
	if d < 49.9 || d > 50.7 {
		t.Fatalf("中重装档回归基线应落在 50m（非官服锚点，见函数注释），实得 %.2f", d)
	}
}

// TestCastNominalDistanceByRigFamily 锁定标称分族：淡水路亚锚点档 23.06
// （竿 Test 上限缺失或低于 4g 时取该档）、海钓路亚 50-54 暂留 45 待补
// 锚点、其余默认族 60。
func TestCastNominalDistanceByRigFamily(t *testing.T) {
	if got := castNominalDistanceM(5); got != 23.06 {
		t.Fatalf("淡水路亚锚点档标称 = %v，期望 23.06", got)
	}
	if got := castNominalDistanceM(22); got != 23.06 {
		t.Fatalf("淡水路亚 rigType22 锚点档标称 = %v，期望 23.06", got)
	}
	if got := castNominalDistanceM(52); got != 45.0 {
		t.Fatalf("海钓路亚标称 = %v，期望 45（待补锚点保留旧值）", got)
	}
	if got := castNominalDistanceM(1); got != 60.0 {
		t.Fatalf("浮钓默认族标称 = %v，期望 60", got)
	}
	if got := castNominalDistanceM(12); got != 60.0 {
		t.Fatalf("底钓默认族标称 = %v，期望 60", got)
	}
}

func TestCastDynamicModifiersAreMonotonic(t *testing.T) {
	if !(castPowerFactor(1) > castPowerFactor(0.5) && castPowerFactor(0.5) > castPowerFactor(0.1)) {
		t.Fatal("抛投力度应单调增加距离")
	}
	if !(castLineDiameterFactor(0.12) > castLineDiameterFactor(0.40)) {
		t.Fatal("更粗主线应缩短距离")
	}
	if !(castLineStretchFactor(0.001) > castLineStretchFactor(0.87)) {
		t.Fatal("高拉伸主线应降低能量传递效率")
	}
	if !(castLineMaterialFactor(3) > castLineMaterialFactor(2)) {
		t.Fatal("编织线低拉伸材质应优于高拉伸材质")
	}
	if !(castLineMassFactor(1) > castLineMassFactor(20)) {
		t.Fatal("更重的运行主线应缩短距离")
	}
	if !(castRodActionFactor(4) > castRodActionFactor(3) && castRodActionFactor(3) > castRodActionFactor(1)) {
		t.Fatal("快调竿梢应小幅提高能量传递")
	}
}

func TestPlayerConditionUsesDatabase(t *testing.T) {
	srv, db := newRigTestServer(t)
	ctx := context.Background()
	const uid = int64(1000001)
	// UpdatedTS 放到未来：无在线跨度，懒计算不推进，纯验证读库路径。
	if err := db.UpsertPlayerCondition(ctx, store.PlayerCondition{
		UID: uid, Primary: 0.62, Secondary: 0.71, Comfort: 1.0, UpdatedTS: float64(time.Now().Unix()) + 3600,
	}); err != nil {
		t.Fatal(err)
	}
	primary, secondary := srv.castPlayerCondition(ctx, uid)
	if math.Abs(primary-0.62) > 1e-9 || math.Abs(secondary-0.71) > 1e-9 {
		t.Fatalf("玩家状态未从 SQLite 读取: primary=%f secondary=%f", primary, secondary)
	}
	got, err := db.EnsurePlayerCondition(ctx, uid)
	if err != nil || got == nil || math.Abs(got.Primary-0.62) > 1e-9 || math.Abs(got.Secondary-0.71) > 1e-9 {
		t.Fatalf("玩家状态未持久化: got=%+v err=%v", got, err)
	}
}

func TestCastDistanceUsesMountedReelAndClip(t *testing.T) {
	srv, db := newRigTestServer(t)
	ctx := context.Background()
	const uid = int64(1000001)
	rod, err := srv.CreateItem(ctx, uid, "feeder_10125_FD360", "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	reel, err := srv.CreateItem(ctx, uid, "spin_5820", "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	line, err := srv.CreateItem(ctx, uid, "mono_40853", "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	if !srv.rigSetType(ctx, uid, rod.GUID, 16) {
		t.Fatal("设置底钓 Method 钓组失败")
	}
	feeder, err := srv.CreateItem(ctx, uid, "feeder_30037", "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	bait, err := srv.CreateItem(ctx, uid, "worm_01", "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	sess := newProbeSess(srv)
	mountGearForTest(t, srv, sess, rod.GUID, reel.GUID)
	mountGearForTest(t, srv, sess, rod.GUID, line.GUID)
	mountGearForTest(t, srv, sess, rod.GUID, feeder.GUID)
	mountGearForTest(t, srv, sess, rod.GUID, bait.GUID)
	if err := db.AddSkillPoints(ctx, uid, 3, 50); err != nil {
		t.Fatal(err)
	}

	p := srv.castDistanceForRig(ctx, uid, rod.GUID, 16)
	if p.ReelGUID != reel.GUID || p.DistanceM <= 10 || p.Friction <= 0 || p.Efficiency >= 1 {
		t.Fatalf("真实轮子抛投参数未生效: %+v", p)
	}
	if p.BaitItemID != "worm_01" || p.BaitWeightG <= 0 {
		t.Fatalf("当前饵未与抛投载荷对齐: bait=%q weight=%f", p.BaitItemID, p.BaitWeightG)
	}
	if p.RodLengthM <= 0 || p.RodTestMaxG <= 0 || p.RigWeightG <= 0 || p.BaitWeightG <= 0 ||
		p.PayloadWeightG <= p.BaitWeightG || p.SkillCategory != 3 || math.Abs(p.SkillMastery-50) > 1e-9 ||
		p.LineDiameterMM <= 0 || p.LineLengthM <= 0 || p.LineLinearDensityGPM <= 0 || p.LineRunningWeightG <= 0 ||
		p.PlayerPrimary <= 0 || p.PlayerSecondary <= 0 {
		t.Fatalf("竿长/锥度/Test/主线/线组/饵/玩家状态/技能数据未完整进入模型: %+v", p)
	}
	if p.SpoolCapacityM <= 0 || p.SpoolFill <= 0 || p.SpoolFill > 1 {
		t.Fatalf("spool capacity/fill missing: capacity=%.3f fill=%.3f", p.SpoolCapacityM, p.SpoolFill)
	}
	if want := math.Min(p.LineLengthM, p.SpoolCapacityM); math.Abs(p.LineLimitM-want) > 1e-9 {
		t.Fatalf("line limit should be capped by spool capacity: line=%.3f capacity=%.3f limit=%.3f want=%.3f", p.LineLengthM, p.SpoolCapacityM, p.LineLimitM, want)
	}
	perf, err := db.GetReelPerformance(ctx, reel.GUID)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(perf.CastFriction-p.Friction) > 1e-9 || math.Abs(perf.CastEfficiency-p.Efficiency) > 1e-9 {
		t.Fatalf("抛投参数未落入 reel_performance: perf=%+v profile=%+v", perf, p)
	}
	if err := db.AddSkillPoints(ctx, uid, 3, 50); err != nil {
		t.Fatal(err)
	}
	mastered := srv.castDistanceForRig(ctx, uid, rod.GUID, 16)
	if mastered.DistanceM <= p.DistanceM || mastered.SkillMastery != 100 {
		t.Fatalf("熟练度未提高抛投距离: 50%%=%+v 100%%=%+v", p, mastered)
	}

	if err := db.UpsertRodSetting(ctx, rod.GUID, reelClipSettingKey, "18"); err != nil {
		t.Fatal(err)
	}
	clipped := srv.castDistanceForRig(ctx, uid, rod.GUID, 16)
	// 卡线不折进 DistanceM（158.f1 全量程，贴卡由客户端按 153.f5 截停），
	// 只落 ClipLimitM 元数据。
	if clipped.ClipLimitM != 18 || math.Abs(clipped.DistanceM-mastered.DistanceM) > 1e-6 {
		t.Fatalf("卡线只应记元数据不改量程: %+v 未卡=%+v", clipped, mastered)
	}
}

// TestReelSpoolCapacityMatchesGameData 锁定客户端 lkgakdhinig.jnidmcfnkgp
// 的分段容量公式。实机 UI 锚点为 Overlord 3RS 20S（spin_5425）0.7mm/34m、
// Adriatica 3000S（spin_5026）0.42mm/91m；其余是公式回归与边界用例。
func TestReelSpoolCapacityMatchesGameData(t *testing.T) {
	cases := []struct {
		name     string
		coef     float64
		diameter float64
		want     float64
	}{
		{"Overlord-3RS-20S/0.70", 100, 0.70, 34},
		{"Adriatica-3000S/0.42", 120, 0.42, 91},
		{"2000/0.14", 100, 0.14, 260},
		{"2000/0.18", 100, 0.18, 171},
		{"2000/0.22", 100, 0.22, 123},
		{"4000/0.20", 180, 0.20, 260},
		{"4000/0.24", 180, 0.24, 192},
		{"4000/0.26", 180, 0.26, 178},
		{"boundary/0.25", 100, 0.25, 100},
		{"boundary/0.80", 100, 0.80, 23},
		{"coarse/1.20", 100, 1.20, 12},
		{"minimum/5m", 1, 0.70, 5},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := reelSpoolCapacityM(tc.coef, tc.diameter)
			if math.Abs(got-tc.want) > 1e-9 {
				t.Fatalf("容量 %.0fm，期望 %.0fm", got, tc.want)
			}
		})
	}
}

// TestReelSpoolCapacityUsesStaticItemAnchors 直接从静态物品读取线杯系数，
// 锁住实机锚点对应的具体轮型和线材，避免把同系列不同系数的 20S 混用。
func TestReelSpoolCapacityUsesStaticItemAnchors(t *testing.T) {
	srv, _ := newRigTestServer(t)
	ctx := context.Background()
	const uid = int64(1000001)
	cases := []struct {
		name, reelID, lineID string
		want                 float64
	}{
		{"Overlord 3RS 20S / 0.70mm", "spin_5425", "wireline_40813", 34},
		{"Adriatica 3000S / 0.42mm", "spin_5026", "wrl_sim_pfc_21LB", 91},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reel, err := srv.CreateItem(ctx, uid, tc.reelID, "backpack", 1)
			if err != nil {
				t.Fatal(err)
			}
			_, _, got := srv.castReelSpoolStats(ctx, reel, tc.lineID)
			if got != tc.want {
				t.Fatalf("静态轮型 %s / 线材 %s 容量=%0.fm，期望 %0.fm", tc.reelID, tc.lineID, got, tc.want)
			}
		})
	}
}

// TestSpoolFillCapsAtCapacity 锁定：整盘线长超过杯容量时按容量计，
// 短于容量时按实际线长计。
func TestSpoolFillCapsAtCapacity(t *testing.T) {
	if got := spoolFillRatio(600, 17); got != 1 {
		t.Fatalf("600m 盘装 17m 杯，上杯率应为 1，实得 %.3f", got)
	}
	if got := spoolFillRatio(8.5, 17); math.Abs(got-0.5) > 1e-9 {
		t.Fatalf("8.5m/17m 上杯率应为 0.5，实得 %.3f", got)
	}
	if got := spoolFillRatio(0, 17); got != 0 {
		t.Fatalf("无对应为 0，实得 %.3f", got)
	}
}

// newOverMaxCastRig 造一根"红区可超上限"的带轮装配：返回竿 GUID、满竿上限
// 与探针会话。选 4.5 m 鲤鱼竿 + 5000 轮，满竿上限约 25.3 m——红区 ×1.3 会越过
// 30 m 成就阶梯，正好用来验证收口是否作用到成就口径。
func newOverMaxCastRig(t *testing.T, srv *Server) (string, float64, *Session) {
	t.Helper()
	const uid = int64(1000001)
	rod, err := srv.CreateItem(t.Context(), uid, "carp_10139_13_4_50", "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	reel, err := srv.CreateItem(t.Context(), uid, "spin_5002", "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	line, err := srv.CreateItem(t.Context(), uid, "mono_40853", "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	sess := newProbeSess(srv)
	mountGearForTest(t, srv, sess, rod.GUID, reel.GUID)
	mountGearForTest(t, srv, sess, rod.GUID, line.GUID)
	if !srv.rigSetType(t.Context(), uid, rod.GUID, 16) {
		t.Fatal("设置底钓 Method 钓组失败")
	}
	rigMax := srv.castDistanceForRig(t.Context(), uid, rod.GUID, 16).DistanceM
	if rigMax <= 0 {
		t.Fatalf("装配满竿上限未生效: %v", rigMax)
	}
	return rod.GUID, rigMax, sess
}

// castOverMaxRequest 提交一次红区抛投（requested = 上限 ×1.3）并返回本次
// 收口后的实测距离。
func castOverMaxRequest(t *testing.T, srv *Server, sess *Session, rodGUID string, rigMax float64) float64 {
	t.Helper()
	fields := make([]proto.Field, 8)
	for i := range fields {
		fields[i] = proto.Field{Op: "U32", Num: ptrU64(0)}
	}
	// f0 = requested（上限 ×1.3，模拟红区）；f8 常规全挥杆（BYTE，系数 1.0）。
	fields[0] = proto.Field{Op: "U32", Num: ptrU64(uint64(proto.F32Bits(float32(rigMax * 1.3))))}
	fields = append(fields, proto.Field{Op: "BYTE", Num: ptrU64(1)})
	tree := proto.Args524(proto.Guid(rodGUID), proto.Obj("olieodeoaof", 0, fields...))
	c := &Ctx{Sess: sess, Main: 14, Sub: 2, Tree: tree,
		DecodeOK: true, ExpectResponse: true, Log: logging.Discard()}
	if body, handled := hCast(c); !handled || len(body) == 0 {
		t.Fatalf("14/2 未处理: handled=%v len=%d", handled, len(body))
	}
	st := srv.stateFor(sess.Userid)
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.CastM
}

// 红区收口：带轮装配 14/2 requested 超过满竿上限（158.f1×动作系数）时按
// 上限收口——客户端功率条满格红区可把请求推到上限的 ~1.25 倍（2026-11-13
// 实抓 51m/40.85m），官服拉满就是上限本身。
func TestCastOverMaxRequestedClampsToRigMax(t *testing.T) {
	srv, _ := newRigTestServer(t)
	rodGUID, rigMax, sess := newOverMaxCastRig(t, srv)
	castM := castOverMaxRequest(t, srv, sess, rodGUID, rigMax)
	if math.Abs(castM-rigMax) > 1e-6 {
		t.Fatalf("红区请求应按满竿上限收口: CastM=%g 上限=%g", castM, rigMax)
	}
}

// 轮管卡截停：卡米数是机械止线，requested（含红区）超过卡米时实测距离以
// 卡米为准。2026-11-19 实机：卡 8 抛 11.46，客户端未自行截停落 11.39m——
// 客户端贴卡依赖 17/13 推送竿树后的 153.f5（配套修复），服务端仍须兜底收口。
func TestCastOverClipCapsAtClipM(t *testing.T) {
	srv, _ := newRigTestServer(t)
	rodGUID, rigMax, sess := newOverMaxCastRig(t, srv)
	clipM := int64(rigMax / 2)
	clip := float64(clipM)
	if err := srv.DB.UpsertRodSetting(t.Context(), rodGUID, reelClipSettingKey, strconv.FormatInt(clipM, 10)); err != nil {
		t.Fatal(err)
	}
	castM := castOverMaxRequest(t, srv, sess, rodGUID, rigMax)
	if math.Abs(castM-clip) > 1e-6 {
		t.Fatalf("超卡抛投应按卡米截停: CastM=%g 卡米=%g", castM, clip)
	}
}

// 红区请求同样不能计入成就：成就阶梯用的是与资格判定同一个收口后距离。
// 不修时 DistanceM 取 requested（上限 ×1.3），比上限高一级的阶梯会被白刷。
func TestCastOverMaxRequestedDoesNotUnlockAchievements(t *testing.T) {
	srv, _ := newRigTestServer(t)
	rodGUID, rigMax, sess := newOverMaxCastRig(t, srv)
	castM := castOverMaxRequest(t, srv, sess, rodGUID, rigMax)
	if math.Abs(castM-rigMax) > 1e-6 {
		t.Fatalf("前置条件：本次距离应已收口到上限，CastM=%g 上限=%g", castM, rigMax)
	}
	// 1000-1004 是 30/45/60/80/100 m 阶梯（achievements_rules.go）。
	tiers := []struct {
		id int64
		m  float64
	}{{1000, 30}, {1001, 45}, {1002, 60}, {1003, 80}, {1004, 100}}
	states, err := srv.DB.PlayerAchievementStates(t.Context(), sess.Userid)
	if err != nil {
		t.Fatal(err)
	}
	above := 0
	for _, tr := range tiers {
		if rigMax > tr.m {
			continue // 上限本身就过了这一档，收不收口都该解锁
		}
		above++
		if states[tr.id].Unlocked {
			t.Fatalf("红区请求（%.2f）不应解锁 %.0f m 成就 %d，实际上限只有 %.2f",
				rigMax*1.3, tr.m, tr.id, rigMax)
		}
	}
	if above == 0 {
		t.Fatalf("用例前提不成立：上限 %.2f 已覆盖全部成就阶梯", rigMax)
	}
	// 正对照：把同一个 1.3 倍距离直接喂给成就规则，30 m 阶梯必须能解锁——
	// 证明上面"没解锁"是收口的结果，而不是成就系统没跑起来。
	srv.evaluateAchievements(&Ctx{Sess: sess, Main: 14, Sub: 2, Log: logging.Discard()},
		achievementEvent{Kind: evCast, DistanceM: rigMax * 1.3})
	states2, err := srv.DB.PlayerAchievementStates(t.Context(), sess.Userid)
	if err != nil {
		t.Fatal(err)
	}
	if !states2[1000].Unlocked {
		t.Fatalf("正对照失败：直接喂 %.2f m 应解锁 30 m 成就", rigMax*1.3)
	}
}

// 无轮手竿走旧协议基线（7.5m 是协议兼容位，不是真实上限），14/2 requested
// 直通、不做红区收口（手竿距离对齐见 PR #457）。
func TestCastRodlessKeepsRequestedOverLegacyMax(t *testing.T) {
	srv, _ := newRigTestServer(t)
	const uid = int64(1000001)
	if _, err := srv.DB.EnsureProfile(t.Context(), uid); err != nil {
		t.Fatal(err)
	}
	rod, err := srv.CreateItem(t.Context(), uid, "tele_10012_tl16", "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	sess := newProbeSess(srv)
	fields := make([]proto.Field, 8)
	for i := range fields {
		fields[i] = proto.Field{Op: "U32", Num: ptrU64(0)}
	}
	fields[0] = proto.Field{Op: "U32", Num: ptrU64(uint64(proto.F32Bits(50)))}
	tree := proto.Args524(proto.Guid(rod.GUID), proto.Obj("olieodeoaof", 0, fields...))
	c := &Ctx{Sess: sess, Main: 14, Sub: 2, Tree: tree,
		DecodeOK: true, ExpectResponse: true, Log: logging.Discard()}
	if body, handled := hCast(c); !handled || len(body) == 0 {
		t.Fatalf("14/2 未处理: handled=%v len=%d", handled, len(body))
	}
	st := srv.stateFor(uid)
	st.mu.Lock()
	castM := st.CastM
	st.mu.Unlock()
	if castM != 50 {
		t.Fatalf("无轮手竿红区请求应直通: CastM = %g，期望 50", castM)
	}
}

// 载荷未知（<=0）时走固定折减 0.80，是「缺 Test 区间竿按 0.85 折减」
// 之外的另一条边界：见 specs/changes/rod-missing-test-default/spec.md
// 「payload 为 0 的既有 0.80 分支不变」。该分支压得过低会让满功率抛投
// 掉到脚边，也会让超出 rig 上限的解锁用例失去正对照参照，因此单独锁定。
func TestRodTestWeightFactorWithoutPayloadKeepsDefault(t *testing.T) {
	for _, tc := range []struct{ minG, maxG float64 }{
		{2, 10}, {5, 30}, {0.5, 1.2}, {0, 8},
	} {
		if got := rodTestWeightFactor(0, tc.minG, tc.maxG); math.Abs(got-0.80) > 1e-9 {
			t.Fatalf("载荷未知时应固定折减 0.80（Test %.1f~%.1f），实得 %.4f",
				tc.minG, tc.maxG, got)
		}
	}
	// 未登记 Test 区间时优先走 0.85 保守折减，不应被载荷未知分支覆盖。
	if got := rodTestWeightFactor(0, 0, 0); math.Abs(got-0.85) > 1e-9 {
		t.Fatalf("未登记 Test 且载荷未知时应取 0.85，实得 %.4f", got)
	}
	// 已知载荷不应落在未知载荷的默认值上，避免把「恒返回 0.80」写成合法实现。
	if got := rodTestWeightFactor(8, 5, 30); math.Abs(got-0.80) < 1e-9 {
		t.Fatalf("已知载荷不应返回未知载荷的默认折减: %.4f", got)
	}
}

// TestCastCarpRigMatchesForumRange 锁定非路亚族的重装锚点：3.9m 鲤鱼竿
// （Test 1587-2040g）+ 140g 铅坠 + 纺车轮 + 7 点鲤鱼竿/5 点纺车轮/竿自带
// 15% = +75% 技能，玩家实测 135-146m。上游把该族压在固定 60 基准时同一
// 配置只有 49m，差近三倍。
func TestCastCarpRigMatchesForumRange(t *testing.T) {
	d, factors := calculateCastDistance(CastDistanceContext{
		RigType: 16, CastAction: 0, CastPower: 1,
		RodLengthM: 3.9, RodAction: 2, RodStiffness: 2,
		RodTestMinG: 1587, RodTestMaxG: 2040, PayloadWeightG: 140,
		ReelEfficiency: 1.0, ReelType: 1, SpoolSize: 20, SpoolFill: 1,
		LineDiameterMM: 0.35, LineStretch: 0.01, LineMaterial: 1,
		PlayerPrimary:   defaultPlayerConditionPrimary,
		PlayerSecondary: defaultPlayerConditionSecondary,
		SkillMastery:    100, SkillCastPct: 75,
	})
	// 同帖四条重装实测分布 95 / 105-110 / 115 / 135 / 146m，配置差异主要在竿与轮；
	// 指数取中位后本配置落在 112-117m（四条的中位区），而不是顶配那条的 146m。
	if d < 105 || d > 130 {
		t.Fatalf("3.9m/Test2040g/140g/+75%% 应落在四条实测的中位区 105-130m，实得 %.2f（nominal=%.1f，因子=%v）",
			d, castNominalDistanceForRod(16, 2040), factors)
	}
	// 同配置只挂 +60%（无竿自带 15%）应略近，且仍远于旧 60 基准的 45m。
	d60, _ := calculateCastDistance(CastDistanceContext{
		RigType: 16, CastAction: 0, CastPower: 1,
		RodLengthM: 3.9, RodAction: 2, RodStiffness: 2,
		RodTestMinG: 1587, RodTestMaxG: 2040, PayloadWeightG: 140,
		ReelEfficiency: 1.0, ReelType: 1, SpoolSize: 20, SpoolFill: 1,
		LineDiameterMM: 0.35, LineStretch: 0.01, LineMaterial: 1,
		PlayerPrimary:   defaultPlayerConditionPrimary,
		PlayerSecondary: defaultPlayerConditionSecondary,
		SkillMastery:    100, SkillCastPct: 60,
	})
	if d60 >= d {
		t.Fatalf("+60%% 应比 +75%% 近: %.2f vs %.2f", d60, d)
	}
}

// TestCastNominalNonLureFamilyScalesWithTest 锁定非路亚族同样随竿 Test 放大：
// 博洛尼亚（Test 18g）明显近于飞德（Test 100g），两者都远小于鲤鱼竿。
func TestCastNominalNonLureFamilyScalesWithTest(t *testing.T) {
	bologna := castNominalDistanceForRod(1, 18)
	feeder := castNominalDistanceForRod(12, 100)
	carp := castNominalDistanceForRod(16, 2040)
	if !(bologna < feeder && feeder < carp) {
		t.Fatalf("非路亚族标称应随 Test 单调递增: 博洛尼亚 %.1f, 飞德 %.1f, 鲤鱼 %.1f",
			bologna, feeder, carp)
	}
	if bologna < 35 || bologna > 41 {
		t.Fatalf("博洛尼亚 Test18 标称应落在 35-41，实得 %.2f", bologna)
	}
	if feeder < 55 || feeder > 62 {
		t.Fatalf("飞德 Test100 标称应落在 55-62，实得 %.2f", feeder)
	}
	// Test 缺失仍回落到族常量，不被分档曲线拉走。
	if got := castNominalDistanceForRod(1, 0); got != 60.0 {
		t.Fatalf("非路亚族 Test 缺失应回落 60，实得 %v", got)
	}
	if got := castNominalDistanceForRod(5, 0); got != 23.06 {
		t.Fatalf("路亚族 Test 缺失应回落 23.06，实得 %v", got)
	}
}
