package gameserver

import "math"

// defaultFishTimeCapSeconds 是派鱼"半小时"截断口径的默认目标秒数。
// 用户口径：可钓的最大鱼，在适合的装备下半小时内能拉起来。
const defaultFishTimeCapSeconds = 1800.0

// halfHourCapGrams 反解「目标秒数内能拉起」的最大鱼重（g）。
//
// 搏斗时长按服务端下发的参数推导（客户端按同一套数跑模拟）：
//
//	f6(0.5628) × stamina(w) / min(泄力kg, 鱼拉力kg)
//
// 鱼拉力一旦大于泄力就进入打滑段，张力恒为泄力，所以式中取泄力：
//
//	stamina(w) = rate × w × (w/knee)^(exp-1)
//	=> 0.5628 × rate × w^exp × knee^(1-exp) = seconds × drag
//
// 拐点以下体力池线性，改用 stamina = rate × w 求解。
func halfHourCapGrams(dragKg, ratePerGram, seconds float64) int64 {
	return halfHourCapGramsWith(dragKg, ratePerGram, seconds,
		defaultFishStaminaKneeG, defaultFishStaminaExponent)
}

func halfHourCapGramsWith(dragKg, ratePerGram, seconds, kneeG, exponent float64) int64 {
	if dragKg <= 0 || ratePerGram <= 0 || seconds <= 0 || exponent <= 0 {
		return 0
	}
	k := ratePerGram
	if kneeG > 0 {
		k *= math.Pow(kneeG, 1-exponent)
	}
	powered := seconds * dragKg / (verifiedInitialStaminaRatio * k)
	if powered <= 0 || math.IsInf(powered, 0) || math.IsNaN(powered) {
		return 0
	}
	w := math.Pow(powered, 1.0/exponent)
	if w <= 0 || math.IsInf(w, 0) || math.IsNaN(w) {
		return 0
	}
	if kneeG > 0 && w < kneeG {
		w = seconds * dragKg / (verifiedInitialStaminaRatio * ratePerGram)
	}
	if w <= 0 || math.IsInf(w, 0) || math.IsNaN(w) || w > 1e12 {
		return 0
	}
	return int64(math.Round(w))
}

// staminaRatePerGramFor 取鱼种每克体力系数（与 fishStaminaForProfile 同源）：
// 官方实测种走实测表，其余走 2.388 × 原型耐力倍率（grade 4 保守取最大耐力）。
func (s *Server) staminaRatePerGramFor(species string) float64 {
	if r, ok := fishStaminaRatePerGram[species]; ok && r > 0 {
		return r
	}
	prof := s.fishFightProfileFor(&RolledFish{Species: species, Grade: 4})
	if prof.StaminaScale <= 0 {
		return verifiedStaminaPerGram
	}
	return verifiedStaminaPerGram * prof.StaminaScale
}

// timeCapGFor 按当前钓组泄力与该鱼种体力系数算「半小时上限」（g）。0=不截断。
// 无轮（手竿/浮漂）时以竿承力 × 过载系数作为张力上限。
func (s *Server) timeCapGFor(species string, spec gearPhysicsSpec) int64 {
	if s == nil || s.tunableF("fish_time_cap", 1) <= 0 {
		return 0
	}
	seconds := s.tunableF("fish_time_cap_seconds", defaultFishTimeCapSeconds)
	if seconds <= 0 {
		return 0
	}
	drag := spec.ReelDragKg
	if drag <= 0 {
		drag = spec.RodPowerKg * gearOverloadFactor
	}
	if drag <= 0 || species == "" {
		return 0
	}
	rate := s.staminaRatePerGramFor(species)
	if rate <= 0 {
		return 0
	}
	return halfHourCapGramsWith(drag, rate, seconds,
		s.tunableF("fish_stamina_knee_g", defaultFishStaminaKneeG),
		s.tunableF("fish_stamina_exponent", defaultFishStaminaExponent))
}

// rollCapGFor 是派鱼实际生效的重量上限（g，0=不截断）：承力口径（gearCapG，
// 由 fish_weight_cap/hook_entry_cap 两个开关算好）与半小时口径取更严的一侧。
//
// hunt 分支（含双轨保底）必须和普通分支调用同一个函数，否则「半小时上限」
// 只在没开狩猎状态时生效：2026-11-16 手杆套 L0（9.10 kg）就是被 hunt 分支
// 塞进一条 113.297 kg 欧洲六须鲇，客户端判超载直接断竿（§3.33）。
func (s *Server) rollCapGFor(species string, spec gearPhysicsSpec, gearCapG float64) float64 {
	capG := gearCapG
	// 半小时口径：按当前泄力在目标秒数内能拉起的重量重抽。
	// 只压重量、不砍鱼种；与承力口径取更严的一侧。
	if tc := s.timeCapGFor(species, spec); tc > 0 && (capG <= 0 || float64(tc) < capG) {
		capG = float64(tc)
	}
	return capG
}
