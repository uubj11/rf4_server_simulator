package gameserver

import (
	"math"
	"testing"
	"time"
)

// 饵效倍率不得改变四档比例；面板显示的占比就是实际发鱼的占比。
func TestGradeRatioSharesIgnoreBaitModeMultiplier(t *testing.T) {
	srv, db := newRigTestServer(t)
	enableGradeRatio(t, srv, db)
	setGradeRatioTunables(t, srv, db, map[string]string{
		"grade_pct_below":     "5.06",
		"grade_pct_qualified": "93.65",
		"grade_pct_trophy":    "1.19",
		"grade_pct_super":     "0.1",
	})
	lines := cachedGradeLines{val: ptrI64(1000), trophy: ptrI64(5000), blue: ptrI64(8000)}
	for _, mode := range []float64{1, 1.12, 1.36, 2} {
		weights, handled := srv.gradeRatioGradeWeights(lines, mode, 1, 0)
		if !handled {
			t.Fatalf("mode=%g 应有可用档位", mode)
		}
		total := weights[0] + weights[1] + weights[2] + weights[3]
		if math.Abs(weights[0]/total-.0506) > 1e-9 ||
			math.Abs(weights[1]/total-.9365) > 1e-9 ||
			math.Abs(weights[2]/total-.0119) > 1e-9 ||
			math.Abs(weights[3]/total-.001) > 1e-9 {
			t.Fatalf("mode=%g 改动了四档比例: %v", mode, weights)
		}
	}
}

func TestGradeRatioDisablesTrophyLuckResample(t *testing.T) {
	srv := &Server{
		tunCache: map[string]float64{"grade_ratio_enabled": 1},
		tunAt:    time.Now(),
	}
	got := srv.applyRollTrophyLuck(300, 2, func() float64 { return 500 })
	if got != 300 {
		t.Fatalf("档位比例开启时 TrophyLuck 不得重采样，实得 %g", got)
	}

	srv.tunCache["grade_ratio_enabled"] = 0
	got = srv.applyRollTrophyLuck(300, 2, func() float64 { return 500 })
	if got != 500 {
		t.Fatalf("档位比例关闭后应恢复 TrophyLuck 重采样，实得 %g", got)
	}
}

func TestGradeRatioRetainedMassIgnoresTrophyLuck(t *testing.T) {
	law := fishWeightLaw{roundedSurvival: func(int64) float64 { return .4 }}
	if got := law.retainedMass(1000, 2); got != .4 {
		t.Fatalf("档位比例查询预估不得按 TrophyLuck 放大，实得 %g", got)
	}
}
