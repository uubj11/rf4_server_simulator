package gameserver

import (
	"math"
	"testing"
)

// TestHalfHourCapGrams 验证「半小时能拉起多重」的反解：
// 同一公式用 w_catfish 的每克体力系数（2.388×catfish 1.65≈3.9403）。
// 标定依据：用户实拉 224 kg 白鲶 73 分钟（泄力 13.63 kgf），本公式算出 78 分钟以内。
func TestHalfHourCapGrams(t *testing.T) {
	const rateWC = 3.9403 // verifiedStaminaPerGram × catfish StaminaScale 1.65
	const seconds = 1800.0

	// 13.63 kgf 的普通轮：半小时上限应在 14~18 kg 量级
	if got := halfHourCapGrams(13.63, rateWC, seconds); got < 14000 || got > 18000 {
		t.Fatalf("13.63kg 泄力半小时上限=%d g，期望 14000~18000 g", got)
	}

	// 手杆成长套满级 25 kgf：半小时上限应覆盖该钓法 250 kg 上限
	if got := halfHourCapGrams(25.0, rateWC, seconds); got < 230000 || got > 270000 {
		t.Fatalf("25kg 泄力半小时上限=%d g，期望 230000~270000 g", got)
	}

	// 泄力为 0（无轮且无承力）时不做截断
	if got := halfHourCapGrams(0, rateWC, seconds); got != 0 {
		t.Fatalf("泄力=0 应返回 0（不截断），实际 %d", got)
	}

	// 上限随泄力单调增
	if a, b := halfHourCapGrams(20, rateWC, seconds), halfHourCapGrams(40, rateWC, seconds); b <= a {
		t.Fatalf("泄力翻倍后上限未增：20kg→%d, 40kg→%d", a, b)
	}
}

// TestHalfHourCapFightSeconds 反向自检：上限重量的预计搏斗时长应接近目标秒数。
func TestHalfHourCapFightSeconds(t *testing.T) {
	const rateWC = 3.9403
	drag := 13.63
	w := float64(halfHourCapGrams(drag, rateWC, 1800))

	stamina := rateWC * w * math.Pow(w/defaultFishStaminaKneeG, defaultFishStaminaExponent-1)
	seconds := stamina * verifiedInitialStaminaRatio / drag
	if math.Abs(seconds-1800) > 60 { // ±1 分钟
		t.Fatalf("反解重量 %.0f g 的预计时长=%.0f s，期望 1800±60 s", w, seconds)
	}
}
