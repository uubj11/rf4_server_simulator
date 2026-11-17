package gameserver

import (
	"math"
	"testing"
)

// A+ 方案口径：成长件单次渔获回复 = clamp(鱼重kg × 0.002, 保底 0.01, 封顶 0.10)，
// 再减去本次磨损并钳到 [growthDurabilityFloor, 1.0]。
//
// 即保留原 A 方案的"每公斤 0.2%"，只给每尾加 1% 保底：小鱼（<5 kg）一律回 1%，
// 界面上的损耗百分比每尾至少掉 1 个点；大鱼仍按重量线性拉开差距，50 kg 才吃满 10% 封顶。
// 非成长件与下限钳制不变。
func TestGrowthDurabilityHealRateWithFloor(t *testing.T) {
	cases := []struct {
		name   string
		before float64
		wear   float64
		kg     float64
		want   float64
	}{
		{"保底：0.165 kg 小鱼回 1%", 0.10, 0.0002, 0.165, 0.10 - 0.0002 + 0.01},
		{"保底：0.01 kg 极小鱼同样回 1%", 0.10, 0.0002, 0.01, 0.10 - 0.0002 + 0.01},
		{"保底：1 kg 仍只回 1%（0.002 不足保底）", 0.10, 0.0002, 1, 0.10 - 0.0002 + 0.01},
		{"线性：10 kg 回 2%", 0.10, 0.0002, 10, 0.10 - 0.0002 + 0.02},
		{"线性：25 kg 回 5%", 0.10, 0.0002, 25, 0.10 - 0.0002 + 0.05},
		{"封顶：50 kg 吃满 10%", 0.50, 0.0002, 50, 0.50 - 0.0002 + 0.10},
		{"封顶：300 kg 不再超过 10%", 0.50, 0.0002, 300, 0.50 - 0.0002 + 0.10},
	}
	for _, tc := range cases {
		got := growthDurabilityAfterCatch(tc.before, tc.wear, tc.kg, true)
		if math.Abs(got-tc.want) > 1e-9 {
			t.Fatalf("%s：期望 %.6f，实得 %.6f", tc.name, tc.want, got)
		}
	}

	// 断竿地板 0.10 起，一尾 165 g 小鱼必须让界面（整数百分比）看见变化。
	if got := growthDurabilityAfterCatch(0.10, 0.0002, 0.165, true); got < 0.105 {
		t.Fatalf("小鱼回复应让损耗百分比至少掉 1 点，实得 %.6f", got)
	}

	// 非成长件不受保底影响，照旧线性磨损。
	if got := growthDurabilityAfterCatch(0.5, 0.02, 300, false); math.Abs(got-0.48) > 1e-9 {
		t.Fatalf("非成长件应按磨损线性下降，实得 %.4f", got)
	}

	// 地板钳制仍在：回复补不上大磨损时停在地板。
	if got := growthDurabilityAfterCatch(0.10, 0.05, 0.165, true); got != growthDurabilityFloor {
		t.Fatalf("不应跌破地板 %.2f，实得 %.4f", growthDurabilityFloor, got)
	}
}
