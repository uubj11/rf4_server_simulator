package gameserver

import (
	"math"
	"testing"
	"time"
)

// 官服 14/14 抓包 A 级锚点（2026-09-18 真值表 9 样本 + 2026-11-10/11 新抓 12+8
// 样本 + 2026-11-16/17 断电抢救包 53 样本，见 fish-pull-k-table spec v2 章节）。
// 只落数值锚点，不含任何抓包原始数据（CONTRIBUTING 禁令不受影响）。
// 恒 k 类鱼种锚点配对跨体重档（如 coalfish 702/3027g），钉死 Slope=0。
var officialPullAnchors = []struct {
	species string
	weightG int64
	pullN   float64 // 官方 f8（牛）
	tolPct  float64 // 允许偏差
}{
	{"crucian", 611, 9.0919, 2},
	{"crucian", 293, 4.28627, 2},
	{"crucian", 743, 11.1182, 2},
	{"crucian", 193, 2.7937, 2},
	{"crucian", 527, 7.814, 2},
	{"crucian", 320, 4.695, 2},
	{"crucian", 341, 5.014, 2},
	{"c.roach", 680, 11.0735, 2},
	{"c.roach", 150, 2.3899, 2},
	{"c.roach", 713, 11.6109, 2},
	{"perch", 221, 3.74761, 2},
	{"perch", 93, 1.55752, 3},
	{"perch", 1070, 18.9743, 3},
	{"perch", 119, 1.99803, 3},
	{"perch", 342, 5.86736, 3},
	{"perch", 642, 11.1688, 3},
	{"perch", 100, 1.676, 3},
	{"perch", 340, 5.832, 3},
	{"perch", 259, 4.408, 3},
	{"g_carp", 21742, 423.798, 2},
	{"g_carp", 4880, 93.4397, 2},
	{"g_carp", 5484, 105.073, 2},
	{"g_carp", 5716, 109.545, 2},
	{"g_carp", 10307, 198.496, 2},
	{"b_carp", 7828, 146.519, 2},
	{"carp", 1063, 17.5527, 2},
	{"carp", 4589, 79.202, 2},
	{"carp", 2434, 41.320, 2},
	{"carp", 5710, 98.919, 2},
	{"c.carp", 655, 8.9112, 2},
	{"c.carp", 140, 1.8186, 3},
	{"c.carp", 631, 8.576, 3},
	{"c.carp", 1574, 22.198, 3},
	{"s.bream", 359, 4.5193, 2},
	{"taran", 611, 11.3884, 2},
	{"piksha", 2975, 55.451, 2},
	{"merlang", 252, 5.19145, 2},
	{"b_gill", 113, 2.21706, 2},
	{"n.pike", 4305, 76.6636, 2},
	{"din_l_carp", 1715, 29.3737, 2},
	// —— v2 新实测种（2026-11-16/17 断电抢救包）——
	// 恒 k 类：跨体重档锚点钉死 Slope=0。
	{"coalfish", 702, 15.151, 2},
	{"coalfish", 3027, 65.329, 2},
	{"coalfish", 1456, 31.423, 2},
	{"coalfish", 1692, 36.517, 2},
	{"atl_cod", 2674, 57.710, 2},
	{"atl_cod", 5677, 122.521, 2},
	{"m_scorpius", 205, 3.620, 2},
	{"m_scorpius", 585, 10.330, 2},
	{"m_scorpius", 1142, 20.165, 2},
	{"shemaya_cha", 117, 2.181, 2},
	{"shemaya_cha", 237, 4.417, 2},
	{"shemaya_cha", 220, 4.101, 2},
	{"tiulka", 10, 0.186, 3},
	{"tiulka", 13, 0.242, 3},
	{"pumps_sunfish", 52, 1.020, 2},
	{"menek", 6117, 114.015, 2},
	{"pollachius", 2387, 53.858, 2},
	{"atl_herring", 560, 12.635, 2},
	{"makrel", 1891, 44.522, 2},
	{"cha_herring", 114, 2.908, 2},
	// 变 k 类新种：配对锚点钉 slope。
	{"rudd", 196, 3.602, 3},
	{"rudd", 402, 7.541, 3},
	{"rudd", 397, 7.446, 3},
	{"rudd", 195, 3.583, 3},
	{"n_carp", 4405, 75.099, 2},
	{"carp_ghost", 4513, 77.871, 2},
	{"kw_tench", 987, 18.432, 2},
	{"c_bleak", 24, 0.424, 3},
}

