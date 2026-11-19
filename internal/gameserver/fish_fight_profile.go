package gameserver

import (
	"math"
	"strings"
)

// fishFightProfile 是一次搏鱼中鱼种与体重共同决定的动态参数。
// 拉力与曲线缩放已改官方口径（f8 逐鱼种 K 表 + 曲线纯 uniform，见
// specs/changes/fish-pull-k-table/spec.md），profile 只剩体力池一路；
// StaminaScale 作用于 27.f5 的体力池，逐鱼种表属 P2。
type fishFightProfile struct {
	StaminaScale float64
	SizeRatio    float64
}

type fishFightArchetype struct {
	Stamina   float64
	CurvePool string
	// Fatigue 是本地「鱼搏斗自耗」的行为型档：鱼越活跃/越猛，自己挣扎越耗体力。
	// 它乘在看官方 f6 上（见 applyFishFatigueScale），1.00 = 官方口径。
	// 判据是服务端既有的行为型分类，不涉及客户端逐事件改动。
	Fatigue float64
}

var fishFightArchetypes = map[string]fishFightArchetype{
	"default":  {Stamina: 1.00, CurvePool: "crucian", Fatigue: 1.00},
	"ambush":   {Stamina: 0.95, CurvePool: "perch", Fatigue: 1.00},
	"salmonid": {Stamina: 1.28, CurvePool: "e.chub", Fatigue: 1.45},
	"cyprinid": {Stamina: 1.38, CurvePool: "c.carp", Fatigue: 1.25},
	"catfish":  {Stamina: 1.65, CurvePool: "e.chub", Fatigue: 1.15},
	"sturgeon": {Stamina: 1.75, CurvePool: "e.chub", Fatigue: 1.25},
	"small":    {Stamina: 0.72, CurvePool: "c.roach", Fatigue: 0.90},
	"marine":   {Stamina: 1.18, CurvePool: "perch", Fatigue: 1.25},
	"eel":      {Stamina: 0.92, CurvePool: "a.sleeper", Fatigue: 0.90},
	"apex":     {Stamina: 1.55, CurvePool: "e.chub", Fatigue: 1.45},
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
	sp := strings.ToLower(strings.TrimSpace(species))
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
		"ruffe", "piksha", "sander"):
		return fishFightArchetypes["ambush"]
	case has("eel", "lamprey", "conger"):
		return fishFightArchetypes["eel"]
	case has("carp", "bream", "tench", "barbel", "buffalo", "karasekarp"):
		return fishFightArchetypes["cyprinid"]
	case has("roach", "rudd", "dace", "bleak", "gudgeon", "minnow",
		"vendace", "sabrefish", "shemaya", "zubatka", "vobla", "stickleback"):
		return fishFightArchetypes["small"]
	case has("cod", "herring", "mackerel", "makrel", "tuna", "thynnus",
		"halibut", "kambala", "coalfish", "pollachius", "merluza", "molva",
		"hake", "saithe", "turbot", "flounder", "scorpius", "zoarces"):
		return fishFightArchetypes["marine"]
	default:
		return fishFightArchetypes["default"]
	}
}

func (s *Server) fishFightProfileFor(f *RolledFish) fishFightProfile {
	arch := fishFightArchetypeFor("")
	if f != nil {
		arch = fishFightArchetypeFor(f.Species)
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
		StaminaScale: arch.Stamina * (0.66 + 0.58*root),
		SizeRatio:    ratio,
	}
}
