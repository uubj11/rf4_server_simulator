package gameserver

import (
	"context"
	"testing"
	"time"
)

func TestStrictBaitRulesCoverage(t *testing.T) {
	if len(fishBaitRulesStrict) != 254 {
		t.Fatalf("strict rules=%d", len(fishBaitRulesStrict))
	}
	for name, r := range fishBaitRulesStrict {
		if len(r.allow) == 0 {
			t.Fatalf("strict rule %q has no allow list", name)
		}
	}
	if _, ok := lookupStrictFishBaitRule("常见拟鲤"); !ok {
		t.Fatal("strict rule missing 常见拟鲤")
	}
}

func TestStrictExactBaitModeIgnoresEvidenceAndCombos(t *testing.T) {
	s, db := seedBaitRuleTest(t)
	setBaitRuleSwitch(t, s, "1")
	if err := db.TunableSet(context.Background(), "bait_exact_only", "1", "test"); err != nil {
		t.Fatal(err)
	}
	s.tunMu.Lock()
	s.tunAt = time.Time{}
	s.tunMu.Unlock()

	// 即便有 companion 证据，补充饵也不能绕过严格 allow 清单。
	if _, err := db.SQL.Exec(`INSERT INTO companion_bait_fish(bait_id,fish_key,tier,evidence) VALUES('rule_other','c.roach',3,1)`); err != nil {
		t.Fatal(err)
	}
	for _, bait := range []string{"rule_other", "rule_lure", "missing"} {
		if f := s.rollFish("level_900_rules", bait); f != nil {
			t.Fatalf("strict mode let %q produce %s", bait, f.Species)
		}
	}
	if f := s.rollFish("level_900_rules", "rule_worm"); f == nil {
		t.Fatal("strict mode rejected listed bait")
	}

	// 组合饵必须每一项都在 allow 里；混入补充饵后整组不咬。
	keys, weights := s.filterFishBaitSet(
		"level_900_rules",
		[]string{"rule_worm", "rule_other"},
		[]string{"c.roach", "frog"},
		[]float64{1, 1},
		time.Now(),
	)
	if len(keys) != 0 || len(weights) != 0 {
		t.Fatalf("strict combo leaked: keys=%v weights=%v", keys, weights)
	}
}