// TestFishPullMatchesOfficialAnchors 防回归：已实测鱼种的 f8 必须落在官服
// A 级样本 ±tol% 内。这是 P1 修复的核心验收。
func TestFishPullMatchesOfficialAnchors(t *testing.T) {
	srv := &Server{}
	for _, a := range officialPullAnchors {
		f := &RolledFish{Species: a.species, WeightG: a.weightG}
		got := srv.fishFightPullFor(f)
		tol := a.pullN * a.tolPct / 100
		if math.Abs(got-a.pullN) > tol {
			t.Errorf("%s %dg f8=%.4fN，官服=%.4fN（±%v%%）", a.species, a.weightG, got, a.pullN, a.tolPct)
		}
	}
}

// TestFishPullGroupFallback 验证未实测鱼种走习性组表与 fallback，量级为官方域；
// 已升实测的种（v2：coalfish 等 17 种）优先走逐鱼种表，不再落组。
func TestFishPullGroupFallback(t *testing.T) {
	srv := &Server{}
	cases := []struct {
		species string
		group   string
		wantK   float64
	}{
		{"riv_rb_trout", "salmonid", 18.0},
		{"lm_b_bass", "predator", 17.30},     // v2：组值锚 n.pike 实测
		{"din_m_carp", "carp_mid", 16.50},    // v2：组值锚 carp
		{"karasekarp", "still_carp", 15.05},  // v2：组值锚 crucian
		{"shemaya", "small_forage", 18.64},   // v2：移出 roach_run，同黑海欧白鱼实测族
		{"cetorhinus_m", "apex", 25.53},      // v2 新组：鲨/剑鱼/金枪鱼（纯推断）
		{"carp_albino", "alias→carp", 16.50}, // v2 别名：皮肤变种继承宿主 carp
		{"crawfish", "weak_silver", 13.00},   // v2.1 修正（PR #516）：虾无拉力数据按体型归贝/蛙组，不再落 salmonid
		{"pinagor", "marine_rock", 17.66},    // v2.1 修正（PR #516）：圆鳍鱼仅挪威海，海底层归 marine_rock，与 f5/f6 marine 判定自洽
		{"not_in_calib", "fallback", 17.5},
	}
	for _, c := range cases {
		if got := srv.fishPullK0For(c.species); math.Abs(got-c.wantK) > 1e-9 {
			t.Errorf("%s K0=%f，期望 %s 组 %f", c.species, got, c.group, c.wantK)
		}
		// 官方 k 实测域 [12.59, 25.53]（v2：s.bream~cha_herring；v1 的 [12.6, 20.6]
		// 被恒 k 海鱼实证推翻）。1kg 时 ln 项为 0，k 即 K0。
		pull := srv.fishFightPullFor(&RolledFish{Species: c.species, WeightG: 1000})
		k := pull // w=1kg 时 k 即 pull
		if k < 12.0 || k > 26.0 {
			t.Errorf("%s 1kg 等效 k=%f 越出官方域", c.species, k)
		}
	}
	// v2.1（PR #516）：别名必须整参数继承宿主（含斜率）。1kg 时 ln(1)=0 斜率
	// 项消失，无法证明继承；用 2kg（ln2≈0.693）逐位对比 host/alias 的 f8，
	// 并确认它 ≠ 纯 K0 外推（斜率项确实参与）。
	carp2kg := srv.fishFightPullFor(&RolledFish{Species: "carp", WeightG: 2000})
	alias2kg := srv.fishFightPullFor(&RolledFish{Species: "carp_albino", WeightG: 2000})
	if carp2kg != alias2kg {
		t.Errorf("carp_albino 2kg f8=%f 应逐位等于宿主 carp %f（斜率继承）", alias2kg, carp2kg)
	}
	if want := math.Round(2*(16.50+0.49*math.Log(2))*10000) / 10000; carp2kg != want {
		t.Errorf("carp 2kg f8=%f 应为 K0+0.49·ln2 外推 %f（斜率参与计算）", carp2kg, want)
	}
	// v2 升实测的种必须优先命中逐鱼种表（组表已移除其映射）。
	if got := srv.fishPullK0For("coalfish"); math.Abs(got-21.58) > 1e-9 {
		t.Errorf("coalfish 应走实测表 K0=21.58，实得 %f（v2 前=组 19.7）", got)
	}
	if got := srv.fishPullK0For("n_carp"); math.Abs(got-16.53) > 1e-9 {
		t.Errorf("n_carp 应走实测表 K0=16.53，实得 %f（v2 前=组 16.7）", got)
	}
	// m_carp v2.1 口径（PR #516 审查修正）：4 样本仅产出拟合参数，无单样本
	// f8 锚点可断言（样本原始值按 CONTRIBUTING 禁令不入库、断电后未留档）。
	// 此处钉表参数防误改；拿到补抓样本后再升级为数值锚点。
	if got := srv.fishPullParamFor("m_carp"); got != (fishPullParam{K0: 16.27, Slope: 0.59}) {
		t.Errorf("m_carp 参数应为 {16.27, 0.59}，实得 %+v", got)
	}
}

