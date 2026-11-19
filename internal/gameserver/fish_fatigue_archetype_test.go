package gameserver

import (
	"math"
	"testing"
)

// 鱼搏斗自耗按「行为型」分档：猛冲型（鲨/鲑）自己耗得快，伏击型（狗鱼/鲈）省体力，
// 小鱼/鳗最省。行为型来自 fishFightArchetype（服务端既有分类），不是逐事件行为。
func TestFishFatigueArchetypeScale(t *testing.T) {
	cases := []struct {
		species string
		want    float64
	}{
		{"shark_katran", 1.45},   // apex
		{"atlantic_salmon", 1.45}, // salmonid
		{"c.carp", 1.25},         // cyprinid
		{"beluga_sturgeon", 1.25}, // sturgeon
		{"atlantic_cod", 1.25},   // marine
		{"wels_catfish", 1.15},   // catfish
		{"northern_pike", 1.00},  // ambush
		{"c.roach", 0.90},        // small
		{"conger_eel", 0.90},     // eel
		{"", 1.00},               // default
	}
	for _, c := range cases {
		if got := fishFatigueArchetypeScale(c.species); math.Abs(got-c.want) > 1e-9 {
			t.Errorf("fishFatigueArchetypeScale(%q)=%v, want %v", c.species, got, c.want)
		}
	}
}

// 总倍率 × 行为型开关：开关为 0 时与官服口径完全一致（默认值），设 1 才启用分档。
func TestFishFatigueEffectiveScale(t *testing.T) {
	cases := []struct {
		name         string
		species      string
		global       float64
		profileScale float64
		want         float64
	}{
		{"分档关闭=官服口径", "c.carp", 1.0, 0.0, 1.00},
		{"鲤科 分档全开", "c.carp", 1.0, 1.0, 1.25},
		{"鲨鱼 分档全开", "shark_katran", 1.0, 1.0, 1.45},
		{"狗鱼 分档全开=不动", "northern_pike", 1.0, 1.0, 1.00},
		{"小鱼 分档全开更省", "c.roach", 1.0, 1.0, 0.90},
		{"鲨鱼 分档半开", "shark_katran", 1.0, 0.5, 1.225},
		{"总倍率整体压到0.8", "northern_pike", 0.8, 1.0, 0.80},
	}
	for _, c := range cases {
		got := fishFatigueEffectiveScale(c.species, c.global, c.profileScale)
		if math.Abs(got-c.want) > 1e-9 {
			t.Errorf("%s: fishFatigueEffectiveScale=%v, want %v", c.name, got, c.want)
		}
	}
}
