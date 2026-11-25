package gameserver

import (
	"testing"
	"time"
)

// 空轮三件套：空因标注、保底杂口（环境放宽）、空轮节奏由 tunable 控制。
// 食性硬门不放宽是本设计的红线：pityEnv 下 G1 依然剔除非掠食鱼。

// 保底杂口：分布散布为 0 的鱼在 pityEnv 下保底 0.15，正常路径仍为 0。
func TestPityDistributionFloor(t *testing.T) {
	s, _ := seedBaitRuleTest(t)
	at := time.Unix(1794643000, 0)
	area := "rules-test/1/1"
	// 找一个该格散布为 0 的鱼种×时刻组合（散布确定性，约一成组合为 0）。
	// 散布周期是 24 真实小时，所以按周期步进扫描，可复现且不再跳过。
	var zeroKey string
	// 只有稀有鱼会整天缺席，这里把 eel 标成 rarity 1 才可能搜到散布为 0 的格子。
	setFishRaritySnapshot(map[string]int64{"eel": 1})
	for step := 0; step < 64 && zeroKey == ""; step++ {
		ts := at.Add(time.Duration(step) * distributionPeriodRealHours * time.Hour)
		for _, key := range []string{"eel", "frog"} {
			// 份额 1/101：低于「永不缺席」阈值，仍可能被刷掉。
			if distributionMultiplier(area, key, ts, 1.0/101) == 0 {
				at, zeroKey = ts, key
				break
			}
		}
	}
	if zeroKey == "" {
		t.Fatal("64 小时×2 鱼种内无散布为 0 的组合：散布模型已变，需修本测试")
	}
	// 同杆里另一款饵承担绝大部分权重，目标鱼份额 1/101，才会命中缺席判定线。
	base := []string{zeroKey, "c.roach"}
	wNormal := []float64{10, 1000}
	applyFishDistribution("rules-test", base, wNormal, fishRollContext{x: 50, z: 50, hasPos: true, now: at})
	if wNormal[0] != 0 {
		t.Fatalf("正常路径散布为 0 时权重应为 0, got %v", wNormal[0])
	}
	wPity := []float64{10, 1000}
	applyFishDistribution("rules-test", []string{zeroKey, "c.roach"}, wPity, fishRollContext{x: 50, z: 50, hasPos: true, now: at, pityEnv: true})
	if wPity[0] != 10*0.15 {
		t.Fatalf("pityEnv 散布 0 应保底 ×0.15, got %v", wPity[0])
	}
	_ = s
}

// 保底杂口：G2 水层地板在 pityEnv 下抬到 ≥0.05；G1 拟饵硬门不受 pityEnv 影响。
func TestPityLayerFloorButNotLureGate(t *testing.T) {
	s, db := seedBaitRuleTest(t)
	if _, err := db.SQL.Exec(`INSERT OR REPLACE INTO fish_ecology(fish_key,layer_min,layer_pref,layer_max,temp_min_c,temp_pref_c,temp_max_c,diel_mask,diet_json)
		VALUES('frog',0.9,0.95,1.0,0,20,40,15,'{"lure":0.2,"worm":1.0}')`); err != nil {
		t.Fatal(err)
	}
	// 实证票：frog×蚯蚓 在列 → G-diet 双票全权重，断言只剩水层地板一个因子
	if _, err := db.SQL.Exec(`INSERT INTO companion_bait_fish(bait_id,fish_key,tier,evidence) VALUES('rule_worm','frog',1,1)`); err != nil {
		t.Fatal(err)
	}
	if err := s.reloadFishEcologyCache(s.dbCtx()); err != nil {
		t.Fatal(err)
	}
	setBiteGateSwitch(t, s, "1")
	cfg := s.biteGateConfig()
	// 水层地板：目标层 0.1，frog 范围 0.9~1.0 → 超界。正常 ×0.01，pity ×0.05。
	normal := fishRollContext{baits: []string{"rule_worm"}, now: time.Now(), rigType: 1, depthCM: 50}
	pity := normal
	pity.pityEnv = true
	kn, wn := s.applyBiteGates(s.dbCtx(), []string{"frog"}, []float64{100}, normal, cfg, "rules-test")
	kp, wp := s.applyBiteGates(s.dbCtx(), []string{"frog"}, []float64{100}, pity, cfg, "rules-test")
	if len(kn) != 1 || len(kp) != 1 {
		t.Fatalf("frog 应在两种模式下都幸存: normal=%v pity=%v", kn, kp)
	}
	if want := 100 * 0.01; wn[0] != want {
		t.Fatalf("正常地板 ×0.01, got %v", wn[0])
	}
	if want := 100 * 0.05; wp[0] != want {
		t.Fatalf("pity 地板 ×0.05, got %v", wp[0])
	}
	// G1 红线：拟饵(lure 0.2 < 0.30)即使 pityEnv 也必须剔除。
	lureCtx := fishRollContext{baits: []string{"rule_lure"}, now: time.Now()}
	lurePity := lureCtx
	lurePity.pityEnv = true
	if k, _ := s.applyBiteGates(s.dbCtx(), []string{"frog"}, []float64{100}, lureCtx, cfg, "rules-test"); len(k) != 0 {
		t.Fatal("G1 拟饵硬门应剔除 frog")
	}
	if k, _ := s.applyBiteGates(s.dbCtx(), []string{"frog"}, []float64{100}, lurePity, cfg, "rules-test"); len(k) != 0 {
		t.Fatal("pityEnv 不得放宽 G1 拟饵硬门")
	}
}
