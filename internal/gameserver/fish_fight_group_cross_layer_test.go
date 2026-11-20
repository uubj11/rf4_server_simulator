package gameserver

import (
	"testing"
	"time"
)

// 跨层一致性门禁（PR #516 复审 P2-1/P2-2）：f8 组表声明的习性必须驱动
// f5/f6/f21 全部搏斗层，皮肤变种必须全链路继承宿主。此前只断言 f8 K0，
// 组表归组与词表判定漂移（如 54 个 f8 salmonid 中 26 个 f5/f6 落 default）
// 无法被测试捕获。

// TestFishPullGroupFightKeyCompleteness 组表直查映射完备性：组表出现过的
// 每个组、组值表的每个键都必须有 f5/f6/f21 组键映射，防未来加组漏配。
func TestFishPullGroupFightKeyCompleteness(t *testing.T) {
	used := map[string]bool{}
	for species, g := range fishPullSpeciesGroup {
		if _, ok := fishPullGroupFightKey[g]; !ok {
			t.Errorf("鱼种 %s 的 f8 组 %s 无 fishPullGroupFightKey 映射（f5/f6/f21 将落词表漂移）", species, g)
		}
		used[g] = true
	}
	for g := range fishPullGroupK0 {
		if _, ok := fishPullGroupFightKey[g]; !ok {
			t.Errorf("组值表 %s 无 fishPullGroupFightKey 映射", g)
		}
	}
	// 映射值必须是真实 archetype 键（"marine" 例外：交回海图判定）。
	for g, key := range fishPullGroupFightKey {
		if key == "marine" {
			continue
		}
		if _, ok := fishFightArchetypes[key]; !ok {
			t.Errorf("组 %s 映射到未知 archetype 键 %q", g, key)
		}
	}
	_ = used
}

// TestFishFightCrossLayerConsistency 代表鱼全链路断言：f8 组值、f5 每克、
// f6、f21 曲线池来自同一习性口径。marine 用注入的静态库海图键模拟。
func TestFishFightCrossLayerConsistency(t *testing.T) {
	srv := &Server{}
	srv.tunMu.Lock()
	srv.tunCache = map[string]float64{}
	srv.tunAt = time.Now()
	srv.tunMu.Unlock()
	// pinagor 仅出现在挪威海（loc_fish），注入海图判定模拟生产环境。
	srv.marineFishMu.Lock()
	srv.marineFishKeys = map[string]bool{"pinagor": true}
	srv.marineFishMu.Unlock()

	const w = int64(500)
	cases := []struct {
		species   string
		groupKey  string  // fishFightGroupKeyFor 期望
		wantK0    float64 // f8 基值
		wantF5Per float64 // f5 每克
		wantF6    float64
		wantPool  string // f21 曲线池
	}{
		// 复审 P2-1 主案例：虾按体型归弱鱼口径，f8/f5/f6/f21 同源。
		{"crawfish", "small", 13.00, 2.50, 0.50, "c.roach"},
		// f8 salmonid 但词表落 default 的俄语译名鲑科，修后全链路 salmonid。
		{"sig", "salmonid", 18.00, 3.00, 0.60, "e.chub"},
		{"chavycha", "salmonid", 18.00, 3.00, 0.60, "e.chub"},
		// small_forage 组的泥鳅/鳞走小鱼口径（a_smelt 原被词表 smelt 误归 salmonid）。
		{"barb_toni", "small", 18.64, 2.50, 0.50, "c.roach"},
		{"a_smelt", "small", 18.64, 2.50, 0.50, "c.roach"},
		// sunfish 组淡水太阳鱼走小鱼口径（warm 原被词表 marine 误吃 8.445/g）。
		{"warm", "small", 19.62, 2.50, 0.50, "c.roach"},
		// predator 组鳄雀鳝（词表 marine 误伤）修后伏击口径。
		{"agar", "ambush", 17.30, 2.75, 0.65, "perch"},
		// marine_rock 淡水成员（鳗鱼）回落词表 eel 口径，不吃海水耐力。
		{"eel", "eel", 17.66, 2.80, 0.50, "a.sleeper"},
		// 海图成员全链路 marine：f8=marine_rock 组值，f5/f6/f21=marine 口径。
		{"pinagor", "marine", 17.66, 8.445, 0.50, "merlang"},
	}
	for _, c := range cases {
		marine := srv.fishMarineGroupHit(c.species)
		if got := fishFightGroupKeyFor(c.species, marine); got != c.groupKey {
			t.Errorf("%s f5/f6 组键=%s，期望 %s", c.species, got, c.groupKey)
		}
		if got := srv.fishPullK0For(c.species); got != c.wantK0 {
			t.Errorf("%s f8 K0=%f，期望 %f", c.species, got, c.wantK0)
		}
		if got := srv.fishStaminaFor(&RolledFish{Species: c.species, WeightG: w}); got != c.wantF5Per*float64(w) {
			t.Errorf("%s 500g f5=%f，期望 %f（%f/g）", c.species, got, c.wantF5Per*float64(w), c.wantF5Per)
		}
		if got := srv.fishF6BaseFor(&RolledFish{Species: c.species, WeightG: w}); got != c.wantF6 {
			t.Errorf("%s f6=%f，期望 %f", c.species, got, c.wantF6)
		}
		if got := fishCurveArchetypeFor(c.species, marine).CurvePool; got != c.wantPool {
			t.Errorf("%s f21 曲线池=%s，期望 %s", c.species, got, c.wantPool)
		}
	}
}