// TestFishPullLnSlopeTunable 验证斜率与 fallback 的 tunable 应急覆盖。
// 统一用 2kg（PR #516 审查修正）：1kg 时 ln(1)=0，斜率项恒消失，
// tunable 是否生效根本不可观测。tunable 语义：只替换 Slope<0（未标定）
// 的回退斜率；自拟合斜率与恒 k 不受影响。
func TestFishPullLnSlopeTunable(t *testing.T) {
	srv := &Server{}
	ln2 := math.Log(2)
	srv.tunCache = map[string]float64{"fish_pull_ln_slope": 0}
	srv.tunAt = time.Now()
	if got := srv.fishPullK0For("not_in_calib"); got != 17.5 {
		t.Fatalf("fallback K0 应为 17.5，实得 %f", got)
	}
	// tunable 置 0 抹平未标定种斜率：2kg fallback = 2×K0。
	// 若 tunable 未接线，会是 2×(17.5+0.35·ln2)≈36.192。
	if got := srv.fishFightPullFor(&RolledFish{Species: "not_in_calib", WeightG: 2000}); math.Abs(got-2*17.5) > 1e-6 {
		t.Fatalf("斜率置 0 后 2kg fallback 应恰为 2×17.5=35.0，实得 %f", got)
	}
	// 自拟合斜率不吃 tunable：置 0 后 2kg perch 仍带 0.38·ln2 项
	// （期望值先按实现口径 round 到 1e-4）。
	want := math.Round(2*(17.6+0.38*ln2)*10000) / 10000
	if got := srv.fishFightPullFor(&RolledFish{Species: "perch", WeightG: 2000}); got != want {
		t.Fatalf("斜率置 0 不应影响自拟合的 perch，2kg 应为 %f，实得 %f", want, got)
	}
	// 恒 k 类（Slope=0）不吃 tunable。
	if got := srv.fishFightPullFor(&RolledFish{Species: "coalfish", WeightG: 2000}); math.Abs(got-2*21.58) > 1e-6 {
		t.Fatalf("恒 k 的 coalfish 2kg 应恒为 2×21.58=43.16（Slope=0 不吃 tunable），实得 %f", got)
	}
	// tunable=1：未标定种斜率取 1，2kg fallback = 2×(K0+1·ln2)。
	srv.tunCache = map[string]float64{"fish_pull_ln_slope": 1.0}
	srv.tunAt = time.Now()
	want1 := math.Round(2*(17.5+ln2)*10000) / 10000
	if got := srv.fishFightPullFor(&RolledFish{Species: "not_in_calib", WeightG: 2000}); got != want1 {
		t.Fatalf("斜率 tunable=1 时 fallback 2kg 应为 2×(17.5+ln2)=%f，实得 %f", want1, got)
	}
}
