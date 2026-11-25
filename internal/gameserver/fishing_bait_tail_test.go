package gameserver

import (
	"context"
	"fmt"
	"testing"
	"time"

	"rf4server/internal/store"
)

// 长尾闸：官方纪录里这款饵对这条鱼占比过低（或样本不足）时，不能靠
// companion 证据或本图历史记录兜底进判鱼池；规则明确点名的饵不受影响。
func TestBaitShareTailGate(t *testing.T) {
	s, db := seedBaitRuleTest(t)
	for _, q := range []string{
		`INSERT INTO item_def(numeric_id,item_id,category,attrs_json) VALUES
			(999821,'rule_potato',27,'{"fopbhlmkcpe":9}')`,
		`INSERT INTO item_catalog(item_id,name_zh) VALUES('rule_potato','马铃薯块')`,
		// companion 证据给足，验证证据兜底不能再绕过长尾闸
		`INSERT INTO companion_bait_fish(bait_id,fish_key,tier,evidence) VALUES
			('rule_potato','c.roach',3,900),('rule_potato','frog',3,900)`,
	} {
		if _, err := db.SQL.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	seq := 0
	ins := func(fish, bait string) {
		t.Helper()
		seq++
		if _, err := db.SQL.Exec(`INSERT INTO rf4records_catch(fish_name_en,fish_key,bait_name,bait_id,weight_g,waterbody,region,categories,trophy_class,catch_date,fetched_at) VALUES('x','` + fish + `','n','` + bait + `',` + fmt.Sprintf("%d", seq) + `,'','','','','',1)`); err != nil {
			t.Fatal(err)
		}
	}
	// 常见拟鲤 601 条纪录：rule_other 599、rule_worm 1（规则点名）、rule_potato 1（0.17%）
	for i := 0; i < 599; i++ {
		ins("c.roach", "rule_other")
	}
	ins("c.roach", "rule_worm")
	ins("c.roach", "rule_potato")
	// 青蛙只有 40 条纪录（< 100），长尾闸不生效
	for i := 0; i < 40; i++ {
		ins("frog", "rule_other")
	}
	s.tunMu.Lock()
	delete(s.tunCache, "bait_share_strong")
	s.tunCache["bait_share_min_count"] = 1
	s.tunAt = time.Now()
	s.tunMu.Unlock()
	s.loadRf4Share()

	cache := newFishingRollCache()
	now := time.Unix(worldDayEpochUnix+1500, 0)
	weight := func(fish, bait string) float64 {
		t.Helper()
		keys, w := s.filterFishBaitSet("rules-test", []string{bait}, []string{fish}, []float64{1}, now, fishRollContext{cache: cache})
		if len(keys) == 0 {
			return 0
		}
		return w[0]
	}

	// 1) 占比 99.7% 的饵正常满分
	if w := weight("c.roach", "rule_other"); w < 1 {
		t.Fatalf("高占比饵应满分: %f", w)
	}
	// 2) 规则点名但官方只有 1 条：规则优先，仍然可以咬
	if w := weight("c.roach", "rule_worm"); w <= 0 {
		t.Fatalf("规则点名的饵不应被长尾闸排除: %f", w)
	}
	// 3) 未被规则点名、官方 0.17%、companion tier3：必须出池
	if w := weight("c.roach", "rule_potato"); w != 0 {
		t.Fatalf("低占比且未被规则点名的饵必须出池: %f", w)
	}
	// 4) 纪录不足 100 条的鱼不受长尾闸影响
	if w := weight("frog", "rule_potato"); w <= 0 {
		t.Fatalf("证据不足的鱼不应被长尾闸清空: %f", w)
	}
}

func TestBaitNamedByRuleRejectsCategoryWildcards(t *testing.T) {
	for _, tt := range []struct {
		name string
		term string
		bait ruleBait
	}{
		{"any bait", "各类饵", ruleBait{name: "马铃薯块", category: 27, baitType: 9}},
		{"any baitfish", "各类饵鱼", ruleBait{name: "任意低占比饵鱼", category: 27, baitType: 4}},
		{"baitfish", "饵鱼", ruleBait{name: "低占比活饵鱼", category: 27, baitType: 4}},
		{"live bait", "活饵类", ruleBait{name: "低占比活饵鱼", category: 27, baitType: 4}},
		{"lure", "拟饵", ruleBait{name: "任意亮片", category: 17, lureType: 4}},
		{"worm", "蠕虫类", ruleBait{name: "某种蚯蚓", category: 27, baitType: 6}},
		{"insect larva", "昆虫幼虫", ruleBait{name: "某种幼虫", category: 27, baitType: 2}},
		{"fish meat", "鱼肉类", ruleBait{name: "某种鱼肉饵", category: 27, baitType: 14}},
		{"spinner", "亮片类", ruleBait{name: "某型号亮片", category: 17, lureType: 1}},
		{"soft lure", "软饵类", ruleBait{name: "某型号软饵", category: 17, lureType: 3}},
		{"salmon family", "鲑鱼类", ruleBait{name: "鲑鱼软饵", category: 27, baitType: 14}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if !tt.bait.matches(tt.term) {
				t.Fatalf("测试夹具未命中类别词 %q", tt.term)
			}
			r := fishBaitRule{allow: []string{tt.term}}
			if baitNamedByRule(r, tt.bait) {
				t.Fatalf("类别词 %q 不应被视为明确点名", tt.term)
			}
		})
	}
}

