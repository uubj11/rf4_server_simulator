package gameserver

import (
	"context"
	"math"
	"testing"
)

// 等级曲线：累计渔获数落在 map_prof_steps 的第几档；0 是第一档，超出末档封顶。
func TestMapProfLevelAndSteps(t *testing.T) {
	steps := mapProfStepList(defMapProfSteps)
	if len(steps) != 10 || steps[0] != 0 || steps[9] != 4800 {
		t.Fatalf("默认档位解析错误: %v", steps)
	}
	cases := []struct {
		catches int64
		want    int
	}{
		{0, 0}, {9, 0}, {10, 1}, {29, 1}, {30, 2}, {69, 2}, {70, 3},
		{150, 4}, {600, 6}, {4799, 8}, {4800, 9}, {99999, 9},
	}
	for _, tc := range cases {
		if got := mapProfLevelFor(tc.catches, steps); got != tc.want {
			t.Fatalf("%d 尾等级=%d, want %d", tc.catches, got, tc.want)
		}
	}
	// 非法/空档位表：一律 Lv0，不能 panic。
	if got := mapProfLevelFor(500, mapProfStepList("")); got != 0 {
		t.Fatalf("空档位等级=%d, want 0", got)
	}
	if got := mapProfLevelFor(500, mapProfStepList("30,10,0")); got != 0 {
		t.Fatalf("非单调档位必须退回 Lv0，实得 %d", got)
	}
}

// 稀有度权重：普通鱼不抬，稀有/超稀有随等级线性提升；关闭或 Lv0 恒 1。
func TestMapProfSpeciesWeight(t *testing.T) {
	const step = 0.05
	cases := []struct {
		rarity, level int64
		step          float64
		want          float64
	}{
		{0, 9, step, 1.0},
		{1, 0, step, 1.0},
		{1, 9, step, 1.45},
		{2, 9, step, 1.9},
		{2, 3, step, 1.3},
		{2, 9, 0, 1.0},
	}
	for _, tc := range cases {
		got := mapProfSpeciesWeight(tc.rarity, tc.level, tc.step)
		if math.Abs(got-tc.want) > 1e-9 {
			t.Fatalf("rarity=%d level=%d step=%v 倍率=%v, want %v",
				tc.rarity, tc.level, tc.step, got, tc.want)
		}
	}
}

// 重抽概率：每级线性增长，封顶 0.9；关闭为 0。
func TestMapProfHeavyChance(t *testing.T) {
	cases := []struct {
		level int64
		step  float64
		want  float64
	}{
		{0, 0.02, 0},
		{9, 0.02, 0.18},
		{100, 0.02, 0.9},
		{9, 0, 0},
	}
	for _, tc := range cases {
		if got := mapProfHeavyChanceFor(tc.level, tc.step); math.Abs(got-tc.want) > 1e-9 {
			t.Fatalf("level=%d step=%v 概率=%v, want %v", tc.level, tc.step, got, tc.want)
		}
	}
}

// 重抽实现：命中取两次较大值，不命中只保留首抽。
func TestMapProfHeavyDraw(t *testing.T) {
	seq := []float64{420, 1180}
	i := 0
	sample := func() float64 {
		v := seq[i%len(seq)]
		i++
		return v
	}
	if got := mapProfHeavyDraw(0.5, sample, func() float64 { return 0.2 }); got != 1180 {
		t.Fatalf("命中重抽应取较大值 1180，实得 %v", got)
	}
	i = 0
	if got := mapProfHeavyDraw(0.5, sample, func() float64 { return 0.8 }); got != 420 {
		t.Fatalf("未命中不得重抽，实得 %v", got)
	}
	i = 0
	if got := mapProfHeavyDraw(0, sample, func() float64 { return 0.0 }); got != 420 {
		t.Fatalf("概率 0 不得重抽，实得 %v", got)
	}
}

