package gameserver

import (
	"math"
	"strings"
)

// fishFightProfile 是一次搏鱼中鱼种与体重共同决定的动态参数。
// 官方模式只用 StaminaScale/SizeRatio；legacy 模式额外使用 ForceScale/
// DurationScale/RestScale 恢复 3166958 之前的手感。
type fishFightProfile struct {
	ForceScale    float64
	DurationScale float64
	RestScale     float64
	StaminaScale  float64
	SizeRatio     float64
}

type fishFightArchetype struct {
	Burst     float64
	Duration  float64
	Rest      float64
	Stamina   float64
	RestPower float64
	CurvePool string
	// Fatigue 是本地「鱼搏斗自耗」的行为型档：鱼越活跃/越猛，自己挣扎越耗体力。
	// 它乘在看官方 f6 上（见 applyFishFatigueScale），1.00 = 官方口径。
	// 判据是服务端既有的行为型分类，不涉及客户端逐事件改动。
	Fatigue float64
}

var fishFightArchetypes = map[string]fishFightArchetype{
	"default":  {Burst: 1.10, Duration: 0.95, Rest: 0.85, Stamina: 1.00, RestPower: 0.55, CurvePool: "crucian", Fatigue: 1.00},
	"ambush":   {Burst: 1.38, Duration: 0.72, Rest: 0.68, Stamina: 0.95, RestPower: 0.48, CurvePool: "perch", Fatigue: 1.00},
	"salmonid": {Burst: 1.30, Duration: 1.18, Rest: 0.92, Stamina: 1.28, RestPower: 0.52, CurvePool: "e.chub", Fatigue: 1.45},
	"cyprinid": {Burst: 1.06, Duration: 1.22, Rest: 1.10, Stamina: 1.38, RestPower: 0.58, CurvePool: "c.carp", Fatigue: 1.25},
	"catfish":  {Burst: 1.48, Duration: 1.20, Rest: 1.18, Stamina: 1.65, RestPower: 0.60, CurvePool: "e.chub", Fatigue: 1.15},
	"sturgeon": {Burst: 1.34, Duration: 1.42, Rest: 1.22, Stamina: 1.75, RestPower: 0.62, CurvePool: "e.chub", Fatigue: 1.25},
	"small":    {Burst: 0.96, Duration: 0.62, Rest: 0.58, Stamina: 0.72, RestPower: 0.45, CurvePool: "c.roach", Fatigue: 0.90},
	// marine 借官方 merlang 真池（39a1085 官服 14/14 抓包直配入库）：主搏 105
	// 6.5~20s、持续逃窜段 5~23s。此前借 perch 池是淡水小杂鱼剧本（主搏仅
	// 4.5~8s、逃窜权重 1~2%、98% 时间 0.3~0.7s 碎潜水），实测 84.2kg meagre
	// 全程 19s 被匀速拖回、14/8 遥测零负步（2026-11-16 搏鱼秒起调查）。张力
	// 列按 pull/池基准 uniform 缩放，时长/速度/权重保持池原值，故大鱼直接
	// 继承官方海鱼的搏斗节奏。
	"marine": {Burst: 1.22, Duration: 1.02, Rest: 0.90, Stamina: 1.18, RestPower: 0.52, CurvePool: "merlang", Fatigue: 1.25},
	"eel":    {Burst: 1.16, Duration: 0.78, Rest: 0.68, Stamina: 0.92, RestPower: 0.46, CurvePool: "a.sleeper", Fatigue: 0.90},
	"apex":   {Burst: 1.58, Duration: 1.05, Rest: 1.05, Stamina: 1.55, RestPower: 0.56, CurvePool: "e.chub", Fatigue: 1.45},
}

// fishFatigueArchetypeScale 返回该鱼种行为型对应的自耗档（1.00 = 官方口径）。
func fishFatigueArchetypeScale(species string) float64 {
	arch := fishFightArchetypeFor(species)
	if arch.Fatigue <= 0 {
		return 1.0
	}
	return arch.Fatigue
}

