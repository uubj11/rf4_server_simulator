package gameserver

import (
	"context"
	"math"
	"testing"
	"time"
)

// disableSpawnFloor 关掉本服 spawn_min_ratio 加严，回到官服口径。
// 本服默认 0.6 会把 1000g 达标线的未达标下界压到 600g，掩盖官服 min_g 语义。
func disableSpawnFloor(srv *Server) {
	srv.tunMu.Lock()
	srv.tunCache = map[string]float64{tunableSpawnMinRatio: 0}
	srv.tunAt = time.Now() // 30s 缓存把 tunAt 一并刷新，否则下次读库会覆盖掉本次设置
	srv.tunMu.Unlock()
}

// 未达标档的下界取鱼种自然最小体重（species_weight.min_g）。
func TestGradeRatioBelowBandHonorsSpeciesMinWeight(t *testing.T) {
	srv, db := newDetachTestServer(t)
	enableGradeRatio(t, srv, db)

	const val, tro, sup = int64(1000), int64(5000), int64(8000)
	const minG = int64(120)
	lines := cachedGradeLines{
		val: ptrI64(val), trophy: ptrI64(tro), blue: ptrI64(sup), minG: ptrI64(minG),
	}

	below := 0
	for i := 0; i < 4000; i++ {
		w, ok := srv.sampleWeightByGradeRatio(lines, 1, 1, 0)
		if !ok {
			t.Fatalf("第 %d 次抽样没有结果", i)
		}
		if gradeRatioGradeOf(w, val, tro, sup) != 1 {
			continue
		}
		below++
		if w < minG {
			t.Fatalf("未达标个体 %d g 低于该鱼种最小体重 %d g", w, minG)
		}
	}
	if below == 0 {
		t.Fatal("样本里没有未达标个体，用例无效")
	}
}

// 缺 min_g 数据时下界退回达标线的 15%（钳到 50 g），未达标档仍存在。
func TestGradeRatioBelowBandFallsBackToProportionalFloor(t *testing.T) {
	srv, db := newDetachTestServer(t)
	enableGradeRatio(t, srv, db)
	disableSpawnFloor(srv)

	const val, tro, sup = int64(1000), int64(5000), int64(8000)
	lines := cachedGradeLines{val: ptrI64(val), trophy: ptrI64(tro), blue: ptrI64(sup)}

	const wantLo = 50.0 // belowGradeFloorFromVal(1000) = 15% = 150，钳到 50
	model, handled := srv.gradeRatioModel(lines, 1, 1, 0)
	if !handled {
		t.Fatal("档位模型不可用")
	}
	found := false
	for _, b := range model.bands {
		if b.grade == 1 {
			found = true
			if math.Abs(b.lo-wantLo) > 1e-9 {
				t.Fatalf("min_g 缺失时未达标档下界应 = 兜底 15%%钳 50 g，实得 %.2f", b.lo)
			}
		}
	}
	if !found {
		t.Fatal("min_g 缺失时未达标档不应消失")
	}
}

// 数据异常（min_g ≥ 达标线）时使用比例兜底，保证未达标档存在。
func TestGradeRatioBelowBandGuardsMinAboveLine(t *testing.T) {
	srv, db := newDetachTestServer(t)
	enableGradeRatio(t, srv, db)
	disableSpawnFloor(srv)

	const val = int64(1000)
	lines := cachedGradeLines{
		val: ptrI64(val), trophy: ptrI64(5000), blue: ptrI64(8000), minG: ptrI64(1200),
	}
	model, handled := srv.gradeRatioModel(lines, 1, 1, 0)
	if !handled {
		t.Fatal("档位模型不可用")
	}
	found := false
	for _, b := range model.bands {
		if b.grade != 1 {
			continue
		}
		found = true
		if math.Abs(b.lo-50) > 1e-9 {
			t.Fatalf("min_g 越界时未达标档下界应 = 兜底 15%%钳 50 g，实得 %.2f", b.lo)
		}
	}
	if !found {
		t.Fatal("未达标档不应因 min_g 越界而消失")
	}
}

