package gameserver

import (
	"context"
	"math"
	"testing"
	"time"
)

// setRf4Shares 直接写入两类 rf4 占比快照，避免测试依赖真实 rf4records_catch。
func setRf4Shares(s *Server, baitShares map[string]map[string]float64, lureShares map[string]float64) {
	s.rf4ShareMu.Lock()
	s.rf4ShareCache = baitShares
	s.rf4LureShare = lureShares
	s.rf4ShareLoaded = true
	s.rf4ShareAt = time.Now()
	s.rf4ShareMu.Unlock()
}

func setLureShare(s *Server, m map[string]float64) {
	setRf4Shares(s, nil, m)
}

// 拟饵门按鱼种在官方纪录里的拟饵占比分档：<1% 出池、1~5% 满档但压到四分之一、
// ≥5% 不干预；双壳类与螯虾不吃移动拟饵，无论鱼种或具体饵占比都不参与判鱼。
func TestLureGateByFishShare(t *testing.T) {
	s, db := seedBaitRuleTest(t)
	setWeatherTestTunable(t, s, "bait_share_strong", "5")
	setWeatherTestTunable(t, s, "bait_share_medium", "1")
	setWeatherTestTunable(t, s, "bait_share_tail", "0.2")
	for _, q := range []string{
		`INSERT INTO loc_wiki_fish(loc,fish_key,fish_zh) VALUES('rules-test','n.pike','常见狗鱼'),('rules-test','freshwater_mussel','珠蚌')`,
	} {
		if _, err := db.SQL.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now()
	bite := func(fish string) ([]string, []float64) {
		return s.filterFishBaitSet("rules-test", []string{"rule_lure"}, []string{fish}, []float64{1}, now)
	}
	natural := func(fish string) ([]string, []float64) {
		return s.filterFishBaitSet("rules-test", []string{"rule_worm"}, []string{fish}, []float64{1}, now)
	}

	// ≥5%：正常参与判鱼。
	setLureShare(s, map[string]float64{"n.pike": 50, "freshwater_mussel": 50})
	highKeys, highW := bite("n.pike")
	if len(highKeys) != 1 || highW[0] <= 0 {
		t.Fatalf("high share: keys=%v weights=%v", highKeys, highW)
	}

	// 1~5%：还在池里，但满档权重压到四分之一。
	setLureShare(s, map[string]float64{"n.pike": 3})
	midKeys, midW := bite("n.pike")
	if len(midKeys) != 1 {
		t.Fatalf("mid share excluded: %v", midKeys)
	}
	if ratio := midW[0] / highW[0]; math.Abs(ratio-0.25) > 1e-9 {
		t.Fatalf("mid share ratio=%g want 0.25", ratio)
	}

	// <1%：直接出池。
	setLureShare(s, map[string]float64{"n.pike": 0.5})
	if keys, _ := bite("n.pike"); len(keys) != 0 {
		t.Fatalf("low share still bit: %v", keys)
	}

	// 无官方拟饵纪录（-1）时按未知处理，不能误伤。
	setLureShare(s, map[string]float64{})
	if keys, _ := bite("n.pike"); len(keys) != 1 {
		t.Fatalf("unknown share excluded: %v", keys)
	}

	// 具体饵强证据仍优先于鱼种级低占比（硬排除鱼种除外）。
	setRf4Shares(s,
		map[string]map[string]float64{"n.pike": {"rule_lure": 50}},
		map[string]float64{"n.pike": 0.5},
	)
	if keys, _ := bite("n.pike"); len(keys) != 1 {
		t.Fatalf("strong bait evidence excluded: %v", keys)
	}

	// 双壳类：鱼种级占比再高也不吃移动拟饵。
	setLureShare(s, map[string]float64{"freshwater_mussel": 90})
	if keys, _ := bite("freshwater_mussel"); len(keys) != 0 {
		t.Fatalf("bivalve accepted lure: %v", keys)
	}

	// 天然饵不受拟饵门影响。
	setLureShare(s, map[string]float64{"n.pike": 0.5})
	if keys, _ := natural("n.pike"); len(keys) != 1 {
		t.Fatalf("natural bait gated by lure share: %v", keys)
	}
}

func TestLureGateHardExclusionBeatsStrongBaitShare(t *testing.T) {
	s, db := seedBaitRuleTest(t)
	setWeatherTestTunable(t, s, "bait_share_strong", "5")
	setWeatherTestTunable(t, s, "bait_share_medium", "1")
	setWeatherTestTunable(t, s, "bait_share_tail", "0.2")
	for _, q := range []string{
		`INSERT INTO loc_wiki_fish(loc,fish_key,fish_zh) VALUES
			('rules-test','freshwater_mussel','珠蚌'),
			('rules-test','dreissena','河斑马纹贻贝'),
			('rules-test','crawfish','河螯虾')`,
	} {
		if _, err := db.SQL.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	fish := []string{"freshwater_mussel", "dreissena", "crawfish"}
	baitShares := map[string]map[string]float64{}
	lureShares := map[string]float64{}
	for _, key := range fish {
		// 故意构造“具体饵强证据 + 鱼种高占比”双强场景；硬排除仍应生效。
		baitShares[key] = map[string]float64{"rule_lure": 50}
		lureShares[key] = 90
	}
	setRf4Shares(s, baitShares, lureShares)
	for _, key := range fish {
		keys, _ := s.filterFishBaitSet("rules-test", []string{"rule_lure"}, []string{key}, []float64{1}, time.Now())
		if len(keys) != 0 {
			t.Fatalf("hard-excluded fish %s accepted lure: %v", key, keys)
		}
	}
}

func TestLureGateDisabledWithShareThresholdsRestoresLegacy(t *testing.T) {
	s, db := seedBaitRuleTest(t)
	if _, err := db.SQL.Exec(`INSERT INTO loc_wiki_fish(loc,fish_key,fish_zh) VALUES
		('rules-test','n.pike','常见狗鱼'),
		('rules-test','freshwater_mussel','珠蚌')`); err != nil {
		t.Fatal(err)
	}
	setWeatherTestTunable(t, s, "bait_share_strong", "0")
	setWeatherTestTunable(t, s, "bait_share_medium", "0")
	setWeatherTestTunable(t, s, "bait_share_tail", "0")
	setLureShare(s, map[string]float64{"n.pike": 0.2, "freshwater_mussel": 0.2})

	for _, fish := range []string{"n.pike", "freshwater_mussel"} {
		keys, _ := s.filterFishBaitSet("rules-test", []string{"rule_lure"}, []string{fish}, []float64{1}, time.Now())
		if len(keys) != 1 {
			t.Fatalf("disabled grading did not restore legacy behavior for %s: %v", fish, keys)
		}
	}
}

func TestRf4LureShareTTLExpiresToUnknown(t *testing.T) {
	s, _ := seedBaitRuleTest(t)
	setLureShare(s, map[string]float64{"n.pike": 50})
	if got := s.rf4LureShareOf("n.pike"); got != 50 {
		t.Fatalf("fresh share=%g want 50", got)
	}

	s.rf4ShareMu.Lock()
	s.rf4ShareAt = time.Now().Add(-rf4ShareTTL - time.Second)
	s.rf4ShareMu.Unlock()
	// 阻止过期读取触发异步全表聚合，只验证读取侧的 TTL 契约。
	s.rf4ShareLoading.Store(true)
	defer s.rf4ShareLoading.Store(false)
	if got := s.rf4LureShareOf("n.pike"); got != -1 {
		t.Fatalf("expired share=%g want -1", got)
	}
}

func TestRf4ShareQueryErrorClearsStaleSnapshot(t *testing.T) {
	s, db := seedBaitRuleTest(t)
	setRf4Shares(s,
		map[string]map[string]float64{"n.pike": {"rule_lure": 50}},
		map[string]float64{"n.pike": 50},
	)
	if _, err := db.SQL.Exec(`DROP TABLE rf4records_catch`); err != nil {
		t.Fatal(err)
	}
	s.loadRf4Share()

	if got := s.rf4LureShareOf("n.pike"); got != -1 {
		t.Fatalf("stale lure share survived query error: %g", got)
	}
	if got := s.rf4BaitShare(context.Background(), "n.pike", "rule_lure"); got != -1 {
		t.Fatalf("stale bait share survived query error: %g", got)
	}
	s.rf4ShareMu.RLock()
	defer s.rf4ShareMu.RUnlock()
	if len(s.rf4ShareCache) != 0 || len(s.rf4LureShare) != 0 || len(s.rf4FishTotal) != 0 {
		t.Fatalf("stale snapshots not cleared: bait=%v lure=%v total=%v", s.rf4ShareCache, s.rf4LureShare, s.rf4FishTotal)
	}
}

// TestRf4ShareSQLAggregationDenominators 用真实的 rf4records_catch 聚合锁定两个分母口径：
// 饵级占比的分母只算已映射纪录（饵必须能归到具体 item_id），鱼种级拟饵占比的
// 分母算该鱼全部有效渔获（未映射饵按非拟饵计）。鱼种级分子与分母必须由同一条聚合 SQL 算出。
func TestRf4ShareSQLAggregationDenominators(t *testing.T) {
	s, db := seedBaitRuleTest(t)
	seq := 0
	ins := func(fish, bait string, n int) {
		t.Helper()
		for i := 0; i < n; i++ {
			seq++
			if _, err := db.SQL.Exec(`INSERT INTO rf4records_catch(fish_name_en,fish_key,bait_name,bait_id,weight_g,waterbody,region,categories,trophy_class,catch_date,fetched_at) VALUES('x',?,'n',?,?,'','','','','',1)`,
				fish, bait, seq); err != nil {
				t.Fatal(err)
			}
		}
	}
	// c.roach：拟饵 6 条 + 天然饵 4 条 + 未映射 5 条 → 已映射 10 条、全部渔获 15 条。
	ins("c.roach", "rule_lure", 6)
	ins("c.roach", "rule_worm", 4)
	ins("c.roach", "", 5)
	// frog：只有天然饵 3 条，没有任何拟饵纪录。
	ins("frog", "rule_worm", 3)
	// ghost：只有未映射纪录，一条已映射都没有 → 占比按未知处理。
	ins("ghost", "", 4)
	s.loadRf4Share()

	ctx := context.Background()
	if got := s.rf4BaitShare(ctx, "c.roach", "rule_lure"); math.Abs(got-60) > 1e-9 {
		t.Fatalf("bait share=%g want 60 (6/10 已映射)", got)
	}
	// 样本低于 bait_share_min_count(5) 的饵不进饵级表，但仍计入分母。
	if got := s.rf4BaitShare(ctx, "c.roach", "rule_worm"); got != -1 {
		t.Fatalf("below min count entered bait share: %g", got)
	}
	s.rf4ShareMu.RLock()
	total := s.rf4FishTotal["c.roach"]
	s.rf4ShareMu.RUnlock()
	if total != 10 {
		t.Fatalf("fish total=%g want 10 (已映射分母)", total)
	}
	// 鱼种级拟饵占比是 6/15=40%，不是 6/10=60%：未映射饵按非拟饵计入分母。
	if got := s.rf4LureShareOf("c.roach"); math.Abs(got-40) > 1e-9 {
		t.Fatalf("lure share=%g want 40 (6/15 全部渔获)", got)
	}
	if got := s.rf4LureShareOf("frog"); got != 0 {
		t.Fatalf("no-lure fish share=%g want 0", got)
	}
	// 没有已映射纪录的鱼不进表：没有可归类的饵就不该硬判 0%。
	if got := s.rf4LureShareOf("ghost"); got != -1 {
		t.Fatalf("unmapped-only fish share=%g want -1 (未知)", got)
	}
}
