package gameserver

import (
	"math"
	"testing"
)

// 成长套装钩宽容带：只放宽概率模型，不动 gape、钩号与承力。
// 25 号 / 18mm = gape 62.99kg（手竿、浮漂现役钩）；
// 21 号 / 10mm = gape 10.80kg（底钓现役钩）；33 号 / 76mm = gape 2700kg（海钓现役钩，已封顶）。

func tolHookSpec(size int64, lengthMM float64) gearPhysicsSpec {
	return gearPhysicsSpec{HookPresent: true, HookSize: size, HookLengthMM: lengthMM}
}

// H1：普通竿（tol=0）必须与原版逐位一致，这是回归护栏。
func TestHookToleranceOrdinaryRodUnchanged(t *testing.T) {
	cases := []struct {
		name  string
		spec  gearPhysicsSpec
		weightG int64
	}{
		{"手竿25号钓1kg", tolHookSpec(25, 18), 1000},
		{"手竿25号钓3kg", tolHookSpec(25, 18), 3000},
		{"手竿25号钓40kg", tolHookSpec(25, 18), 40000},
		{"底钓21号钓40kg", tolHookSpec(21, 10), 40000},
		{"海钓33号钓3125kg", tolHookSpec(33, 76), 3125000},
		{"小钩6号钓28kg", tolHookSpec(6, 3.3), 28000},
	}
	for _, tc := range cases {
		wantSolid, wantHold := hookSolidHold(tc.weightG, tc.spec)
		gotSolid, gotHold := hookSolidHoldTol(tc.weightG, tc.spec, 0)
		if gotSolid != wantSolid || gotHold != wantHold {
			t.Fatalf("%s: tol=0 必须等于原版，得 solid=%.6f hold=%.6f，原版 solid=%.6f hold=%.6f",
				tc.name, gotSolid, gotHold, wantSolid, wantHold)
		}
		if got := hookSpawnCompatibilityTol(tc.weightG, tc.spec, 0); got != hookSpawnCompatibility(tc.weightG, tc.spec) {
			t.Fatalf("%s: tol=0 发鱼权重必须等于原版，得 %.6f", tc.name, got)
		}
	}
}

// H1b：tol=0 时三分段公式必须与「.55+1.8r」「.35+2.6r」「.96−.45(r−.8)」数值等价。
func TestHookToleranceZeroMatchesLegacyFormula(t *testing.T) {
	spec := tolHookSpec(21, 10) // gape 10.8kg
	gape := hookGapeG(spec)
	if gape <= 0 {
		t.Fatal("gape 必须大于 0")
	}
	for _, weightG := range []int64{500, 1000, 2000, 5000, 12000, 40000, 120000} {
		r := float64(weightG) / gape
		wantSolid := .96 - .45*math.Max(0, r-.8)
		if r < .25 {
			wantSolid *= .55 + 1.8*r
		}
		wantSolid = groundbaitClamp(wantSolid, .15, .96)
		wantHold := 1.0
		if r > 1 {
			wantHold = groundbaitClamp(.98-.30*(r-1), .50, .98)
		}
		gotSolid, gotHold := hookSolidHoldTol(weightG, spec, 0)
		if math.Abs(gotSolid-wantSolid) > 1e-9 || math.Abs(gotHold-wantHold) > 1e-9 {
			t.Fatalf("weight=%d tol=0 与原版公式不符: solid %.9f vs %.9f, hold %.9f vs %.9f",
				weightG, gotSolid, wantSolid, gotHold, wantHold)
		}
	}
}

// H2：满级套装的大钩钓小鱼不再被压到 0.55，而是抬到 0.79 起。
func TestHookToleranceLiftsLargeHookSmallFish(t *testing.T) {
	spec := tolHookSpec(25, 18) // 63kg gape
	baseSolid, _ := hookSolidHoldTol(1000, spec, 0)
	if math.Abs(baseSolid-.5554) > 2e-3 {
		t.Fatalf("原版 25 号钓 1kg 的正口率应约 0.555，实得 %.4f", baseSolid)
	}
	solid, hold := hookSolidHoldTol(1000, spec, 1)
	if solid < .80 {
		t.Fatalf("满级宽容后 25 号钓 1kg 正口率应 ≥0.80，实得 %.4f", solid)
	}
	if hold != 1 {
		t.Fatalf("鱼重未超 gape 时保持率必须为 1，实得 %.4f", hold)
	}
	if solid <= baseSolid {
		t.Fatalf("宽容带没有生效: tol=1 %.4f 不大于 tol=0 %.4f", solid, baseSolid)
	}
}

// H3：满级套装修正超重鱼的脱钩断崖（底钓 21 号钓 40kg）。
func TestHookToleranceSoftensOverCapacity(t *testing.T) {
	spec := tolHookSpec(21, 10) // 10.8kg gape
	baseSolid, baseHold := hookSolidHoldTol(40000, spec, 0)
	if math.Abs(baseSolid-.15) > 1e-9 || math.Abs(baseHold-.50) > 1e-9 {
		t.Fatalf("原版 21 号钓 40kg 应为地板 0.15/0.50，实得 %.4f/%.4f", baseSolid, baseHold)
	}
	solid, hold := hookSolidHoldTol(40000, spec, 1)
	if solid < .40 {
		t.Fatalf("满级宽容后 40kg 正口率应 ≥0.40，实得 %.4f", solid)
	}
	if hold < .60 {
		t.Fatalf("满级宽容后 40kg 保持率应 ≥0.60，实得 %.4f", hold)
	}
}

