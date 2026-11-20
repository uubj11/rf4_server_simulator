package gameserver

import (
	"encoding/json"
	"fmt"
	"math"
	"testing"
	"time"
)

// 官服 f5 锚点（A 级 14/14 样本，specs/changes/fish-official-data-tables/spec.md；
// 2026-11-16/17 断电抢救包新增 16 种（m_carp 无 f5 实测，见 fish-pull-k-table
// spec v2/v2.1.1）。
// tolPct = 该种种内散布带（官方随机因子，grade 检验证实与档位无关）。
// c.carp v2 由 6 放宽到 20：新样本实测每克 2.42~3.17（655g vs 631g 差 30%），
// 旧 6% 是两样本巧合收紧，新散布带以 4 样本极差为准。
var officialStaminaAnchors = []struct {
	species string
	weightG int64
	f5      float64
	tolPct  float64
}{
	{"merlang", 252, 2646.53, 3},
	{"piksha", 2975, 19010.5, 3},
	{"s.bream", 359, 2105.9785, 3},
	{"b_gill", 113, 459.556, 3},
	{"taran", 611, 2171.68, 3},
	{"din_l_carp", 1715, 4745.5, 3},
	{"n.pike", 4305, 11859.9, 3},
	{"carp", 1063, 2235.09, 3},
	{"b_carp", 7828, 16335.8, 3},
	{"perch", 642, 1586.1823, 19},
	{"perch", 1070, 2739.0903, 19},
	{"crucian", 611, 1604.2435, 25},
	{"c.carp", 655, 1587.5297, 20},
	{"c.carp", 631, 2002.2, 20},
	{"g_carp", 10307, 30384.6, 60},
	// —— v2 新实测种（2026-11-16/17 断电抢救包；散布带按样本极差）——
	// coalfish 每克 7.27~9.89（702g 是低散布端，均值口径偏离 +23.5%）→ tol 25。
	{"coalfish", 3027, 29950.7, 25},
	{"coalfish", 702, 5103.2, 25},
	{"atl_cod", 5677, 41758.1, 15},
	{"atl_cod", 2674, 16623.4, 15},
	{"menek", 6117, 45057.0, 5},
	{"makrel", 1891, 33727.9, 3},
	{"cha_herring", 114, 2198.9, 3},
	{"atl_herring", 560, 6554.6, 3},
	{"pollachius", 2387, 17940.6, 3},
	{"m_scorpius", 585, 2552.3, 15},
	{"m_scorpius", 205, 851.3, 15},
	{"shemaya_cha", 237, 1699.7, 25},
	{"shemaya_cha", 117, 995.0, 25},
	{"tiulka", 10, 33.3, 20},
	{"tiulka", 13, 36.4, 20},
	{"rudd", 402, 1761.5, 25},
	{"rudd", 196, 573.3, 25},
	{"pumps_sunfish", 52, 247.0, 3},
	{"n_carp", 4405, 8705.3, 3},
	{"carp_ghost", 4513, 10457.1, 3},
	{"kw_tench", 987, 2050.9, 3},
	{"c_bleak", 24, 68.1, 3},
}

// TestFishStaminaMatchesOfficialAnchors 防回归：实测种 f5 必须落在官方散布带内。
func TestFishStaminaMatchesOfficialAnchors(t *testing.T) {
	srv, _ := newRigTestServer(t)
	for _, a := range officialStaminaAnchors {
		f := &RolledFish{Species: a.species, WeightG: a.weightG, Grade: 2}
		profile := srv.fishFightProfileFor(f)
		got := srv.fishStaminaForProfile(f, profile)
		tol := a.f5 * a.tolPct / 100
		if math.Abs(got-a.f5) > tol {
			t.Errorf("%s %dg f5=%.1f，官服=%.1f（±%v%%）", a.species, a.weightG, got, a.f5, a.tolPct)
		}
	}
}

// TestFishF6MatchesOfficialTable 验证 f6 量化表、体重台阶与 fallback。
func TestFishF6MatchesOfficialTable(t *testing.T) {
	srv2, _ := newRigTestServer(t)
	srv2.tunMu.Lock()
	srv2.tunCache = map[string]float64{"fish_f6_scale": 1}
	srv2.tunText = map[string]string{"fish_f6_scale": "1"}
	srv2.tunAt = time.Now()
	srv2.tunMu.Unlock()
	cases := []struct {
		species string
		weightG int64
		want    float64
	}{
		{"perch", 342, 0.70},
		{"perch", 642, 0.80},
		{"crucian", 293, 0.55},
		{"crucian", 743, 0.60},
		{"c.roach", 680, 0.50},
		{"s.bream", 359, 0.30},
		{"n.pike", 4305, 0.80},
		{"g_carp", 21742, 0.70},
		{"not_in_calib", 500, 0.55},
	}
	for _, c := range cases {
		f := &RolledFish{Species: c.species, WeightG: c.weightG}
		if got := srv2.fishF6For(f); math.Abs(got-c.want) > 1e-9 {
			t.Errorf("%s %dg f6=%f，期望 %f", c.species, c.weightG, got, c.want)
		}
	}
}

