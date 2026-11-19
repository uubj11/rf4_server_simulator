package gameserver

import (
	"math"
	"testing"
)

// 鱼搏斗自耗：fish_fatigue_scale 通过压低 f6（抗疲劳系数）加速鱼疲劳累积。
// 客户端公式：疲劳 += (1-f6)×5×倍率×挣扎功率×dt，所以 (1-f6) 越大鱼越累得快。

func TestApplyFishFatigueScale(t *testing.T) {
	const base = 0.5628187656402588 // 官方抓包初始体力比 / 抗疲劳系数
	cases := []struct {
		name  string
		f6    float64
		scale float64
		want  float64
	}{
		{"scale=1 保持官方口径", base, 1.0, base},
		{"scale=1.25 疲劳快四分之一", base, 1.25, 1 - 0.4371812343597412*1.25},
		{"scale=0.2 疲劳变慢", base, 0.2, 1 - 0.4371812343597412*0.2},
		{"scale=4 直接拉满疲劳速率", base, 4.0, 0.0},
		{"测试鱼 f6=1 永不疲劳", 1.0, 4.0, 1.0},
		{"f6=0 也不会变成负", 0.0, 2.0, 0.0},
		{"上限钳到 0.95", 0.99, 0.001, 0.95},
	}
	for _, c := range cases {
		got := applyFishFatigueScale(c.f6, c.scale)
		if math.Abs(got-c.want) > 1e-9 {
			t.Errorf("%s: applyFishFatigueScale(%v,%v)=%v, want %v", c.name, c.f6, c.scale, got, c.want)
		}
	}
}
