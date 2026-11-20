package gameserver

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestFishDielCurveWeightsParsing(t *testing.T) {
	if w := dielCurveWeights(""); w != nil {
		t.Fatalf("空曲线应返回 nil，得到 %v", w)
	}
	if w := dielCurveWeights("[1,0.5,0.2]"); w != nil {
		t.Fatalf("非四段曲线应返回 nil，得到 %v", w)
	}
	if w := dielCurveWeights("not json"); w != nil {
		t.Fatalf("非法 JSON 应返回 nil，得到 %v", w)
	}
	w := dielCurveWeights("[-1,0.4,0.35,1.2]")
	want := []float64{0, 0.4, 0.35, 1}
	if !reflect.DeepEqual(w, want) {
		t.Fatalf("四段曲线解析或钳制错误: 得到 %v，期望 %v", w, want)
	}
}

func TestFishDielWeightUsesCurveAsWindow(t *testing.T) {
	// 夜行鱼：夜里满权重，白天明显衰减但不为零（窗口期语义）。
	p := fishEcologyProfile{DielCurve: "[1,0.25,0.12,0.6]", DielMask: 0b1111}
	cases := []struct {
		hour int64
		want float64
	}{
		{23, 1},   // 夜
		{3, 1},    // 夜
		{6, 0.25}, // 晨
		{12, 0.12},
		{18, 0.6},
	}
	for _, c := range cases {
		if got := fishDielWeight(p, c.hour); got != c.want {
			t.Fatalf("hour=%d 期望 %.2f 实得 %.2f", c.hour, c.want, got)
		}
	}
}

func TestFishDielWeightSegmentBoundaries(t *testing.T) {
	p := fishEcologyProfile{DielCurve: "[0.1,0.2,0.3,0.4]"}
	cases := []struct {
		hour int64
		want float64
	}{
		{-1, 0.1}, // 归一化到 23 点，夜
		{0, 0.1},
		{4, 0.1},
		{5, 0.2}, // 晨
		{8, 0.2},
		{9, 0.3}, // 昼
		{15, 0.3},
		{16, 0.4}, // 昏
		{19, 0.4},
		{20, 0.1}, // 夜
		{23, 0.1},
		{24, 0.1}, // 跨日归一化
	}
	for _, c := range cases {
		if got := fishDielWeight(p, c.hour); got != c.want {
			t.Fatalf("hour=%d 期望 %.2f 实得 %.2f", c.hour, c.want, got)
		}
	}
}

func TestFishDielWeightFallsBackToMask(t *testing.T) {
	// 无曲线时仍按位图：命中 1.0，未命中 0.35。
	day := fishEcologyProfile{DielMask: 1 << 2} // 只允许白天
	if got := fishDielWeight(day, 12); got != 1 {
		t.Fatalf("白天命中位图应得 1，实得 %.2f", got)
	}
	if got := fishDielWeight(day, 23); got != .35 {
		t.Fatalf("夜间未命中位图应得 0.35，实得 %.2f", got)
	}
}

func TestFishDielCurveAssetMatchesDerivedMaskAndSources(t *testing.T) {
	type assetProfile struct {
		FishKey    string `json:"fish_key"`
		DielMask   int64  `json:"diel_mask"`
		DielCurve  string `json:"diel_curve"`
		DielSource string `json:"diel_source"`
	}
	var raw []assetProfile
	if err := json.Unmarshal(fishEcologyDefsJSON, &raw); err != nil {
		t.Fatalf("加载生态基线失败: %v", err)
	}
	if len(raw) != 253 {
		t.Fatalf("生态资产=%d，期望 253", len(raw))
	}

	sources := map[string]int{}
	for _, p := range raw {
		if p.DielSource == "" {
			t.Fatalf("%s 缺少 diel_source", p.FishKey)
		}
		sources[p.DielSource]++
		w := dielCurveWeights(p.DielCurve)
		if len(w) != 4 {
			t.Fatalf("%s 曲线无法解析: %q", p.FishKey, p.DielCurve)
		}
		var rawWeights []float64
		if err := json.Unmarshal([]byte(p.DielCurve), &rawWeights); err != nil {
			t.Fatalf("%s 原始曲线无法解析: %v", p.FishKey, err)
		}
		expectedMask := int64(0)
		peak := 0.0
		for i, v := range rawWeights {
			if v < fishDielCurveMin || v > 1 {
				t.Fatalf("%s 曲线越界: %v", p.FishKey, rawWeights)
			}
			if v > peak {
				peak = v
			}
			if v >= 0.5 {
				expectedMask |= 1 << uint(i)
			}
		}
		if peak < 0.999 {
			t.Fatalf("%s 曲线峰值 %.2f 未归一到 1: %v", p.FishKey, peak, rawWeights)
		}
		if p.DielMask != expectedMask {
			t.Fatalf("%s diel_mask=%d，按曲线应为 %d: %v", p.FishKey, p.DielMask, expectedMask, rawWeights)
		}
	}

	wantSources := map[string]int{
		"rf4info":              120,
		"gamekee-avg:high/low": 1,
		"gamekee-avg:low/high": 4,
		"gamekee-avg:low/low":  13,
		"gamekee-avg:mid/high": 9,
		"gamekee-avg:mid/low":  15,
		"gamekee-avg:mid/mid":  69,
		"global-avg":           22,
	}
	if !reflect.DeepEqual(sources, wantSources) {
		t.Fatalf("diel_source 覆盖不符: 得到 %v，期望 %v", sources, wantSources)
	}
}
