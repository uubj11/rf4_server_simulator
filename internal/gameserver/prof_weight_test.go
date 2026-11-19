package gameserver

import (
	"math"
	"testing"
)

// 熟练度加权：重量档 / 稀有度档 / 合并系数 / 单次增量。
// 期望值来自本次设计稿（见 .trae/documents/energy_prof_fatigue_plan.md）。

func TestProfWeightTierFactor(t *testing.T) {
	cases := []struct {
		weightG int64
		want    float64
	}{
		{42, 0.6},      // 胡瓜鱼、小鱼
		{199, 0.6},     // 边界下方
		{200, 1.0},     // 0.2kg 边界
		{339, 1.0},     // 河鲈
		{999, 1.0},     // 1kg 下方
		{1500, 1.5},    // 1.5kg
		{2999, 1.5},    // 3kg 下方
		{5000, 2.2},    // 5kg
		{22440, 3.0},   // 22.4kg 鲤鱼
		{100000, 3.0},  // 100kg 巨物
	}
	for _, c := range cases {
		if got := profWeightTierFactor(c.weightG); math.Abs(got-c.want) > 1e-9 {
			t.Errorf("profWeightTierFactor(%d)=%v, want %v", c.weightG, got, c.want)
		}
	}
}

func TestProfGradeTierFactor(t *testing.T) {
	cases := []struct {
		grade int64
		want  float64
	}{
		{0, 1.0}, {1, 1.0}, {2, 1.2}, {3, 1.4}, {4, 1.7}, {5, 1.7},
	}
	for _, c := range cases {
		if got := profGradeTierFactor(c.grade); math.Abs(got-c.want) > 1e-9 {
			t.Errorf("profGradeTierFactor(%d)=%v, want %v", c.grade, got, c.want)
		}
	}
}

func TestProfCatchFactor(t *testing.T) {
	cases := []struct {
		name               string
		w, g               int64
		weightScale, rarScale float64
		want               float64
	}{
		{"小鱼不放大", 42, 1, 1, 1, 0.6},
		{"22kg 3档", 22440, 3, 1, 1, 3.0 * 1.4},
		{"两轴都关掉=旧行为", 22440, 3, 0, 0, 1.0},
		{"只开重量轴", 22440, 3, 1, 0, 3.0},
		{"只开稀有轴", 42, 4, 0, 1, 1.7},
	}
	for _, c := range cases {
		got := profCatchFactor(c.w, c.g, c.weightScale, c.rarScale)
		if math.Abs(got-c.want) > 1e-9 {
			t.Errorf("%s: profCatchFactor=%v, want %v", c.name, got, c.want)
		}
	}
}

func TestProfWeightedGain(t *testing.T) {
	cases := []struct {
		name                  string
		base, factor, cap, rem float64
		want                  float64
	}{
		{"旧行为 0.1 不变", 0.1, 1.0, 1.0, 100, 0.1},
		{"旧行为 0.2 不变", 0.2, 1.0, 1.0, 100, 0.2},
		{"22kg3档 0.2×4.2", 0.2, 4.2, 1.0, 100, 0.8},
		{"小鱼 0.1×0.6", 0.1, 0.6, 1.0, 100, 0.1},
		{"封顶 0.5", 0.2, 4.2, 0.5, 100, 0.5},
		{"剩余空间不足", 0.2, 4.2, 1.0, 0.3, 0.3},
		{"剩余空间为0", 0.2, 4.2, 1.0, 0, 0},
	}
	for _, c := range cases {
		got := profWeightedGain(c.base, c.factor, c.cap, c.rem)
		if math.Abs(got-c.want) > 1e-9 {
			t.Errorf("%s: profWeightedGain=%v, want %v", c.name, got, c.want)
			_ = got
		}
	}
}