// 接线：只抬稀有/超稀有候选，普通鱼与顺序都不变。
func TestApplyMapProfSpeciesBias(t *testing.T) {
	keys := []string{"common", "rare1", "rare2"}
	wts := []float64{10, 10, 10}
	rarity := map[string]int64{"common": 0, "rare1": 1, "rare2": 2}
	applyMapProfSpeciesBias(keys, wts, rarity, 9, 0.05)
	want := []float64{10, 14.5, 19}
	for i := range want {
		if math.Abs(wts[i]-want[i]) > 1e-9 {
			t.Fatalf("%s 权重=%v, want %v", keys[i], wts[i], want[i])
		}
	}
	// Lv0：一个数都不许动。
	wts = []float64{10, 10, 10}
	applyMapProfSpeciesBias(keys, wts, rarity, 0, 0.05)
	for i, w := range wts {
		if w != 10 {
			t.Fatalf("Lv0 不得加权：%s=%v", keys[i], w)
		}
	}
}

// 结算计数与跨级：9→10 尾跨 Lv1，10→11 尾不再跨级。
func TestMapProfCatchCountAndLevelUp(t *testing.T) {
	srv, db := newProgressSrv(t)
	ctx := context.Background()
	const uid, loc = int64(1000001), "level_001_lake"
	tx, err := db.SQL.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`INSERT INTO map_proficiency(uid,loc,catches,last_ts) VALUES(?,?,?,0)`,
		uid, loc, 9); err != nil {
		t.Fatal(err)
	}
	p1, err := srv.addMapProfCatchTx(ctx, tx, uid, loc, 1000)
	if err != nil {
		t.Fatal(err)
	}
	if p1.Before != 0 || p1.After != 1 || p1.Catches != 10 {
		t.Fatalf("9→10 尾应跨 Lv0→Lv1 且累计 10，实得 %+v", p1)
	}
	var catches int64
	if err := tx.QueryRow(`SELECT catches FROM map_proficiency WHERE uid=? AND loc=?`, uid, loc).Scan(&catches); err != nil {
		t.Fatal(err)
	}
	if catches != 10 {
		t.Fatalf("累计渔获=%d, want 10", catches)
	}
	after2, err := srv.addMapProfCatchTx(ctx, tx, uid, loc, 1001)
	if err != nil {
		t.Fatal(err)
	}
	if after2.Before != 1 || after2.After != 1 {
		t.Fatalf("10→11 尾不应再跨级，实得 %d→%d", after2.Before, after2.After)
	}
	if after2.Catches != 11 {
		t.Fatalf("累计渔获=%d, want 11", after2.Catches)
	}
}

// 关闭开关：发鱼侧不再读取等级，结算也不写表。
func TestMapProfDisabled(t *testing.T) {
	srv, db := newProgressSrv(t)
	ctx := context.Background()
	if _, err := db.SQL.Exec(`INSERT INTO tunables(key,value,updated_by,updated_ts) VALUES('map_prof_enabled','0','test',0)
		ON CONFLICT(key) DO UPDATE SET value='0'`); err != nil {
		t.Fatal(err)
	}
	srv.tunAt = srv.tunAt.Add(-24 * 60 * 60 * 1e9) // 逼缓存重载
	const uid, loc = int64(1000001), "level_001_lake"
	if _, err := db.SQL.Exec(`INSERT INTO map_proficiency(uid,loc,catches,last_ts) VALUES(?,?,?,0)`,
		uid, loc, 4800); err != nil {
		t.Fatal(err)
	}
	if prof := srv.mapProfForRoll(ctx, uid, loc); prof.level != 0 || prof.heavyChance != 0 || prof.rareStep != 0 {
		t.Fatalf("开关关闭仍读到 %+v", prof)
	}
	tx, err := db.SQL.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := srv.addMapProfCatchTx(ctx, tx, uid, loc, 1000); err != nil {
		t.Fatal(err)
	}
	var catches int64
	if err := tx.QueryRow(`SELECT catches FROM map_proficiency WHERE uid=? AND loc=?`, uid, loc).Scan(&catches); err != nil {
		t.Fatal(err)
	}
	if catches != 4800 {
		t.Fatalf("开关关闭不得写库，累计=%d", catches)
	}
}

