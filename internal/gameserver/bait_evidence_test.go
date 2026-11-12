package gameserver

import (
	"context"
	"math"
	"testing"
	"time"
)

// A1：连续加权核。evidence 是真实渔获计数，avg 是该鱼种全部饵的平均证据。
func TestBaitContinuousMult(t *testing.T) {
	const eps = 1e-3
	cases := []struct {
		name string
		ev   int64
		avg  float64
		want float64
	}{
		{"欧鳊 1455/32.9 被 ceil 截顶", 1455, 32.9, 20.0},
		{"银鲷鱼 105/32.3 未触顶", 105, 32.3, math.Pow(105.0/32.3, 0.85)},
		{"噪声 1/74 被 floor 兜底", 1, 74.0, 0.4},
		{"恰好等平均 32/32", 32, 32.0, 1.0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := baitContinuousMult(c.ev, c.avg, 0.85, 0.4, 20)
			if math.Abs(got-c.want) > eps {
				t.Fatalf("got %g want %g", got, c.want)
			}
			if got <= 0 {
				t.Fatalf("倍率不得为 0：%g", got)
			}
		})
	}

	// 无信号必须返回 0，交由调用方回退到 tier，绝不直接乘进权重。
	for _, c := range [][2]float64{{0, 32.9}, {1455, 0}, {-3, 32.9}, {1455, -1}} {
		if got := baitContinuousMult(int64(c[0]), c[1], 0.85, 0.4, 20); got != 0 {
			t.Fatalf("ev=%g avg=%g 应返回 0（走回退），got %g", c[0], c[1], got)
		}
	}

	// 非法参数（NaN / 非正 floor、ceil）必须回落默认，不允许传播 NaN。
	if got := baitContinuousMult(105, 32.3, math.NaN(), 0, 0); got <= 0 || math.IsNaN(got) || math.IsInf(got, 0) {
		t.Fatalf("非法参数未回落默认：%g", got)
	}
	if got := baitContinuousMult(105, 32.3, 0.85, 0.4, 0.1); got < 0.4 {
		t.Fatalf("ceil 小于 floor 时应使用默认，got %g", got)
	}
}

// A2/A4：加权优先级（evidence → tier → 衰减）与开关一键回退。
func TestBaitWeightMultPriorityAndFallback(t *testing.T) {
	s, db := seedBaitRuleTest(t)
	ctx := context.Background()

	reload := func() {
		s.tunMu.Lock()
		s.tunAt = time.Time{}
		s.tunMu.Unlock()
	}
	for _, kv := range [][2]string{
		{"bait_pref_scale", "1"},
		{"bait_evidence_enabled", "1"},
		{"bait_evidence_beta", "0.85"},
		{"bait_evidence_floor", "0.4"},
		{"bait_evidence_ceil", "20"},
	} {
		if err := db.TunableSet(ctx, kv[0], kv[1], "test"); err != nil {
			t.Fatal(err)
		}
	}
	reload()

	tiers := map[string]int64{"bream": 3, "s.bream": 2, "frog": 1}
	ev := map[string]int64{"bream": 1455, "s.bream": 105}
	avg := map[string]float64{"bream": 32.9, "s.bream": 32.3}
	meta := map[string]int64{"frog": 7475}

	mb, hitB := s.baitWeightMult("bream", tiers, ev, avg, meta)
	ms, hitS := s.baitWeightMult("s.bream", tiers, ev, avg, meta)
	if !hitB || !hitS {
		t.Fatalf("evidence 分支未命中：hitB=%v hitS=%v", hitB, hitS)
	}
	if mb != 20.0 {
		t.Fatalf("欧鳊应被 ceil 截到 20，got %g", mb)
	}
	if math.Abs(ms-math.Pow(105.0/32.3, 0.85)) > 1e-3 {
		t.Fatalf("银鲷鱼倍率异常：%g", ms)
	}
	if mb/ms < 6.0 {
		t.Fatalf("梯度不足 6:1：bream=%g s.bream=%g ratio=%g", mb, ms, mb/ms)
	}

	// 无 evidence 的鱼种回退到 tier 乘子（tier1 → ×2）。
	if mf, _ := s.baitWeightMult("frog", tiers, ev, avg, meta); math.Abs(mf-2.0) > 1e-9 {
		t.Fatalf("frog 应走 tier1 ×2，got %g", mf)
	}

	// tier 也没有的鱼种走覆盖度衰减，但仍为正数（绝不归零）。
	if mu, hitU := s.baitWeightMult("unknown_fish", map[string]int64{}, map[string]int64{}, map[string]float64{}, meta); hitU || mu <= 0 || mu > 1 {
		t.Fatalf("未知鱼种应为正衰减值：m=%g hit=%v", mu, hitU)
	}

	// evMap 为 nil（表缺失/查询失败）时同样回退到 tier，不允许 panic。
	if mt, _ := s.baitWeightMult("bream", tiers, nil, nil, meta); math.Abs(mt-8.0) > 1e-9 {
		t.Fatalf("evMap 为 nil 应回退 tier3 ×8，got %g", mt)
	}

	// 关闭总开关 → 完全回到 4 档 tier 逻辑。
	if err := db.TunableSet(ctx, "bait_evidence_enabled", "0", "test"); err != nil {
		t.Fatal(err)
	}
	reload()
	if mb2, _ := s.baitWeightMult("bream", tiers, ev, avg, meta); math.Abs(mb2-8.0) > 1e-9 {
		t.Fatalf("关闭开关应回到 tier3 ×8，got %g", mb2)
	}
}

// A6：meta 缺失导致 avg 全为 0 时，整条链路仍必须能发出鱼。
func TestRollFishSurvivesWithoutEvidenceMeta(t *testing.T) {
	s, db := seedBaitRuleTest(t)
	ctx := context.Background()
	for _, kv := range [][2]string{
		{"bait_pref_scale", "1"},
		{"bait_evidence_enabled", "1"},
	} {
		if err := db.TunableSet(ctx, kv[0], kv[1], "test"); err != nil {
			t.Fatal(err)
		}
	}
	s.tunMu.Lock()
	s.tunAt = time.Time{}
	s.tunMu.Unlock()

	if _, err := db.SQL.Exec(`INSERT INTO companion_bait_fish(bait_id,fish_key,tier,evidence) VALUES('rule_worm','c.roach',3,500)`); err != nil {
		t.Fatal(err)
	}
	// 故意不写 companion_fish_meta：avg 为空 → 全部回退 tier → 必须仍能发鱼。
	for i := 0; i < 20; i++ {
		if f := s.rollFish("level_900_rules", "rule_worm"); f == nil {
			t.Fatalf("meta 缺失时第 %d 次没能发鱼", i+1)
		}
	}
}
