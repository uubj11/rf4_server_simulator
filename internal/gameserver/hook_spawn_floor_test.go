package gameserver

import (
	"math"
	"testing"
)

// 发鱼下限：宽容带只放宽「够得着的中段」，低于下限的个体要按 重量/下限 把抬升收回，
// 避免大钩发一堆不达标的小鱼；绝对下限封顶 30kg，防止海钓大钩把中小型海鱼全判死。

func spawnFloorWeight(spec gearPhysicsSpec, ratio float64) int64 {
	return int64(math.Round(hookGapeG(spec) * ratio))
}

// S1：tol=0 时下限系数不得改变任何数值，普通钓具零变化。
func TestHookSpawnFloorOrdinaryRodUnchanged(t *testing.T) {
	specs := []struct {
		name string
		spec gearPhysicsSpec
	}{
		{"手竿25号18mm", tolHookSpec(25, 18)},
		{"底钓21号10mm", tolHookSpec(21, 10)},
		{"小钩14号6mm", tolHookSpec(14, 6)},
		{"海钓33号76mm", tolHookSpec(33, 76)},
	}
	ratios := []float64{0.0159, 0.05, 0.12, 0.3}
	for _, sc := range specs {
		for _, ratio := range ratios {
			w := spawnFloorWeight(sc.spec, ratio)
			if w < 1 {
				w = 1
			}
			got := hookSpawnCompatibilityTol(w, sc.spec, 0)
			if want := hookSpawnCompatibility(w, sc.spec); got != want {
				t.Fatalf("%s r≈%.4f: tol=0 必须与原版逐位一致，得 %.6f 期望 %.6f", sc.name, ratio, got, want)
			}
		}
	}
}

// S2：满级套装下，低于下限的超小鱼不得再被抬高。
func TestHookSpawnFloorSuppressesUndersized(t *testing.T) {
	spec := tolHookSpec(25, 18) // gape 62.99kg，下限 5.04kg
	w := spawnFloorWeight(spec, 0.0159)
	legacy := hookSpawnCompatibilityTol(w, spec, 0)
	if legacy < .38 || legacy > .40 {
		t.Fatalf("前提：原版 1kg 的发鱼权重应约 .391，实得 %.4f", legacy)
	}
	got := hookSpawnCompatibilityTol(w, spec, 1)
	if got > .20 {
		t.Fatalf("满级宽容下 r=.0159 发鱼权重应 ≤ .20（改前 .6804），实得 %.4f", got)
	}
	if got < .02 {
		t.Fatalf("不得低于 applyHookSpawnMatch 的 .02 地板，实得 %.4f", got)
	}
}

// S3：下限之上仍享受宽容带放宽；中段满权重。
func TestHookSpawnFloorKeepsBandAboveFloor(t *testing.T) {
	spec := tolHookSpec(25, 18)
	atFloor := int64(math.Round(hookSpawnFloorG(hookGapeG(spec))))
	if got := hookSpawnCompatibilityTol(atFloor, spec, 1); got < .90 {
		t.Fatalf("下限处发鱼权重应 ≥ .90（原版约 .558），实得 %.4f", got)
	}
	mid := spawnFloorWeight(spec, 0.16)
	if got := hookSpawnCompatibilityTol(mid, spec, 1); got != 1 {
		t.Fatalf("中段 r=0.16 应满权重 1，实得 %.4f", got)
	}
}

// S4：低段随鱼重单调递增，不能出现「越小越容易发」。
func TestHookSpawnFloorMonotonic(t *testing.T) {
	spec := tolHookSpec(25, 18)
	prev := -1.0
	for _, ratio := range []float64{0.0159, 0.03, 0.045, 0.06, 0.07, 0.08} {
		w := spawnFloorWeight(spec, ratio)
		got := hookSpawnCompatibilityTol(w, spec, 1)
		if got <= prev {
			t.Fatalf("r≈%.4f 的发鱼权重 %.4f 未高于前一档 %.4f", ratio, got, prev)
		}
		prev = got
	}
}

// S5：下限只作用于发鱼，咬钩正口率保持原宽容带口径。
func TestHookSpawnFloorDoesNotTouchBite(t *testing.T) {
	spec := tolHookSpec(25, 18)
	w := spawnFloorWeight(spec, 0.0159)
	if solid, _ := hookSolidHoldTol(w, spec, 1); solid < .78 {
		t.Fatalf("发鱼下限不得反向压咬钩正口率，实得 solid=%.4f", solid)
	}
}

// S6：超大钩口的绝对下限封顶 30kg，海钓中小型鱼不被判死。
func TestHookSpawnFloorCapsAbsoluteWeight(t *testing.T) {
	sea := tolHookSpec(33, 76) // gape 2700kg
	if got := hookSpawnFloorG(hookGapeG(sea)); math.Abs(got-float64(hookSpawnFloorMaxG)) > 1e-6 {
		t.Fatalf("海钓大钩下限应封顶 %dg，实得 %.1f", hookSpawnFloorMaxG, got)
	}
	// 20kg 的海鱼（相对 2700kg 钩口 r≈.0074）仍应被明显压低，但 40kg 回到宽容带。
	w20 := int64(20000)
	low := hookSpawnCompatibilityTol(w20, sea, 1)
	if low > .50 {
		t.Fatalf("20kg 海鱼在满级大钩下发鱼权重应被压低（≤.50），实得 %.4f", low)
	}
	// 未加下限时会得到 .648（低段基座 .62 + 斜率），封顶后必须明显低于它。
	if low >= .648 {
		t.Fatalf("发鱼下限未生效，20kg 实得 %.4f", low)
	}
	w60 := int64(60000)
	if got := hookSpawnCompatibilityTol(w60, sea, 1); got < .65 || got <= low {
		t.Fatalf("60kg 海鱼已在下限之上，权重应明显回升（≥.65 且高于 20kg 档），实得 %.4f", got)
	}
}