func TestBaitNamedByRuleKeepsSpecificNames(t *testing.T) {
	for _, tt := range []struct {
		name string
		term string
		bait ruleBait
	}{
		{"exact name", "蚯蚓", ruleBait{name: "蚯蚓", category: 27, baitType: 6}},
		{"lure series", "Hunter 1系列", ruleBait{name: "Hunter 1-001", category: 17, lureType: 4}},
		{"bait family", "Gummi Makk", ruleBait{name: "Gummi Makk 10-001", category: 17, lureType: 3}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := fishBaitRule{allow: []string{tt.term}}
			if !baitNamedByRule(r, tt.bait) {
				t.Fatalf("具体饵名/系列 %q 应保留豁免", tt.term)
			}
		})
	}
}

func setRf4TailCache(s *Server, fishKey string, total float64, shares map[string]float64) {
	s.rf4ShareMu.Lock()
	s.rf4ShareCache = map[string]map[string]float64{fishKey: shares}
	s.rf4FishTotal = map[string]float64{fishKey: total}
	s.rf4ShareLoaded = true
	s.rf4ShareAt = time.Now()
	s.rf4ShareMu.Unlock()
}

func enableTailGrading(s *Server) {
	s.tunMu.Lock()
	delete(s.tunCache, "bait_share_strong")
	s.tunAt = time.Now()
	s.tunMu.Unlock()
}

