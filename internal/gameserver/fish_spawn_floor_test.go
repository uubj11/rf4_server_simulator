package gameserver

import (
	"math"
	"testing"
)

func int64p(v int64) *int64 { return &v }

// 发鱼下限：鱼再小也不能低于「达标线 × spawn_min_ratio」。
// 旧口径未达标档从 1g 起，虹鳟达标线 147g 时会发出 22g/10cm 的苗子。
func TestSpawnMinRatioLiftsBelowBand(t *testing.T) {
	srv, _ := newProgressSrv(t)
	lines := cachedGradeLines{val: int64p(147), trophy: int64p(10000), blue: int64p(13000)}

	cases := []struct {
		ratio     float64
		wantLo    float64
		wantBands int
		note      string
	}{
		{0.6, 88, 4, "默认 60%：未达标档从 88g 起（约 16cm）"},
		{0, 1, 4, "0=关闭下限，回到旧的 1g 口径"},
		{1, 0, 3, "100%：未达标档直接消失，最差的鱼也达标"},
	}
	for _, c := range cases {
		srv.tunCache = map[string]float64{tunableSpawnMinRatio: c.ratio}
		model, ok := srv.gradeRatioModel(lines, 1, 1, 0)
		if !ok {
			t.Fatalf("ratio=%v 建模失败", c.ratio)
		}
		if len(model.bands) != c.wantBands {
			t.Fatalf("ratio=%v 档位数 %d，期望 %d（%s）", c.ratio, len(model.bands), c.wantBands, c.note)
		}
		if c.wantLo == 0 {
			if model.bands[0].grade != 2 {
				t.Fatalf("ratio=%v 首档应为合格档，实际 grade=%d", c.ratio, model.bands[0].grade)
			}
			continue
		}
		if model.bands[0].grade != 1 {
			t.Fatalf("ratio=%v 首档应为未达标档，实际 grade=%d", c.ratio, model.bands[0].grade)
		}
		if math.Abs(model.bands[0].lo-c.wantLo) > 1 {
			t.Fatalf("ratio=%v 未达标档下界 %.0f，期望 %.0f（%s）", c.ratio, model.bands[0].lo, c.wantLo, c.note)
		}
	}
}

// 鱼猎链路（sampleHuntBand）也必须吃同一条下限，否则两套模式会不一致。
func TestHuntBandRespectsSpawnFloor(t *testing.T) {
	srv, db := newProgressSrv(t)
	ctx := t.Context()
	if _, err := db.SQL.ExecContext(ctx, `INSERT OR REPLACE INTO species_weight
		(fish_key,trophy_g,trophy_line_g,super_trophy_line_g,length_k,length_p)
		VALUES('floor_fish',NULL,10000,13000,0.0355,0.3333)`); err != nil {
		t.Fatal(err)
	}
	srv.tunCache = map[string]float64{tunableSpawnMinRatio: 0.6}
	// 达标线缺失时 settleStatics 按奖杯线 0.173 估算 → 1730g；下限 60% = 1038g
	floor := srv.spawnFloorG(float64(srv.cachedSettleStatics(ctx, fishRollContext{}, "floor_fish")[0]))
	if floor < 1000 || floor > 1080 {
		t.Fatalf("下限 %.0f g，期望约 1038 g", floor)
	}
	minSeen := int64(math.MaxInt64)
	for range 400 {
		w := srv.sampleHuntBand(ctx, "floor_fish", 1, 1, 1, 0, fishRollContext{})
		if w <= 0 {
			continue
		}
		if w < minSeen {
			minSeen = w
		}
	}
	if minSeen < int64(floor) {
		t.Fatalf("鱼猎链路出现 %dg，低于下限 %.0fg", minSeen, floor)
	}
}