func fishFightArchetypeFor(species string) fishFightArchetype {
	// 皮肤变种继承宿主习性（PR #516 复审 P2-2）：与 f8 的 fishPullParamFor
	// 同源，一层别名无环。宿主均为实测种，词表对实测种的判定即宿主口径。
	sp := fishPullAliasHost(species)
	has := func(parts ...string) bool {
		for _, part := range parts {
			if strings.Contains(sp, part) {
				return true
			}
		}
		return false
	}
	switch {
	case has("shark", "katran", "lamna", "cetorhinus", "somniosus"):
		return fishFightArchetypes["apex"]
	case has("catfish", "burbot", "bullhead", "wels", "som"):
		return fishFightArchetypes["catfish"]
	case has("sturgeon", "beluga", "kaluga", "sterlet", "osetr"):
		return fishFightArchetypes["sturgeon"]
	case has("salmon", "trout", "char", "taimen", "lenok", "nelma", "omul",
		"muksun", "peled", "ripus", "vendace", "smelt", "keta", "nerka", "golets", "grayling"):
		return fishFightArchetypes["salmonid"]
	case has("pike", "zander", "walleye", "perch", "barsch", "orfe", "asp",
		"ruffe", "sander"):
		return fishFightArchetypes["ambush"]
	case has("eel", "lamprey", "conger"):
		return fishFightArchetypes["eel"]
	case has("carp", "bream", "tench", "barbel", "buffalo", "karasekarp"):
		return fishFightArchetypes["cyprinid"]
	case has("roach", "rudd", "dace", "bleak", "gudgeon", "minnow",
		"vendace", "sabrefish", "shemaya", "zubatka", "vobla", "stickleback",
		"tiulka"):
		return fishFightArchetypes["small"]
	case has("cod", "herring", "mackerel", "makrel", "tuna", "thynnus",
		"halibut", "kambala", "coalfish", "pollachius", "merluza", "molva",
		"hake", "saithe", "turbot", "flounder", "scorpius", "zoarces",
		// 词表修正（搏鱼脱钩方案 §五C）：piksha 实测为海水鲽科，此前被显式
		// 列在 ambush 关键词表里（淡水伏击组；不是子串匹配——has 是
		// strings.Contains，"piksha" 并不含 "pike"）；menek/merlang 为海图鱼种。
		// piksha 的组归属移动对行为中性：它有官方直配池（buildFightCurves
		// 鱼种池优先），f5/f6 已有显式表。
		// agar/warm 曾为「行为兼容保留」（chart-roster-marine-group spec），
		// PR #516 复审实证其为海水组误伤（只在淡水图，warm 的 f5 曾吃
		// 8.445/g 海水耐力），已移除：现分别由 f8 组表直查归 predator/
		// sunfish（见 fishPullGroupFightKey）。
		"merlang", "menek", "piksha"):
		return fishFightArchetypes["marine"]
	default:
		return fishFightArchetypes["default"]
	}
}

// fightPhaseTensionScale / fightPhaseDurationScale 是 3166958 之前的分阶段
// 节奏表；legacy 模式或 fight_phase_scales A/B 开启时叠加到官方曲线。
func fightPhaseTensionScale(stage uint32) float64 {
	switch stage {
	case 300:
		return 1.36
	case 301:
		return 1.20
	case 302:
		return 1.78
	case 306:
		return 0.42
	case 101:
		return 1.66
	case 102:
		return 1.42
	case 103:
		return 1.16
	case 104:
		return 0.62
	case 105:
		return 0.30
	default:
		return 0.92
	}
}

func fightPhaseDurationScale(stage uint32) float64 {
	switch stage {
	case 302:
		return 0.82
	case 101:
		return 0.72
	case 102:
		return 0.78
	case 103:
		return 1.18
	case 104:
		return 1.35
	case 105, 306:
		return 1.55
	default:
		return 1.0
	}
}

func fightIsRestPhase(stage uint32) bool {
	return stage == 104 || stage == 105 || stage == 306
}

func (s *Server) fishFightProfileFor(f *RolledFish) fishFightProfile {
	arch := fishFightArchetypeFor("")
	if f != nil {
		arch = fishCurveArchetypeFor(f.Species, s.fishMarineGroupHit(f.Species))
	}
	ratio := 0.5
	if f != nil {
		switch f.Grade {
		case 1:
			ratio = 0.25
		case 2:
			ratio = 0.50
		case 3:
			ratio = 0.75
		case 4:
			ratio = 1.0
		}
	}
	if f != nil && f.WeightG > 0 && s != nil && s.DB != nil {
		var maxG int64
		if err := s.DB.SQL.QueryRowContext(s.dbCtx(),
			`SELECT COALESCE(max_g,0) FROM species_weight WHERE fish_key=?`,
			f.Species).Scan(&maxG); err == nil && maxG > 0 {
			ratio = float64(f.WeightG) / float64(maxG)
		}
	}
	ratio = math.Max(0.03, math.Min(1.0, ratio))
	root := math.Sqrt(ratio)
	return fishFightProfile{
		ForceScale:    arch.Burst * (0.84 + 0.28*root),
		DurationScale: arch.Duration * (0.72 + 0.58*ratio),
		RestScale:     arch.Rest * (0.82 + 0.30*root),
		StaminaScale:  arch.Stamina * (0.66 + 0.58*root),
		SizeRatio:     ratio,
	}
}
