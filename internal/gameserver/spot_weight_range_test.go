package gameserver

import (
	"context"
	"testing"
)

// 体重区间退化（min_g == max_g）的鱼种，钓点情报曾直接显示「固定 3.6kg」。
// 真实出鱼重量走奖杯/蓝冠线，所以展示层要按线还原区间，不能照抄退化行。
func TestLocFishListExpandsDegenerateWeightRange(t *testing.T) {
	srv, db := newProgressSrv(t)
	ctx := context.Background()

	mustExec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.SQL.ExecContext(ctx, q, args...); err != nil {
			t.Fatalf("exec %q: %v", q, err)
		}
	}
	mustExec(`INSERT INTO loc_wiki_fish(loc, fish_key, fish_zh) VALUES('挪威海','katran','白斑角鲨')`)
	mustExec(`INSERT OR REPLACE INTO species_weight
		(fish_key,min_g,max_g,trophy_g,trophy_line_g,super_trophy_line_g,avg_g,samples,src)
		VALUES('katran',3596,3596,3236,7000,10000,3596,1,'passive')`)
	mustExec(`INSERT INTO loc_wiki_fish(loc, fish_key, fish_zh) VALUES('挪威海','normal_fish','正常鱼')`)
	mustExec(`INSERT OR REPLACE INTO species_weight
		(fish_key,min_g,max_g,trophy_g,trophy_line_g,super_trophy_line_g,avg_g,samples,src)
		VALUES('normal_fish',500,9000,4000,7000,11000,4200,88,'active')`)

	list := srv.locFishList(ctx, "挪威海")
	got := map[string]spotFishEntry{}
	for _, e := range list {
		got[e.key] = e
	}

	deg, ok := got["katran"]
	if !ok {
		t.Fatalf("挪威海缺 katran 条目：%+v", list)
	}
	// 下限按 grade1 口径 0.15×达标线，上限按蓝冠带顶端 1.2×蓝冠线
	if deg.minG < 450 || deg.minG > 520 {
		t.Fatalf("katran 下限 %.0f g，期望约 485 g（0.15×达标线 3236）", float64(deg.minG))
	}
	if deg.maxG < 11000 || deg.maxG > 13000 {
		t.Fatalf("katran 上限 %d g，期望约 12000 g（1.2×蓝冠线 10000）", deg.maxG)
	}
	if deg.maxG <= deg.minG {
		t.Fatalf("区间仍然退化：min=%d max=%d", deg.minG, deg.maxG)
	}

	// 没退化的行必须原样透出，不能被兜底逻辑改写
	if n := got["normal_fish"]; n.minG != 500 || n.maxG != 9000 {
		t.Fatalf("正常鱼区间被改写：min=%d max=%d，期望 500/9000", n.minG, n.maxG)
	}
}