// H4：海钓满级用 33 号也能稳上 3125kg，而原版会掉 7% 保持率。
func TestHookToleranceHoldsTrophyAtCap(t *testing.T) {
	spec := tolHookSpec(33, 76) // 2700kg gape
	_, baseHold := hookSolidHoldTol(3125000, spec, 0)
	if baseHold >= 1 {
		t.Fatalf("原版 3125kg 应低于 1.0 保持率，实得 %.4f", baseHold)
	}
	solid, hold := hookSolidHoldTol(3125000, spec, 1)
	if math.Abs(solid-.96) > 1e-9 {
		t.Fatalf("满级宽容后 3125kg 正口率应为 0.96，实得 %.4f", solid)
	}
	if hold != 1 {
		t.Fatalf("满级宽容后 3125kg 保持率应为 1，实得 %.4f", hold)
	}
}

// H5：发鱼权重同步放宽，但保留「大钩小鱼权重略低」的策略性。
// 2026-11-17 追加发鱼下限后：1kg（r≈.0159）属于超小个体，满级宽容也不得再抬高（发鱼下限见 hook_spawn_floor_test.go），
// 下限之上的 5kg 段才享受抬升。
func TestHookToleranceSpawnCompatibility(t *testing.T) {
	spec := tolHookSpec(25, 18)
	base := hookSpawnCompatibilityTol(1000, spec, 0)
	if math.Abs(base-.3913) > 2e-3 {
		t.Fatalf("原版 25 号钓 1kg 的发鱼权重应约 0.391，实得 %.4f", base)
	}
	got := hookSpawnCompatibilityTol(1000, spec, 1)
	if got > .20 {
		t.Fatalf("满级宽容下 1kg 超小个体应被压到 ≤0.20，实得 %.4f", got)
	}
	if got <= .02 {
		t.Fatalf("不得低于发鱼权重 .02 地板，实得 %.4f", got)
	}
	if lifted := hookSpawnCompatibilityTol(5000, spec, 1); lifted < .90 {
		t.Fatalf("下限之上的 5kg 段应放宽到 ≥0.90，实得 %.4f", lifted)
	}
	// 高段（小钩钓超重鱼）也要跟着放宽，但仍有地板：2.33kg 钩钓 100kg，r≈42.9。
	small := tolHookSpec(14, 6) // 2.33kg gape
	if base := hookSpawnCompatibilityTol(100000, small, 0); math.Abs(base-.10) > 1e-9 {
		t.Fatalf("原版小钩钓超重鱼应压到地板 0.10，实得 %.4f", base)
	}
	if got := hookSpawnCompatibilityTol(100000, small, 1); math.Abs(got-.22) > 1e-9 {
		t.Fatalf("满级宽容后小钩钓超重鱼地板应抬到 0.22，实得 %.4f", got)
	}
}

// H6：宽容进度越界必须钳制。
func TestHookToleranceClampsProgress(t *testing.T) {
	spec := tolHookSpec(25, 18)
	loSolid, loHold := hookSolidHoldTol(1000, spec, -5)
	zeroSolid, zeroHold := hookSolidHoldTol(1000, spec, 0)
	if loSolid != zeroSolid || loHold != zeroHold {
		t.Fatalf("负进度应等同 0：%.6f/%.6f vs %.6f/%.6f", loSolid, loHold, zeroSolid, zeroHold)
	}
	hiSolid, hiHold := hookSolidHoldTol(1000, spec, 5)
	oneSolid, oneHold := hookSolidHoldTol(1000, spec, 1)
	if hiSolid != oneSolid || hiHold != oneHold {
		t.Fatalf("越界进度应等同 1：%.6f/%.6f vs %.6f/%.6f", hiSolid, hiHold, oneSolid, oneHold)
	}
}

// H7：等级进度换算，以及 0 级套装的起步宽容度。
func TestGrowthToleranceProgress(t *testing.T) {
	cases := []struct{ level, cap int64; want float64 }{
		{0, 12, 0}, {-3, 12, 0}, {6, 12, .5}, {12, 12, 1}, {20, 12, 1}, {3, 16, .1875}, {5, 0, 0},
	}
	for _, tc := range cases {
		if got := growthToleranceProgress(tc.level, tc.cap); math.Abs(got-tc.want) > 1e-9 {
			t.Fatalf("level=%d cap=%d 得 %.4f 期望 %.4f", tc.level, tc.cap, got, tc.want)
		}
	}
	levels := []struct{ progress, want float64 }{
		{0, hookToleranceBase}, {-1, hookToleranceBase}, {.5, .65}, {1, 1}, {3, 1},
	}
	for _, tc := range levels {
		if got := growthToleranceLevel(tc.progress); math.Abs(got-tc.want) > 1e-9 {
			t.Fatalf("进度 %.2f 的宽容度得 %.4f 期望 %.4f", tc.progress, got, tc.want)
		}
	}
	// 0 级套装必须已经明显比普通钓具宽容，否则玩家买了套装却看不到变化。
	spec := tolHookSpec(25, 18)
	fresh, _ := hookSolidHoldTol(1000, spec, growthToleranceLevel(0))
	legacy, _ := hookSolidHoldTol(1000, spec, 0)
	if fresh < .62 {
		t.Fatalf("0 级套装的 25 号钓 1kg 正口率应 ≥0.62，实得 %.4f", fresh)
	}
	if fresh <= legacy {
			t.Fatalf("0 级套装应比原版宽容: %.4f vs %.4f", fresh, legacy)
	}
	if growthToleranceLevel(0) >= 1 || growthToleranceLevel(1) != 1 {
		t.Fatal("起步宽容度必须小于满级 1.0")
	}
}