func TestBaitShareTailGateRejectsCategoryWildcard(t *testing.T) {
	s, db := seedBaitRuleTest(t)
	for _, q := range []string{
		`INSERT INTO loc_wiki_fish(loc,fish_key,fish_zh) VALUES('rules-test','salmon','大西洋鲑鱼')`,
		`INSERT INTO item_def(numeric_id,item_id,category,attrs_json) VALUES(999821,'rule_livefish',27,'{"fopbhlmkcpe":4}')`,
		`INSERT INTO item_catalog(item_id,name_zh) VALUES('rule_livefish','任意低占比饵鱼')`,
		`INSERT INTO companion_bait_fish(bait_id,fish_key,tier,evidence) VALUES('rule_livefish','salmon',3,900)`,
	} {
		if _, err := db.SQL.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	enableTailGrading(s)
	setRf4TailCache(s, "salmon", 600, map[string]float64{"rule_livefish": 0.17})
	keys, weights := s.filterFishBaitSet("rules-test", []string{"rule_livefish"}, []string{"salmon"}, []float64{1}, time.Unix(worldDayEpochUnix+1500, 0), fishRollContext{cache: newFishingRollCache()})
	if len(keys) != 0 || len(weights) != 0 {
		t.Fatalf("各类饵鱼只是类别词，0.17%% 饵不得绕过长尾闸: keys=%v weights=%v", keys, weights)
	}
}

func TestBaitShareTailGateAllowsActiveRotationGroup(t *testing.T) {
	s, db := seedBaitRuleTest(t)
	for _, q := range []string{
		`INSERT INTO item_def(numeric_id,item_id,category,attrs_json) VALUES(999831,'rot_low',27,'{"fopbhlmkcpe":6}'),(999832,'rot_other',27,'{"fopbhlmkcpe":6}')`,
		`INSERT INTO item_catalog(item_id,name_zh) VALUES('rot_low','低占比轮换饵'),('rot_other','轮换其他饵')`,
		`INSERT INTO companion_bait_fish(bait_id,fish_key,tier,evidence) VALUES('rot_low','c.roach',3,900)`,
	} {
		if _, err := db.SQL.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	ov := store.FishBaitOverride{
		FishZh: "常见拟鲤", Allow: []string{"不存在的静态饵"},
		Groups:    [][]string{{"低占比轮换饵"}, {"轮换其他饵"}},
		RotateSec: 600, RotateMode: "strict", Enabled: true,
	}
	if err := db.SaveFishBaitOverride(context.Background(), ov, "test"); err != nil {
		t.Fatal(err)
	}
	s.invalidateBaitOverrides()
	enableTailGrading(s)
	setRf4TailCache(s, "c.roach", 600, map[string]float64{"rot_other": 99.83})
	at := time.Unix(1_800_000_000, 0)
	for (at.Unix()/600)%2 != 0 {
		at = at.Add(600 * time.Second)
	}
	keys, weights := s.filterFishBaitSet("rules-test", []string{"rot_low"}, []string{"c.roach"}, []float64{1}, at, fishRollContext{cache: newFishingRollCache()})
	if len(keys) != 1 || len(weights) != 1 || weights[0] <= 0 {
		t.Fatalf("当前轮换组必须被视为本轮主推饵: keys=%v weights=%v", keys, weights)
	}
}

func TestBaitShareTailGateRequiresReadyCache(t *testing.T) {
	s, db := seedBaitRuleTest(t)
	for _, q := range []string{
		`INSERT INTO item_def(numeric_id,item_id,category,attrs_json) VALUES(999841,'refresh_bait',27,'{"fopbhlmkcpe":9}'),(999842,'refresh_other',27,'{"fopbhlmkcpe":9}')`,
		`INSERT INTO item_catalog(item_id,name_zh) VALUES('refresh_bait','刷新窗口饵'),('refresh_other','刷新其他饵')`,
		`INSERT INTO companion_bait_fish(bait_id,fish_key,tier,evidence) VALUES('refresh_bait','c.roach',3,900)`,
	} {
		if _, err := db.SQL.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	enableTailGrading(s)
	setRf4TailCache(s, "c.roach", 600, map[string]float64{"refresh_other": 99.83})
	now := time.Unix(worldDayEpochUnix+1500, 0)
	keys, _ := s.filterFishBaitSet("rules-test", []string{"refresh_bait"}, []string{"c.roach"}, []float64{1}, now, fishRollContext{cache: newFishingRollCache()})
	if len(keys) != 0 {
		t.Fatalf("缓存已就绪时，低于 min_count 的已知样本仍应触发长尾闸: %v", keys)
	}

	s.rf4ShareMu.Lock()
	s.rf4ShareAt = time.Now().Add(-31 * time.Minute)
	s.rf4ShareMu.Unlock()
	s.rf4ShareLoading.Store(true) // 模拟异步刷新中，此时 share 与 total 都不可用
	defer s.rf4ShareLoading.Store(false)
	keys, weights := s.filterFishBaitSet("rules-test", []string{"refresh_bait"}, []string{"c.roach"}, []float64{1}, now, fishRollContext{cache: newFishingRollCache()})
	if len(keys) != 1 || len(weights) != 1 || weights[0] <= 0 {
		t.Fatalf("刷新中的未知占比必须回退 companion/local 旧语义: keys=%v weights=%v", keys, weights)
	}
}