// TestFightCurvesSpeciesPoolsLoaded 验证官方池按鱼种键直配（行数 = 官方抓包）。
func TestFightCurvesSpeciesPoolsLoaded(t *testing.T) {
	srv, _ := newRigTestServer(t)
	cases := []struct {
		species string
		rows    int
	}{
		{"taran", 13},
		{"b_gill", 13},
		{"n.pike", 12},
		{"merlang", 8},
		{"piksha", 9},
		{"din_l_carp", 24},
		{"g_carp", 13},
		{"carp", 16},
		{"c.roach", 18},
	}
	for _, c := range cases {
		curves := srv.buildFightCurves(c.species, 10)
		if curves == nil || len(curves.Items) != c.rows {
			t.Fatalf("%s 池行数=%d，期望直配官方 %d 行", c.species, len(curves.Items), c.rows)
		}
	}
	// 未入库种仍走 archetype 借池兜底。
	fallback := srv.buildFightCurves("not_in_calib", 10)
	if fallback == nil || len(fallback.Items) == 0 {
		t.Fatal("未入库种借池兜底失效")
	}
}

// TestFightCurvesMarineFallbackUsesOfficialMerlang 防回归：marine 组借官方
// merlang 真池（39a1085 官服 14/14 抓包直配入库）。此前借 perch 池是淡水
// 小杂鱼剧本（主搏 105 仅 4.5~8s、逃窜权重 1~2%、98% 时间碎潜水），实测
// 84.2kg meagre 全程 19s 被匀速拖回、14/8 遥测零负步（2026-11-16 搏鱼秒起
// 调查）。词表命中 marine 的未入库种（menek）必须与 merlang 拿到同一池：
// 同 pull 同池 uniform 缩放，全部 19 列应逐行一致。
func TestFightCurvesMarineFallbackUsesOfficialMerlang(t *testing.T) {
	if pool := fishFightArchetypes["marine"].CurvePool; pool != "merlang" {
		t.Fatalf("marine 组曲线池=%q，应为官方 merlang 池", pool)
	}
	srv, _ := newRigTestServer(t)
	const pull = 100.0
	menek := srv.buildFightCurves("menek", pull)
	merlang := srv.buildFightCurves("merlang", pull)
	if len(menek.Items) == 0 || len(menek.Items) != len(merlang.Items) {
		t.Fatalf("menek 借池行数=%d，merlang 池=%d", len(menek.Items), len(merlang.Items))
	}
	for i, row := range menek.Items {
		for col := 0; col < 19; col++ {
			if got, want := protoF32Field(row, col), protoF32Field(merlang.Items[i], col); got != want {
				t.Fatalf("menek 行 %d 列 %d=%f，merlang 池=%f（借池应为官方 merlang 真池）", i, col, got, want)
			}
		}
	}
}

// TestFightCurvesEveryPoolHasPhase100Base 防回归：原始资产里每个池必须恰含
// 1 条 f0=100 基准行，且 loader 把它钉到首位——buildFightCurves 用
// selected[0][8] 作 uniform 缩放锚，池缺基准行时 loader 静默保持导出器原序，
// 缩放会锚到恰好排首的错误行，±3% 容差发现不了这种错位。
func TestFightCurvesEveryPoolHasPhase100Base(t *testing.T) {
	var parsed struct {
		Pools map[string]struct {
			Rows [][19]any `json:"rows"`
		} `json:"pools"`
	}
	if err := json.Unmarshal(fightCurvesJSON, &parsed); err != nil {
		t.Fatalf("解析 fight_curves_sets.json: %v", err)
	}
	if len(parsed.Pools) == 0 {
		t.Fatal("fight_curves_sets.json 没有任何池")
	}
	srv, _ := newRigTestServer(t)
	for sp, pool := range parsed.Pools {
		base := 0
		for _, r := range pool.Rows {
			var f0 float64
			switch tv := r[0].(type) {
			case float64:
				f0 = tv
			case string:
				fmt.Sscanf(tv, "%g", &f0)
			}
			if int(f0) == 100 {
				base++
			}
		}
		if base != 1 {
			t.Errorf("%s 池 f0=100 基准行数=%d，期望恰 1 条", sp, base)
			continue
		}
		srv.curveMu.Lock()
		loaded := srv.curves[sp]
		srv.curveMu.Unlock()
		if len(loaded) == 0 {
			t.Errorf("%s 池加载后为空", sp)
			continue
		}
		if int(loaded[0][0]) != 100 {
			t.Errorf("%s 池加载后首位 f0=%v，期望 100（缩放锚必须是基准行）", sp, loaded[0][0])
		}
	}
}