// 结算路径：真实钓获走 settleCatchXP 后，本图累计尾数必须 +1。
func TestMapProfCountedOnSettlement(t *testing.T) {
	srv, db := newProgressSrv(t)
	ctx := context.Background()
	const uid, loc = int64(1000001), "level_001_lake"
	if _, err := db.EnsureProfile(ctx, uid); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL.Exec(`INSERT INTO map_proficiency(uid,loc,catches,last_ts) VALUES(?,?,?,0)`,
		uid, loc, 9); err != nil {
		t.Fatal(err)
	}
	fish := &RolledFish{FishGUID: newFishGUID(), Species: "perch", Grade: 2, WeightG: 642, LocLevel: loc}
	if res := srv.settleCatchXP(ctx, uid, fish, 5); res == nil {
		t.Fatal("结算返回空")
	}
	var catches int64
	if err := db.SQL.QueryRow(`SELECT catches FROM map_proficiency WHERE uid=? AND loc=?`, uid, loc).Scan(&catches); err != nil {
		t.Fatal(err)
	}
	if catches != 10 {
		t.Fatalf("结算后本图累计=%d, want 10", catches)
	}
}

// 重抽命中判定：掷点小于概率才命中，概率为 0 永不命中。
func TestMapProfHeavyHit(t *testing.T) {
	if !mapProfHeavyHit(0.1, func() float64 { return 0.05 }) {
		t.Fatal("掷点小于概率应命中")
	}
	if mapProfHeavyHit(0.1, func() float64 { return 0.5 }) {
		t.Fatal("掷点大于概率不应命中")
	}
	if mapProfHeavyHit(0, func() float64 { return 0 }) {
		t.Fatal("概率 0 不应命中")
	}
}

// 未达标占比随本图熟练度下调：挪走的份额补给合格档，总占比不变。
func TestMapProfGradeBelowPct(t *testing.T) {
	cases := []struct {
		level        int
		want, wantQd float64
	}{
		{0, 3, 87.5},
		{4, 2, 88.5},
		{9, 0.75, 89.75},
		{20, 0.5, 90}, // 触底 grade_pct_below_min
	}
	for _, tc := range cases {
		below := gradeBelowPctFor(3, 0.5, 0.25, tc.level)
		if math.Abs(below-tc.want) > 1e-9 {
			t.Fatalf("level=%d 未达标占比=%v, want %v", tc.level, below, tc.want)
		}
		srv, db := newDetachTestServer(t)
		enableGradeRatio(t, srv, db)
		lines := cachedGradeLines{val: ptrI64(1000), trophy: ptrI64(5000), blue: ptrI64(8000)}
		model, ok := srv.gradeRatioModel(lines, 1, 1, 0, tc.level)
		if !ok || len(model.weights) == 0 {
			t.Fatal("档位模型不可用")
		}
		// 未达标档权重必须等于上面的比例；合格档必须补上挪走的份额。
		var belowW, qdW float64
		for i, band := range model.bands {
			switch band.grade {
			case 1:
				belowW = model.weights[i]
			case 2:
				qdW = model.weights[i]
			}
		}
		if belowW != 0 && math.Abs(belowW-tc.want) > 1e-9 {
			t.Fatalf("level=%d 模型未达标权重=%v, want %v", tc.level, belowW, tc.want)
		}
		if math.Abs(qdW-tc.wantQd) > 1e-9 {
			t.Fatalf("level=%d 合格权重=%v, want %v", tc.level, qdW, tc.wantQd)
		}
	}
}

// 跨级播报文案。
func TestMapProfLevelUpText(t *testing.T) {
	got := mapProfLevelUpText("克马羚诺也湖", 3, 41)
	want := "「克马羚诺也湖」钓鱼熟练度 Lv.3（累计 41 尾）"
	if got != want {
		t.Fatalf("播报=%q, want %q", got, want)
	}
	if got := mapProfLevelUpText("", 1, 10); got != "「未知水域」钓鱼熟练度 Lv.1（累计 10 尾）" {
		t.Fatalf("地图名缺失时应回退占位，实得 %q", got)
	}
}
