package gameserver

import "math"

// 饵效连续加权的默认参数。全部可被 tunables 覆盖，且允许热调。
const (
	defaultBaitEvidenceBeta  = 0.85
	defaultBaitEvidenceFloor = 0.4
	defaultBaitEvidenceCeil  = 20.0
)

// baitContinuousMult 把「该饵对该鱼种的证据」转换成权重倍率。
//
// ev 是该饵在该鱼种的渔获计数，avg 是该鱼种全部饵的平均证据
// （companion_fish_meta.total_evidence / n_baits）。
//
// 返回 0 表示「没有信号」，调用方必须回退到旧的分档逻辑，绝不能把 0 乘进权重。
// 返回值一旦为正，就保证落在 [floor, ceil] 内，因此永远不会把鱼种权重清零。
func baitContinuousMult(ev int64, avg float64, beta, floor, ceil float64) float64 {
	if ev <= 0 || avg <= 0 {
		return 0
	}
	if math.IsNaN(beta) || math.IsInf(beta, 0) || beta <= 0 {
		beta = defaultBaitEvidenceBeta
	}
	if math.IsNaN(floor) || math.IsInf(floor, 0) || floor <= 0 {
		floor = defaultBaitEvidenceFloor
	}
	if math.IsNaN(ceil) || math.IsInf(ceil, 0) || ceil < floor {
		ceil = defaultBaitEvidenceCeil
		if ceil < floor {
			ceil = floor
		}
	}
	r := float64(ev) / avg
	m := math.Pow(r, beta)
	if math.IsNaN(m) || math.IsInf(m, 0) {
		return floor
	}
	if m < floor {
		m = floor
	}
	if m > ceil {
		m = ceil
	}
	return m
}

// baitWeightMult 返回饵效对单个鱼种的权重倍率；第二返回值表示是否命中正加成
// （与原 baitHit 语义一致，只有正加成才算命中）。
//
// 优先级：
//  1. bait_evidence_enabled=1 且有该饵对该鱼种的 evidence 与该鱼种的平均证据 → 连续倍率；
//  2. 否则回退到 4 档 tier 乘子（companionTierMult）；
//  3. 再没有 tier → 沿用原覆盖度衰减 (0.9-0.6*cov)，保证结果恒为正。
//
// 任何分支都不会返回 0。
func (s *Server) baitWeightMult(key string, tiers, evMap map[string]int64, avg map[string]float64, meta map[string]int64) (float64, bool) {
	if s.tunableF("bait_evidence_enabled", 1) == 1 && evMap != nil {
		if ev := evMap[key]; ev > 0 {
			if a := avg[key]; a > 0 {
				m := baitContinuousMult(ev, a,
					s.tunableF("bait_evidence_beta", defaultBaitEvidenceBeta),
					s.tunableF("bait_evidence_floor", defaultBaitEvidenceFloor),
					s.tunableF("bait_evidence_ceil", defaultBaitEvidenceCeil))
				if m > 0 {
					return m, true
				}
			}
		}
	}
	if t := tiers[key]; t > 0 {
		return companionTierMult(t), true
	}
	cov := float64(meta[key]) / 200.0
	if cov > 1.0 {
		cov = 1.0
	}
	return 0.9 - 0.6*cov, false
}
