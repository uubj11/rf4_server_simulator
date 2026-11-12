package gameserver

import (
	"math"
	"testing"
)

// 大鱼体力池幂律曲线：拐点以下完全等于原版线性（小鱼手感不动），
// 拐点以上按 (体重/拐点)^(指数-1) 折减，指数 1.0 可完整回退。

func TestBigFishStaminaCurve(t *testing.T) {
	knee := defaultFishStaminaKneeG
	exp := defaultFishStaminaExponent

	// ① 拐点及以下一律返回 1（与原版逐字节一致）。
	for _, w := range []float64{1, 230, 1000, 5000, 9999, knee} {
		if got := bigFishStaminaScale(w, knee, exp); got != 1 {
			t.Fatalf("%.0fg 在拐点以下不该被折减，得到 %.6f", w, got)
		}
	}

	// ② 拐点以上单调递减：体重越大，折减越狠。
	prev := 1.0
	for _, w := range []float64{11000, 30000, 51948, 246739, 3125000} {
		got := bigFishStaminaScale(w, knee, exp)
		if got <= 0 || got > prev {
			t.Fatalf("%.0fg 折减倍率应单调变小：prev=%.6f got=%.6f", w, prev, got)
		}
		prev = got
	}

	// ③ 数值锚点（指数 0.22、拐点 10 kg）。
	checks := []struct {
		w    float64
		want float64
	}{
		{11000, 0.9284},
		{51948, 0.2766},
		{246739, 0.0820},
		{3125000, 0.0113},
	}
	for _, c := range checks {
		if got := bigFishStaminaScale(c.w, knee, exp); math.Abs(got-c.want) > 0.001 {
			t.Fatalf("%.0fg 折减倍率应为 %.4f，得到 %.4f", c.w, c.want, got)
		}
	}

	// ④ 3,125 kg 抽样上限鱼：体力池压到原版的 1.5% 以内，
	//    这是「最好钓具半小时内拉完」的定量依据。
	origPool := verifiedStaminaPerGram * 3125000
	newPool := origPool * bigFishStaminaScale(3125000, knee, exp)
	if ratio := newPool / origPool; ratio > 0.015 {
		t.Fatalf("3125kg 体力池应压到 1.5%% 以内，实际 %.4f", ratio)
	}

	// ⑤ 回退：指数 1.0 或拐点 <=0 都回到原版线性。
	for _, tc := range []struct{ knee, exp float64 }{{knee, 1.0}, {0, exp}, {-1, exp}, {knee, 0}} {
		if got := bigFishStaminaScale(3125000, tc.knee, tc.exp); got != 1 {
			t.Fatalf("回退参数 knee=%.0f exp=%.2f 应返回 1，得到 %.6f", tc.knee, tc.exp, got)
		}
	}

	// ⑥ 拐点以上连续，不能出现断崖：20kg 与 20.2kg 的倍率差 < 1%。
	a := bigFishStaminaScale(20000, knee, exp)
	b := bigFishStaminaScale(20200, knee, exp)
	if a-b > 0.01 {
		t.Fatalf("拐点以上应连续，20kg=%.4f 20.2kg=%.4f", a, b)
	}
}
