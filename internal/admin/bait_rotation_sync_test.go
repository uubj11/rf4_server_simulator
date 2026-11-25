package admin

import (
	"context"
	"io/fs"
	"strings"
	"testing"

	"rf4server/internal/store"
)

func mustOverride(fishZh string) store.FishBaitOverride {
	return store.FishBaitOverride{
		FishZh:     fishZh,
		Allow:      []string{"红蚯蚓"},
		Deny:       []string{},
		BottomOnly: []string{},
		Groups:     [][]string{{"红蚯蚓", "蚯蚓"}, {"鱼肉", "红虫"}},
		RotateSec:  600,
		RotateMode: "soft",
		Enabled:    true,
	}
}

// 鱼饵偏好页配置的轮换，必须在钓场鱼口页（/api/fish/config）同步可见：
// FishConfigRow 需要携带 fish_bait_override 的轮换组与开关，
// 否则钓场页只有权重和启用状态，无法显示轮换。
func TestFishConfigCarriesBaitRotation(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	if err := db.EnsureAdmin(ctx, "admin", "admin123"); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO loc_wiki_fish(loc,fish_key,fish_zh,in_map_roster,in_habitat) VALUES('铜湖','c.roach','常见拟鲤',1,1)`,
		`INSERT INTO species_weight(fish_key,min_g,max_g) VALUES('c.roach',60,1200)`,
	} {
		if _, err := db.SQL.Exec(q); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	// 直接在库层写一条轮换覆盖（等价于后台保存的结果）。
	if err := db.SaveFishBaitOverride(ctx, mustOverride("常见拟鲤"), "test"); err != nil {
		t.Fatal(err)
	}

	cfg, err := db.GetAdminFishConfig(ctx, "铜湖")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, f := range cfg.Fish {
		if f.FishKey != "c.roach" {
			continue
		}
		found = true
		if f.BaitSource != "override" {
			t.Fatalf("bait_source=%q，期望 override（钓场页据此判断有无轮换）", f.BaitSource)
		}
		if !f.BaitEnabled {
			t.Fatal("bait_enabled 应为 true")
		}
		if len(f.BaitGroups) != 2 {
			t.Fatalf("bait_groups 组数=%d，期望 2（实得 %v）", len(f.BaitGroups), f.BaitGroups)
		}
		if f.BaitRotSec != 600 {
			t.Fatalf("bait_rotate_sec=%d，期望 600", f.BaitRotSec)
		}
		if f.BaitRotMode != "soft" {
			t.Fatalf("bait_rotate_mode=%q，期望 soft", f.BaitRotMode)
		}
	}
	if !found {
		t.Fatalf("鱼口配置里没有 c.roach，实际 %d 条", len(cfg.Fish))
	}
}

// 没有覆盖时 bait_source 必须是 default，避免前端误显示轮换。
func TestFishConfigWithoutOverrideHasDefaultSource(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	if _, err := db.SQL.Exec(
		`INSERT INTO loc_wiki_fish(loc,fish_key,fish_zh,in_map_roster,in_habitat) VALUES('铜湖','c.roach','常见拟鲤',1,1)`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	cfg, err := db.GetAdminFishConfig(ctx, "铜湖")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range cfg.Fish {
		if f.FishKey != "c.roach" {
			continue
		}
		if f.BaitSource != "default" {
			t.Fatalf("无覆盖时 bait_source=%q，期望 default", f.BaitSource)
		}
		if len(f.BaitGroups) != 0 {
			t.Fatalf("无覆盖时 bait_groups 应为空，实得 %v", f.BaitGroups)
		}
		return
	}
	t.Fatal("鱼口配置里没有 c.roach")
}

// 前端约束：当前生效的钓场模块必须渲染后端权威轮换数据，鱼饵偏好保存后必须重新加载。
func TestActiveFishPageRendersBaitRotation(t *testing.T) {
	waters, err := fs.ReadFile(WebFS(), "app/pages/waters.js")
	if err != nil {
		t.Fatal(err)
	}
	bait, err := fs.ReadFile(WebFS(), "app/pages/bait.js")
	if err != nil {
		t.Fatal(err)
	}
	for _, sub := range []string{
		"row.bait_groups",                    // 用后端权威轮换组
		"row.bait_rotate_sec",                // 轮换时间来自同一行
		"baitCell(f, baitByKey[f.fish_key])", // 钓场列表按鱼行渲染饵料摘要
	} {
		if !strings.Contains(string(waters), sub) {
			t.Errorf("当前钓场模块缺少轮换同步标记：%q", sub)
		}
	}
	for _, sub := range []string{
		"await put('/api/bait/rules'", // 保存覆盖规则
		"reload();",                   // 保存后刷新偏好列表
	} {
		if !strings.Contains(string(bait), sub) {
			t.Errorf("当前鱼饵模块缺少保存后刷新标记：%q", sub)
		}
	}
}
