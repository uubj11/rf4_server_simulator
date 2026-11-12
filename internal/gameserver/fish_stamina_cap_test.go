package gameserver

import "testing"

// 大鱼体力池封顶：
// 体力池随体重线性膨胀，而渔轮泄力有硬上限，导致 >38kg 的鱼必然把张力
// 压在泄力上限上，时效 ∝ 体力池 ∝ 体重。封顶后大鱼的等效体重不超过 capG，
// 小鱼完全不受影响，capG<=0 回到原版行为。

func TestBigFishStaminaCap(t *testing.T) {
	const capG = defaultBigFishStaminaCapG

	// A1：远超封顶的鱼，等效体重被压到 capG。
	if got := bigFishStaminaCapG(246739, capG); got != capG {
		t.Fatalf("246.7kg 应封顶到 %.0f，得到 %.0f", capG, got)
	}
	if got := bigFishStaminaCapG(3125000, capG); got != capG {
		t.Fatalf("3125kg 应封顶到 %.0f，得到 %.0f", capG, got)
	}

	// A2：封顶线以内的鱼完全不变（原版手感保留）。
	for _, w := range []float64{1, 230, 1417, 11000, 32000, 38000} {
		if got := bigFishStaminaCapG(w, capG); got != w {
			t.Fatalf("%.0fg 不该被封顶，得到 %.0f", w, got)
		}
	}

	// A3：capG<=0 关闭封顶，回到原版线性体力池。
	if got := bigFishStaminaCapG(246739, 0); got != 246739 {
		t.Fatalf("cap=0 应关闭封顶，得到 %.0f", got)
	}
	if got := bigFishStaminaCapG(246739, -1); got != 246739 {
		t.Fatalf("cap<0 应关闭封顶，得到 %.0f", got)
	}

	// 体力池比值：封顶后 246.7kg 与 38kg 的体力池完全相等。
	rawHeavy := verifiedStaminaPerGram * bigFishStaminaCapG(246739, capG)
	rawCap := verifiedStaminaPerGram * bigFishStaminaCapG(capG, capG)
	if rawHeavy != rawCap {
		t.Fatalf("封顶后体力池应相等：%.1f vs %.1f", rawHeavy, rawCap)
	}
	// 未封顶时 246.7kg 的体力池应是 38kg 的 6.5 倍左右。
	uncapped := verifiedStaminaPerGram * 246739
	if ratio := uncapped / (verifiedStaminaPerGram * capG); ratio < 6.4 || ratio > 6.6 {
		t.Fatalf("未封顶比值应约 6.49，得到 %.2f", ratio)
	}
}