// TestFishPullAliasInheritsFullFightChain 复审 P2-2：皮肤变种全链路继承
// 宿主（非 1kg 体重，1kg 时 ln 项消失无法证伪）。midori_g 是 carp 皮肤：
// f8 斜率外推、f5 显式表、f6 量化表、f21 曲线池都必须与 carp 同源。
func TestFishPullAliasInheritsFullFightChain(t *testing.T) {
	srv := &Server{}
	srv.tunMu.Lock()
	srv.tunCache = map[string]float64{}
	srv.tunAt = time.Now()
	srv.tunMu.Unlock()

	if got, want := srv.fishFightPullFor(&RolledFish{Species: "midori_g", WeightG: 2000}),
		srv.fishFightPullFor(&RolledFish{Species: "carp", WeightG: 2000}); got != want {
		t.Errorf("midori_g 2kg f8=%f 应逐位等于宿主 carp %f", got, want)
	}
	if got, want := srv.fishStaminaFor(&RolledFish{Species: "midori_g", WeightG: 1000}),
		srv.fishStaminaFor(&RolledFish{Species: "carp", WeightG: 1000}); got != want {
		t.Errorf("midori_g 1000g f5=%f 应等于宿主 carp 实测均值 %f（2.10/g）", got, want)
	}
	if got, want := srv.fishF6BaseFor(&RolledFish{Species: "midori_g", WeightG: 100}),
		srv.fishF6BaseFor(&RolledFish{Species: "carp", WeightG: 100}); got != want || got != 0.65 {
		t.Errorf("midori_g f6=%f 应继承宿主 carp 0.65", got)
	}
	if got := fishCurveArchetypeFor("midori_g", false).CurvePool; got != "c.carp" {
		t.Errorf("midori_g f21 曲线池=%s 应随宿主 carp 落 cyprinid 池 c.carp", got)
	}
}

// TestFishPullSmallFishUnderPowerScale 复审 P2-3：fight_power_scale 分支
// 下限与主路径同为 0.05N 浮点保护。tiulka 10g 官方 f8=0.1864N，scale=0.5
// 应得 0.0932N；旧 0.5N 下限会把它抬回 0.5。
func TestFishPullSmallFishUnderPowerScale(t *testing.T) {
	srv := &Server{}
	srv.tunMu.Lock()
	srv.tunCache = map[string]float64{"fight_power_scale": 0.5}
	srv.tunAt = time.Now()
	srv.tunMu.Unlock()
	if got := srv.fishFightPullFor(&RolledFish{Species: "tiulka", WeightG: 10}); got != 0.0932 {
		t.Errorf("tiulka 10g scale=0.5 的 f8=%f，期望 0.0932（0.5N 旧下限会把小鱼抬回）", got)
	}
}
