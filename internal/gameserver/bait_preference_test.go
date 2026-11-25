package gameserver

import (
	"context"
	"testing"
	"time"
)

func TestFishBaitPreferenceAlternateCombinationAndChange(t *testing.T) {
	s, db := seedBaitRuleTest(t)
	if _, err := db.SQL.Exec(`INSERT INTO item_def(numeric_id,item_id,category,attrs_json) VALUES(999804,'rule_fly',27,'{"fopbhlmkcpe":7}'); INSERT INTO item_catalog(item_id,name_zh) VALUES('rule_fly','苍蝇')`); err != nil {
		t.Fatal(err)
	}
	now := time.Unix(worldDayEpochUnix+1500, 0)
	cache := newFishingRollCache()
	weight := func(baits []string, at time.Time) float64 {
		t.Helper()
		keys, w := s.filterFishBaitSet("rules-test", baits, []string{"c.roach"}, []float64{1}, at, fishRollContext{cache: cache})
		if len(keys) == 0 {
			return 0
		}
		return w[0]
	}
	favorite := weight([]string{"rule_worm"}, now)
	other := weight([]string{"rule_other"}, now)
	// 同食性兜底从 maxBaitPreference*0.05（≈0.086）提到 *0.20（≈0.345）：
	// 兜底仍然远低于规则放行的满档，但不再等同于「几乎钓不到」。
	if other <= 0 || other >= favorite || other > maxBaitPreference*legalAlternativeShare+1e-9 || favorite < 1 {
		t.Fatalf("favorite=%f other=%f", favorite, other)
	}
	if weight([]string{"rule_worm", "rule_fly"}, now) <= favorite {
		t.Fatal("two preferred baits must participate together")
	}
	if weight([]string{"rule_worm", "rule_worm"}, now) != favorite || weight([]string{"rule_fly", "rule_worm"}, now) != weight([]string{"rule_worm", "rule_fly"}, now) {
		t.Fatal("duplicates or slot ordering changed bait preference")
	}
	// 禁饵只作用于它自己那一款：同杆里合法的蚯蚓仍然有效（多钩组不能连坐），
	// 单独挂禁饵、坏 id、空列表则必须一片饵都不给。
	if weight([]string{"rule_lure"}, now) != 0 {
		t.Fatal("banned lure alone must not feed the fish")
	}
	if got := weight([]string{"rule_worm", "rule_lure"}, now); got != favorite {
		t.Fatalf("banned bait must be skipped, not poison the legal one: %f != %f", got, favorite)
	}
	if weight([]string{"missing"}, now) != 0 || weight(nil, now) != 0 {
		t.Fatal("invalid or absent bait must not be scored")
	}
	if favorite == weight([]string{"rule_worm"}, now.Add(distributionPeriodRealHours*time.Hour)) {
		t.Fatal("bait preference did not vary over game time")
	}
	setBaitRuleSwitch(t, s, "1")
	// 强制只选拟鲤，验证差异也传到了实际派鱼概率，而非仅改变鱼种比例。
	if _, err := db.SQL.Exec(`INSERT INTO fish_weight_override(loc,fish_key,weight,enabled,updated_by,updated_ts) VALUES('rules-test','frog',0,0,'test',0),('rules-test','c.roach',1,1,'test',0)`); err != nil {
		t.Fatal(err)
	}
	preferredHits, otherHits := 0, 0
	for i := 0; i < 160; i++ {
		if s.rollFishForRig(context.Background(), 0, "", "level_900_rules", "rule_worm", neutralFishingSkillBonus(), fishRollContext{cache: cache}) != nil {
			preferredHits++
		}
		if s.rollFishForRig(context.Background(), 0, "", "level_900_rules", "rule_other", neutralFishingSkillBonus(), fishRollContext{cache: cache}) != nil {
			otherHits++
		}
	}
	if preferredHits < 65 || otherHits > 35 || preferredHits <= otherHits {
		t.Fatalf("preferred hits=%d alternate hits=%d", preferredHits, otherHits)
	}
}
