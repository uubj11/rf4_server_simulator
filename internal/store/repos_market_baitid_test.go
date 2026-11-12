package store

import (
	"context"
	"path/filepath"
	"testing"
)

// 静态饵效表里有少数记录按自制配方 ID（craft_bait_*）落库，而服务端判鱼时
// 传到 BaitTiers 的是产出物 ID（dough_cottage 一类）。两者对不上时饵效整段
// 被跳过，手搓饵拿不到本该有的档位，这里锁住回退行为。
func TestBaitTiersFallsBackToRecipeIDForCraftedBait(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "bait-tiers-recipe.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	recipe := func(recipeID, name, outputID string) {
		t.Helper()
		if _, err := db.SQL.ExecContext(ctx,
			`INSERT INTO recipe(recipe_id,name,output_id,output_qty,materials_json,duration_sec,kind)
			 VALUES(?,?,?,1,'[]',1,'misc')`, recipeID, name, outputID); err != nil {
			t.Fatalf("insert recipe %s: %v", recipeID, err)
		}
	}
	tier := func(baitID, fishKey string, tier, evidence int64) {
		t.Helper()
		if _, err := db.SQL.ExecContext(ctx,
			`INSERT INTO companion_bait_fish(bait_id,fish_key,tier,evidence) VALUES(?,?,?,?)`,
			baitID, fishKey, tier, evidence); err != nil {
			t.Fatalf("insert tier %s/%s: %v", baitID, fishKey, err)
		}
	}

	// 奶渣面团：档位挂在配方 ID 上，背包里的产出物 ID 是 dough_cottage。
	recipe("craft_bait_dough_cottage", "奶渣面团", "dough_cottage")
	tier("craft_bait_dough_cottage", "bream", 3, 631)
	tier("craft_bait_dough_cottage", "e_bream", 3, 181)
	tier("craft_bait_dough_cottage", "c.carp", 1, 53)

	// 湿面包：产出物 ID 自己也有一份记录，必须优先命中自己的那份。
	recipe("craft_bait_bread", "湿面包", "bread")
	tier("bread", "bream", 1, 9)
	tier("craft_bait_bread", "bream", 2, 77)

	got := db.BaitTiers(ctx, "dough_cottage")
	if got == nil {
		t.Fatalf("BaitTiers(dough_cottage) 未回退到配方 ID，返回 nil")
	}
	if got["bream"] != 3 || got["e_bream"] != 3 || got["c.carp"] != 1 {
		t.Fatalf("回退结果不符: %+v", got)
	}

	// 配方 ID 自身仍可直接查。
	if direct := db.BaitTiers(ctx, "craft_bait_dough_cottage"); direct["bream"] != 3 {
		t.Fatalf("配方 ID 直查失效: %+v", direct)
	}

	// 成品 ID 有记录时不得被配方回退覆盖。
	if bread := db.BaitTiers(ctx, "bread"); bread["bream"] != 1 {
		t.Fatalf("成品 ID 应优先，实得 bread bream=%d", bread["bream"])
	}

	if m := db.BaitTiers(ctx, "no_such_bait_at_all"); m != nil {
		t.Fatalf("未知饵应返回 nil，实得 %+v", m)
	}
	if m := db.BaitTiers(ctx, ""); m != nil {
		t.Fatalf("空饵 ID 应返回 nil，实得 %+v", m)
	}
}