// min_g 低于比例兜底时，下界仍取 min_g。
func TestGradeRatioBelowBandUsesMinGBelowFloor(t *testing.T) {
	srv, db := newDetachTestServer(t)
	enableGradeRatio(t, srv, db)
	disableSpawnFloor(srv)

	lines := cachedGradeLines{
		val: ptrI64(1000), trophy: ptrI64(5000), blue: ptrI64(8000), minG: ptrI64(30),
	}
	model, handled := srv.gradeRatioModel(lines, 1, 1, 0)
	if !handled {
		t.Fatal("档位模型不可用")
	}
	found := false
	for _, b := range model.bands {
		if b.grade == 1 {
			found = true
			if math.Abs(b.lo-30) > 1e-9 {
				t.Fatalf("下界应取 min_g=30（低于兜底 50），实得 %.2f", b.lo)
			}
		}
	}
	if !found {
		t.Fatal("未达标档不应消失")
	}
}

// sampleWeightDB 必须通过 species_weight 读取到 min_g，而不只是接受注入的缓存值。
func TestSampleWeightDBLoadsSpeciesMinG(t *testing.T) {
	srv, db := newDetachTestServer(t)
	enableGradeRatio(t, srv, db)
	const key = "floor_db_probe"
	const minG = int64(120)
	if _, err := db.SQL.Exec(`INSERT INTO species_weight(fish_key,min_g,max_g,trophy_g,trophy_line_g,super_trophy_line_g) VALUES(?,?,?,?,?,?)`,
		key, minG, 10000, 1000, 5000, 8000); err != nil {
		t.Fatal(err)
	}

	c := rollContext(nil)
	below := 0
	for i := 0; i < 4000; i++ {
		w := srv.sampleWeightDB(context.Background(), key, 1, 1, 0, c)
		if w <= 0 {
			t.Fatalf("第 %d 次抽样没有结果", i)
		}
		if w >= 1000 {
			continue
		}
		below++
		if w < minG {
			t.Fatalf("未达标个体 %d g 低于数据库 min_g=%d g", w, minG)
		}
	}
	if below == 0 {
		t.Fatal("样本里没有未达标个体，用例无效")
	}
}

// min_g 缺失的鱼种由 gradeRatioModel 兜底：下界 = 达标线×15%，钳到 [1,50] g。
func TestSampleWeightDBFloorsBelowBandWhenMinGMissing(t *testing.T) {
	srv, db := newDetachTestServer(t)
	enableGradeRatio(t, srv, db)
	// 不插入 species_weight 行，模拟 min_g 缺失；直接注入达标线以控制样本。
	c := rollContext(nil) // rollContext 会补上 roll cache
	cache := c.cache
	const valLine = int64(1000)
	cache.mu.Lock()
	cache.settleStatics["floor_probe"] = [6]int64{valLine, 5000, 8000, 10000, 8800, 9600}
	cache.mu.Unlock()

	below := 0
	minSeen := int64(math.MaxInt64)
	for i := 0; i < 4000; i++ {
		w := srv.sampleWeightDB(context.Background(), "floor_probe", 1, 1, 0, c)
		if w <= 0 {
			t.Fatalf("第 %d 次抽样没有结果", i)
		}
		if w >= valLine {
			continue
		}
		below++
		if w < minSeen {
			minSeen = w
		}
	}
	if below == 0 {
		t.Fatal("样本里没有未达标个体，用例无效")
	}
	const floor = int64(50) // 1000 × 15% = 150，钳到上限 50 g（独立常量，不用被测函数自算）
	if minSeen < floor {
		t.Fatalf("min_g 缺失时未达标个体最小应 ≥ 兜底下界 %d g，实测 %d g", floor, minSeen)
	}
}

func TestBelowGradeFloorFromVal(t *testing.T) {
	cases := []struct {
		val, want int64
	}{
		{0, 0}, {-5, 0}, // 无达标线：不兜底
		{1000, 50}, // 15% = 150，但钳到上限 50 g
		{100, 15},
		{10, 2},    // 10 × 15% = 1.5，四舍五入为 2
		{7, 1},     // 1.05 → 1
		{5000, 50}, // 上限 50 g
		{100000, 50},
	}
	for _, c := range cases {
		if got := belowGradeFloorFromVal(c.val); got != c.want {
			t.Fatalf("belowGradeFloorFromVal(%d) = %d, want %d", c.val, got, c.want)
		}
	}
}
