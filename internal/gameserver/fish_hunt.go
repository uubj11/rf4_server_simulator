package gameserver

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"math/rand"
	"time"
)

const (
	fishHuntDaily = iota
	fishHuntGather
	fishHuntActive
	fishHuntGiant
)

const (
	fishHuntModeFreshwater = iota
	fishHuntModeMarine
	fishHuntModeLure
)

const (
	fishHuntCompWindowSeconds = 10 * 60
	fishHuntSpotResetM        = 20.0
)

const (
	tunableHookNibbleEnabled      = "hook_nibble_enabled"
	tunableHookSpawnMatchStrength = "hook_spawn_match_strength"
	tunableTwoStageHuntEnabled    = "two_stage_hunt_enabled"
)

func (s *Server) hookNibbleEnabled() bool {
	return s != nil && s.tunableF(tunableHookNibbleEnabled, 0) != 0
}

func (s *Server) gradeRatioEnabled() bool {
	return s != nil && s.tunableF("grade_ratio_enabled", 1) == 1
}

// twoStageHuntEnabled 是第三种发鱼模式：淡水底钓/浮钓只有日常与打窝两档。
// 与档位比例、鱼猎四阶段三选一，由后台 spawn_mode 原子切换。
func (s *Server) twoStageHuntEnabled() bool {
	return s != nil && s.tunableF(tunableTwoStageHuntEnabled, 0) == 1
}

// huntStateEnabled 是鱼猎状态机（进度/保底/锚点）的总门。两档制模式把
// fish_hunt_enabled 关掉，但 pity、窝料进度和锚点重置必须继续记账，
// 否则切回四阶段时进度凭空丢失；该门只扩权不缩权。
func (s *Server) huntStateEnabled() bool {
	return s.fishHuntEnabled() || s.twoStageHuntEnabled()
}

// effectiveHookSpawnMatchStrength 返回真正参与发鱼的钩型软匹配强度。
// 档位比例开启后由配置比例独占档位权重，钩型不得再改写派鱼结果。
func (s *Server) effectiveHookSpawnMatchStrength() float64 {
	if s == nil || s.gradeRatioEnabled() {
		return 0
	}
	return s.tunableF(tunableHookSpawnMatchStrength, 0)
}

// gradeRatioGradeWeights 把可用档位及其配置权重展开为四档数组，供海图声呐等只读预览复用。
func (s *Server) gradeRatioGradeWeights(lines cachedGradeLines, modeMult, scale, capG float64) ([4]float64, bool) {
	model, handled := s.gradeRatioModel(lines, modeMult, scale, capG)
	if !handled {
		return [4]float64{}, false
	}
	var weights [4]float64
	for i, band := range model.bands {
		weights[band.grade-1] = model.weights[i]
	}
	return weights, true
}

type fishHuntState struct {
	UID           int64
	Loc           string
	Spot          uint64
	Mode          int64
	Zone          int64
	ZoneKnown     bool
	AnchorX       float64
	AnchorZ       float64
	AnchorKnown   bool
	ActiveSeconds float64
	ValidBites    int64
	Pity          int64
	BigMiss       int64
	// TrophyPity / SuperPity：本地服新增的双轨保底计数，与 Pity 独立。
	// TrophyPity 只推奖杯（3 星），SuperPity 只推超稀有（4 星），互不覆盖。
	TrophyPity    int64
	SuperPity     int64
	CompFish      int64
	CompGrade     int64
	CompUntil     float64
	LastCatchGUID string
	LastActiveTS  float64
	UpdatedTS     float64
}

type fishHuntReadiness struct {
	Stage int
	Mode  int
	A     float64
	E     float64
	G     float64
	P     float64
	Q     float64
	Comp  bool
}

func (s *Server) fishHuntEnabled() bool {
	return s != nil && s.DB != nil && s.tunableF("fish_hunt_enabled", 1) != 0
}

// pityKey 是双轨保底计数的聚合键：同一张地图内的不同湖区共用一份进度。
// 取玩家当前所在地图 key；取不到时退回湖区 key，保证计数不写丢。
func (s *Server) pityKey(uid int64, loc string) string {
	if key := s.currentLevel(uid); key != "" {
		return key
	}
	return loc
}

func (s *Server) loadFishHuntState(ctx context.Context, uid int64, loc string, spot uint64) *fishHuntState {
	h := &fishHuntState{UID: uid, Loc: loc, Spot: spot}
	if !s.huntStateEnabled() || uid == 0 || loc == "" {
		return h
	}
	var anchorKnown, zoneKnown, compGrade int64
	err := s.DB.SQL.QueryRowContext(ctx, `
		SELECT anchor_x,anchor_z,anchor_known,active_seconds,valid_bites,pity,
		       big_miss,trophy_pity,super_pity,comp_fish,comp_grade,comp_until,last_catch_guid,
		       last_active_ts,updated_ts,hunt_mode,sea_zone,sea_zone_known
		FROM fish_hunt_state WHERE uid=? AND loc=? AND spot=?`,
		uid, loc, int64(spot)).Scan(
		&h.AnchorX, &h.AnchorZ, &anchorKnown, &h.ActiveSeconds, &h.ValidBites, &h.Pity,
		&h.BigMiss, &h.TrophyPity, &h.SuperPity, &h.CompFish, &compGrade, &h.CompUntil, &h.LastCatchGUID,
		&h.LastActiveTS, &h.UpdatedTS, &h.Mode, &h.Zone, &zoneKnown)
	h.AnchorKnown = anchorKnown != 0
	h.ZoneKnown = zoneKnown != 0
	h.CompGrade = compGrade
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		s.log.Warn("鱼猎状态读取失败", "uid", uid, "loc", loc, "spot", spot, "err", err)
	}
	// 本地服双轨保底：计数按 (uid, 地图) 聚合，既不随抛竿落点(spot)重置，
	// 也不随同一张图内换湖区(loc)重置——否则一张图钓上百条，进度会被拆成几份，
	// 每份都走不满周期，玩家永远等不到保底。
	pityKey := s.pityKey(uid, loc)
	var aggTrophy, aggSuper int64
	if aggErr := s.DB.SQL.QueryRowContext(ctx, `
		SELECT trophy_pity, super_pity FROM fish_hunt_pity WHERE uid=? AND loc=?`,
		uid, pityKey).Scan(&aggTrophy, &aggSuper); aggErr == nil {
		h.TrophyPity, h.SuperPity = aggTrophy, aggSuper
	} else if !errors.Is(aggErr, sql.ErrNoRows) {
		s.log.Warn("保底聚合读取失败", "uid", uid, "loc", loc, "err", aggErr)
	}
	return h
}

func (s *Server) saveFishHuntState(ctx context.Context, h *fishHuntState) {
	if !s.huntStateEnabled() || h == nil || h.UID == 0 || h.Loc == "" {
		return
	}
	h.UpdatedTS = float64(time.Now().Unix())
	_, err := s.DB.SQL.ExecContext(ctx, `
		INSERT INTO fish_hunt_state(
			uid,loc,spot,anchor_x,anchor_z,anchor_known,active_seconds,valid_bites,
			pity,big_miss,trophy_pity,super_pity,comp_fish,comp_grade,comp_until,last_catch_guid,
			last_active_ts,updated_ts,hunt_mode,sea_zone,sea_zone_known)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(uid,loc,spot) DO UPDATE SET
			anchor_x=excluded.anchor_x,anchor_z=excluded.anchor_z,
			anchor_known=excluded.anchor_known,active_seconds=excluded.active_seconds,
			valid_bites=excluded.valid_bites,pity=excluded.pity,
			big_miss=excluded.big_miss,trophy_pity=excluded.trophy_pity,
			super_pity=excluded.super_pity,comp_fish=excluded.comp_fish,
			comp_grade=excluded.comp_grade,comp_until=excluded.comp_until,
			last_catch_guid=excluded.last_catch_guid,
			last_active_ts=excluded.last_active_ts,updated_ts=excluded.updated_ts,
			hunt_mode=excluded.hunt_mode,sea_zone=excluded.sea_zone,
			sea_zone_known=excluded.sea_zone_known`,
		h.UID, h.Loc, int64(h.Spot), h.AnchorX, h.AnchorZ, boolInt(h.AnchorKnown),
		h.ActiveSeconds, h.ValidBites, h.Pity, h.BigMiss, h.TrophyPity, h.SuperPity,
		h.CompFish, h.CompGrade, h.CompUntil, h.LastCatchGUID, h.LastActiveTS, h.UpdatedTS,
		h.Mode, h.Zone, boolInt(h.ZoneKnown))
	if err != nil {
		s.log.Warn("鱼猎状态保存失败", "uid", h.UID, "loc", h.Loc, "spot", h.Spot, "err", err)
	}
	// 双轨保底计数同时写进 (uid, 地图) 聚合表，保证换落点、换湖区都不清零。
	pityKey := s.pityKey(h.UID, h.Loc)
	if _, aggErr := s.DB.SQL.ExecContext(ctx, `
		INSERT INTO fish_hunt_pity(uid,loc,trophy_pity,super_pity,updated_ts)
		VALUES(?,?,?,?,?)
		ON CONFLICT(uid,loc) DO UPDATE SET
			trophy_pity=excluded.trophy_pity,
			super_pity=excluded.super_pity,
			updated_ts=excluded.updated_ts`,
		h.UID, pityKey, h.TrophyPity, h.SuperPity, h.UpdatedTS); aggErr != nil {
		s.log.Warn("保底聚合写入失败", "uid", h.UID, "loc", h.Loc, "key", pityKey, "err", aggErr)
	}
}

// huntAnchorGroundbait 只在超过 20 米的新窝点时重置狩猎进程。
// 同一窝点补料只刷新窝账，不清空发窝度。
func (s *Server) huntAnchorGroundbait(ctx context.Context, uid int64, loc string, spot uint64, x, z float64) {
	if !s.huntStateEnabled() {
		return
	}
	h := s.loadFishHuntState(ctx, uid, loc, spot)
	if h.AnchorKnown && math.Hypot(x-h.AnchorX, z-h.AnchorZ) <= fishHuntSpotResetM {
		return
	}
	h.AnchorX, h.AnchorZ, h.AnchorKnown = x, z, true
	h.ActiveSeconds, h.ValidBites, h.Pity = 0, 0, 0
	h.BigMiss, h.CompFish, h.CompGrade, h.CompUntil = 0, 0, 0, 0
	h.LastActiveTS = float64(time.Now().Unix())
	s.saveFishHuntState(ctx, h)
}

func (s *Server) huntTouch(h *fishHuntState) {
	if h == nil {
		return
	}
	now := float64(time.Now().Unix())
	if h.LastActiveTS > 0 {
		dt := now - h.LastActiveTS
		if dt > 0 && dt <= 60 {
			h.ActiveSeconds += dt
		}
	}
	h.LastActiveTS = now
}

func (s *Server) fishHuntBaitMatch(ctx context.Context, key string, c fishRollContext) float64 {
	best := int64(0)
	for _, bait := range c.baits {
		if bait == "" {
			continue
		}
		for fish, tier := range s.cachedBaitTiers(ctx, c, bait) {
			if fish == key && tier > best {
				best = tier
			}
		}
	}
	switch best {
	case 3:
		return 1
	case 2:
		return .75
	case 1:
		return .5
	default:
		return .35
	}
}

type marineFishTraits struct {
	schooling float64
	chase     float64
	ambush    float64
	pit       float64
}

// marineTraits 是海图 v1 的鱼群习性代理值。现有生态表没有单独的社会性字段，
// 先用活动度、攻击性、水层和稀有度推导，后续可由后台覆盖。
func (s *Server) marineTraits(ctx context.Context, key string, c fishRollContext) marineFishTraits {
	p := s.cachedFishEcology(ctx, c, key)
	activity := groundbaitClamp(p.Activity, .5, 1.5)
	aggression := groundbaitClamp(p.Aggression, .5, 1.5)
	layer := groundbaitClamp(p.LayerPref, 0, 1)
	return marineFishTraits{
		schooling: groundbaitClamp(.35+(activity-.7)*.45-(aggression-1)*.20+(1-layer)*.10, .05, .95),
		chase:     groundbaitClamp(.15+(aggression-.8)*.55+p.CurrentPref*.20, .05, .95),
		ambush:    groundbaitClamp(.15+layer*.60+(1.2-activity)*.20, .05, .95),
		pit:       groundbaitClamp(.15+layer*.65+float64(p.RarityTier)*.04, .05, .95),
	}
}

func marineActionMatch(c fishRollContext, t marineFishTraits) float64 {
	switch c.lureAction {
	case "trolling":
		return groundbaitClamp(.55+.45*(.60*t.schooling+.40*t.chase), .35, 1)
	case "marine_jig", "vertical_dive", "vertical_ascend", "bottom_layer_move", "bottom_move":
		return groundbaitClamp(.50+.50*(.60*t.ambush+.40*t.pit), .30, 1)
	case "drift":
		return groundbaitClamp(.50+.50*(.50*t.schooling+.50*t.chase), .30, 1)
	case "jig":
		return groundbaitClamp(.60+.40*(.55*t.ambush+.45*t.chase), .30, 1)
	default:
		return .55
	}
}

func marineDepthMatch(c fishRollContext, t marineFishTraits) float64 {
	layer := normalizeWaterLayer(c.lureLayer)
	if c.spotEnv.Known && c.spotEnv.DepthM > 0 {
		layer = groundbaitClamp(c.spotEnv.DepthM/220, 0, 1)
	}
	return groundbaitClamp(1-math.Abs(layer-.58*t.pit-.20*t.ambush), .25, 1)
}

// fishHuntReadiness 根据水域模式分派：淡水用窝料，海图用声呐区号和搜索动作。
func (s *Server) fishHuntReadiness(ctx context.Context, key string, c fishRollContext, h *fishHuntState) fishHuntReadiness {
	if marineHuntContext(c) {
		out := s.fishMarineHuntReadiness(ctx, key, c, h)
		out.Mode = fishHuntModeMarine
		return out
	}
	if rigTypeLureFamily[c.rigType] {
		out := s.fishLureHuntReadiness(ctx, key, c, h)
		out.Mode = fishHuntModeLure
		return out
	}
	out := s.fishFreshwaterHuntReadiness(ctx, key, c, h)
	out.Mode = fishHuntModeFreshwater
	return out
}

func marineHuntContext(c fishRollContext) bool {
	return marineRigType(c.rigType) || c.runtime != nil && c.runtime.chartKnown
}

func (s *Server) fishMarineHuntReadiness(ctx context.Context, key string, c fishRollContext, h *fishHuntState) fishHuntReadiness {
	out := fishHuntReadiness{Stage: fishHuntDaily}
	if h == nil || s.DB == nil {
		return out
	}
	s.huntTouch(h)
	zone := 0
	zoneKnown := c.runtime != nil && c.runtime.zoneKnown
	if zoneKnown {
		zone = c.runtime.zone
	}
	if h.Mode != fishHuntModeMarine {
		h.ActiveSeconds, h.ValidBites, h.Pity = 0, 0, 0
		h.BigMiss, h.CompFish, h.CompGrade, h.CompUntil = 0, 0, 0, 0
		h.Zone, h.ZoneKnown = 0, false
	}
	if zoneKnown {
		if h.ZoneKnown && h.Zone != int64(zone) {
			h.ActiveSeconds, h.ValidBites, h.Pity = 0, 0, 0
			h.BigMiss, h.CompFish, h.CompGrade, h.CompUntil = 0, 0, 0, 0
		}
		h.Zone, h.ZoneKnown = int64(zone), true
	}
	h.Mode = fishHuntModeMarine
	traits := s.marineTraits(ctx, key, c)
	action := marineActionMatch(c, traits)
	depth := marineDepthMatch(c, traits)
	zoneMatch := .15
	if zoneKnown {
		zoneMatch = 1
	}
	sonar := .15
	if zoneKnown {
		sonar = .75
		if c.spotEnv.Known {
			sonar += .10
		}
	}
	if c.runtime != nil && c.runtime.sonarSystemID != "" {
		if profile := s.sonarProfileFor(ctx, c.runtime.sonarSystemID); profile != nil {
			sonar = math.Max(sonar, .80)
			if profile.depthMax > profile.depthMin {
				sonar += .08
			}
			if profile.sizeP90 >= profile.sizeMed && profile.sizeMed > 0 {
				sonar += .05
			}
			sonar = math.Min(sonar, 1)
		}
	}
	out.P = .65*(1-math.Exp(-h.ActiveSeconds/720)) + .35*(1-math.Exp(-float64(h.ValidBites)/12))
	out.P = groundbaitClamp(out.P, 0, 1)
	out.A = groundbaitClamp(zoneMatch*(.35+.45*sonar+.20*out.P)*(.55+.45*action), 0, 1)
	env := s.fishBiteEnvironmentWeight(ctx, key, c)
	eco := groundbaitClamp((env-.35)/1.65, 0, 1)
	out.E = groundbaitClamp(.70*eco+.30*.35, 0, 1)
	water := fishWaterConditionsFor(s.cachedFishEcology(ctx, c, key), c)
	lure := groundbaitClamp((s.fishLureWeight(s.cachedFishEcology(ctx, c, key), c, water)-.45)/1.05, 0, 1)
	out.G = groundbaitClamp(.45*lure+.35*action+.20*depth, 0, 1)
	k := groundbaitClamp(.5+c.skill.EcologyInterest*2+c.skill.TrophyLuck*2, 0, 1)
	out.Q = groundbaitClamp(out.E*out.G*(.25+.75*out.P)*(.90+.10*k), 0, 1)
	out.Comp = h.CompFish > 0 && float64(time.Now().Unix()) <= h.CompUntil
	switch {
	case out.A < .25 || out.E < .25 || out.G < .25:
		out.Stage = fishHuntDaily
	case out.A >= .70 && out.Q >= .82:
		out.Stage = fishHuntGiant
	case out.A >= .55 && out.Q >= .58:
		out.Stage = fishHuntActive
	default:
		out.Stage = fishHuntGather
	}
	return out
}

// fishLureHuntReadiness 是路亚族独立的大鱼狩猎进度。路亚不靠窝料把鱼
// 聚过来，而是靠持续搜索、收线动作和拟饵与水层/鱼种的匹配建立机会。
// 因此这里不读取 groundbaitSpotAt；窝料只继续影响非路亚钓法。
func (s *Server) fishLureHuntReadiness(ctx context.Context, key string, c fishRollContext, h *fishHuntState) fishHuntReadiness {
	out := fishHuntReadiness{Stage: fishHuntDaily}
	if h == nil || s.DB == nil {
		return out
	}
	if h.Mode != fishHuntModeLure {
		h.ActiveSeconds, h.ValidBites, h.Pity = 0, 0, 0
		h.BigMiss, h.CompFish, h.CompGrade, h.CompUntil = 0, 0, 0, 0
	}
	h.Mode = fishHuntModeLure
	s.huntTouch(h)

	out.P = .65*(1-math.Exp(-h.ActiveSeconds/720)) + .35*(1-math.Exp(-float64(h.ValidBites)/12))
	out.P = groundbaitClamp(out.P, 0, 1)
	env := s.fishBiteEnvironmentWeight(ctx, key, c)
	eco := groundbaitClamp((env-.35)/1.65, 0, 1)
	out.E = groundbaitClamp(.70*eco+.30*.55, 0, 1)
	water := fishWaterConditionsFor(s.cachedFishEcology(ctx, c, key), c)
	lure := groundbaitClamp((s.fishLureWeight(s.cachedFishEcology(ctx, c, key), c, water)-.45)/1.05, 0, 1)
	action := lureHuntActionQuality(c)
	out.G = groundbaitClamp(.70*lure+.20*action+.10*fishMotionConditionWeight(
		s.cachedFishEcology(ctx, c, key), c, water), 0, 1)
	out.A = groundbaitClamp(.28+.55*out.P+.12*lure+.05*action, 0, 1)
	// 路亚的 Q 是“搜索质量”，不是窝料发窝质量：持续搜索、拟饵匹配和
	// 环境适配共同决定是否进入聚鱼/巨物阶段。
	out.Q = groundbaitClamp(.45*out.A+.25*out.E+.30*out.G, 0, 1)
	out.Comp = h.CompFish > 0 && float64(time.Now().Unix()) <= h.CompUntil

	switch {
	case out.A < .30 || out.G < .18:
		out.Stage = fishHuntDaily
	case out.A >= .68 && out.Q >= .58:
		out.Stage = fishHuntGiant
	case out.A >= .50 && out.Q >= .42:
		out.Stage = fishHuntActive
	default:
		out.Stage = fishHuntGather
	}
	return out
}

func lureHuntActionQuality(c fishRollContext) float64 {
	if c.lureAction == "" {
		return .45
	}
	if c.runtime != nil && c.runtime.lureKnown {
		weight := lureActionTypeWeight(c.runtime.lure.Type, c.lureAction)
		return groundbaitClamp((weight-.80)/.54, 0, 1)
	}
	return .55
}

// hookScore 把实际钩号与钩体长度合成为一个连续值；0=小钩，0.5=中钩，1=大钩。
// 不把钩号直接当阈值，避免同一尺寸族里出现突然断层。
func hookScore(spec gearPhysicsSpec) float64 {
	if !spec.HookPresent {
		return .5
	}
	size := groundbaitClamp(float64(spec.HookSize)/33.0, 0, 1)
	length := groundbaitClamp((spec.HookLengthMM-1.8)/(76.0-1.8), 0, 1)
	return groundbaitClamp(.65*size+.35*length, 0, 1)
}

// hookGapeG 是钩型的参考入口容量。这里只用于软匹配，不再作为派鱼硬上限。
func hookGapeG(spec gearPhysicsSpec) float64 {
	if !spec.HookPresent {
		return 0
	}
	return hookWeightCapG(spec.HookLengthMM, spec.HookSize) * gearOverloadFactor
}

// hookBiteDelayFactor 让大钩上口慢一点、小钩入口快一点。
func hookBiteDelayFactor(spec gearPhysicsSpec) float64 {
	if !spec.HookPresent {
		return 1
	}
	return .90 + .30*hookScore(spec)
}

func (s *Server) hookBiteDelayFactorForRig(ctx context.Context, uid int64, rod string) float64 {
	if s == nil || s.DB == nil || uid == 0 || rod == "" {
		return 1
	}
	ids := s.rigGearIDs(ctx, uid, rod)
	return hookBiteDelayFactor(s.loadGearSpecForRig(ctx, ids, rod))
}

// hookSolidHold 返回普通鱼的稳定入口概率与中钩后保持率（普通钓具口径，tol=0）。
// 蓝冠不走这里，蓝冠由 luckProtected 直接保送。
func hookSolidHold(weightG int64, spec gearPhysicsSpec) (solid, hold float64) {
	return hookSolidHoldTol(weightG, spec, 0)
}

// hookToleranceBase 是成长套装在 0 级时的起步宽容度：套装天生比普通钓具宽容一档，
// 满级再按进度推到 1.0。普通钓具不走这里，仍是 0 = 原版手感。
const hookToleranceBase = 0.30

// growthToleranceLevel 把等级进度映射成实际宽容度，0 级也有 hookToleranceBase。
func growthToleranceLevel(progress float64) float64 {
	progress = groundbaitClamp(progress, 0, 1)
	return hookToleranceBase + (1-hookToleranceBase)*progress
}

// growthToleranceProgress 把套装等级换算成 0~1 的钩宽容进度。
// 0 表示普通钓具手感（与原版逐位一致），1 表示满级成长套装的最宽容区间。
func growthToleranceProgress(level, cap int64) float64 {
	if cap <= 0 || level <= 0 {
		return 0
	}
	if level > cap {
		level = cap
	}
	return float64(level) / float64(cap)
}

// hookSpawnFloorR 是发鱼阶段的相对尺寸下限：r = 鱼重 / 钩口容量。
// 宽容带只应放宽「够得着的中段」，相对尺寸低于本值的个体不再享受抬升，
// 否则大钩会发出一堆小到不达标的鱼。
const hookSpawnFloorR = 0.08

// hookSpawnFloorMaxG 是发鱼下限的绝对封顶（30kg）。超大钩口（海钓 2700kg）按纯比例
// 会得到 216kg 的下限，等于把中小型海鱼全判死，所以下限最多按 30kg 算。
const hookSpawnFloorMaxG = 30000

// hookToleranceBand 是宽容带展开后的三组分段阈值。
// tol=0 时全部退化为原版常量（.25 / .80 / .45 / .15 / 1.00 / .30 / .50 / .55 / .35 / .80 / .10），
// 所以普通钓具的手感与改动前逐位一致；tol=1 为满级成长套装的最宽容区间。
type hookToleranceBand struct {
	lo         float64 // 大钩小鱼阈值
	solidKink  float64 // 正口率平台右端
	solidSlope float64 // 正口率超重段斜率
	solidFloor float64
	holdKink  float64 // 保持率平台右端
	holdSlope float64
	holdFloor float64
	solidBase float64 // 低段基座：r→0 时的正口率系数
	compatBase  float64 // 发鱼低段基座
	compatSlope float64 // 发鱼超重段斜率
	compatFloor float64
}

func hookToleranceBandFor(tol float64) hookToleranceBand {
	tol = groundbaitClamp(tol, 0, 1)
	return hookToleranceBand{
		lo:          .25 - .15*tol,
		solidKink:   .80 + .90*tol,
		solidSlope:  .45 - .18*tol,
		solidFloor:  .15 + .15*tol,
		holdKink:    1.00 + .70*tol,
		holdSlope:   .30 - .12*tol,
		holdFloor:   .50 + .05*tol,
		solidBase:   .55 + .27*tol,
		compatBase:  .35 + .27*tol,
		compatSlope: .80 - .35*tol,
		compatFloor: .10 + .12*tol,
	}
}

// hookSolidHoldTol 在指定宽容进度下返回普通鱼的稳定入口概率与保持率。
// 低段写成 base+(1-base)·(r/lo)：lo=.25 时与原来的 (.55+1.8r) 数值等价。
func hookSolidHoldTol(weightG int64, spec gearPhysicsSpec, tol float64) (solid, hold float64) {
	if weightG <= 0 || !spec.HookPresent {
		return .98, .98
	}
	gape := hookGapeG(spec)
	if gape <= 0 {
		return .98, .98
	}
	b := hookToleranceBandFor(tol)
	r := float64(weightG) / gape
	if r < b.lo {
		// 大钩小鱼：按宽容带把系数从 0.55 抬到 base，越靠近 lo 越接近满分。
		solid = .96 * (b.solidBase + (1-b.solidBase)*(r/b.lo))
	} else {
		solid = .96 - b.solidSlope*math.Max(0, r-b.solidKink)
	}
	solid = groundbaitClamp(solid, b.solidFloor, .96)
	if r <= b.holdKink {
		// 鱼重没有超过宽容带上限时，既然已经中钩，就不再做随机保钩失败。
		hold = 1
	} else {
		hold = groundbaitClamp(.98-b.holdSlope*(r-b.holdKink), b.holdFloor, .98)
	}
	return
}

// hookSpawnCompatibility 是发鱼阶段的几何软匹配权重（普通钓具口径，tol=0）。
func hookSpawnCompatibility(weightG int64, spec gearPhysicsSpec) float64 {
	return hookSpawnCompatibilityTol(weightG, spec, 0)
}

// hookSpawnFloorFade 是发鱼下限的收回系数：鱼重低于下限 hookSpawnFloorG 时，
// 宽容带带来的低段抬升按 重量/下限 线性收回，越靠近 0 越接近不发。
// tol=0 恒为 1，普通钓具与原版逐位一致。
func hookSpawnFloorFade(weightG int64, gape, tol float64) float64 {
	floor := hookSpawnFloorG(gape)
	if floor <= 0 || float64(weightG) >= floor {
		return 1
	}
	keep := groundbaitClamp(float64(weightG)/floor, 0, 1)
	return 1 - groundbaitClamp(tol, 0, 1)*(1-keep)
}

// hookSpawnFloorG 把相对下限换算成绝对鱼重下限：
// r×钩口容量，但不超过 hookSpawnFloorMaxG——海钓 2700kg 钩口按纯比例会抬到 216kg，
// 那会把一票中小型海鱼直接判死，所以给一个绝对封顶。
func hookSpawnFloorG(gape float64) float64 {
	floor := gape * hookSpawnFloorR
	if floor > hookSpawnFloorMaxG {
		return hookSpawnFloorMaxG
	}
	return floor
}

// hookSpawnCompatibilityTol 在指定宽容进度下返回发鱼软匹配权重。
func hookSpawnCompatibilityTol(weightG int64, spec gearPhysicsSpec, tol float64) float64 {
	if weightG <= 0 || !spec.HookPresent {
		return 1
	}
	gape := hookGapeG(spec)
	if gape <= 0 {
		return 1
	}
	b := hookToleranceBandFor(tol)
	r := float64(weightG) / gape
	if r < b.lo {
		v := b.compatBase + (1-b.compatBase)*(r/b.lo)
		v *= hookSpawnFloorFade(weightG, gape, tol)
		return groundbaitClamp(v, .02, 1)
	}
	if r <= b.holdKink {
		return 1
	}
	return groundbaitClamp(1/(1+b.compatSlope*(r-b.holdKink)), b.compatFloor, 1)
}

// applyHookSpawnMatch 对非蓝冠档位应用钩型软匹配，并同比缩放 w4，保持蓝冠
// 的绝对概率不受钩型影响。strength=0 时不改变任何档位权重。
func applyHookSpawnMatch(w1, w2, w3, w4 *float64, compat [4]float64, strength float64) {
	if w1 == nil || w2 == nil || w3 == nil || w4 == nil || strength <= 0 {
		return
	}
	strength = groundbaitClamp(strength, 0, 2)
	baseNonBlue := *w1 + *w2 + *w3
	if baseNonBlue <= 0 {
		return
	}
	adjusted := [3]float64{}
	weights := [3]*float64{w1, w2, w3}
	matchedNonBlue := 0.0
	for i := 0; i < 3; i++ {
		c := groundbaitClamp(compat[i], 0, 1)
		c = groundbaitClamp(1-strength*(1-c), .02, 1)
		adjusted[i] = c
		matchedNonBlue += *weights[i] * c
	}
	if matchedNonBlue <= 0 {
		return
	}
	*w4 *= matchedNonBlue / baseNonBlue
	for i := 0; i < 3; i++ {
		*weights[i] *= adjusted[i]
	}
}

// hookSpawnCompatibilityByGrade 用每个档位的代表重量计算软匹配。档位内部仍
// 使用原分布；蓝冠档固定为 1，由 applyHookSpawnMatch 单独保持其绝对概率。
func (s *Server) hookSpawnCompatibilityByGrade(ctx context.Context, c fishRollContext,
	key string, scale, modeMult float64) [4]float64 {
	compat := [4]float64{1, 1, 1, 1}
	if scale <= 0 {
		scale = 1
	}
	mult := 1.0
	if modeMult > 0 {
		mult = math.Min(modeMult, 2)
	}
	lines := s.cachedSettleStatics(ctx, c, key)
	for grade := int64(1); grade <= 3; grade++ {
		_, _, mode, ok := huntBandBounds(float64(lines[0]), float64(lines[1]), float64(lines[2]), grade)
		if !ok {
			continue
		}
		weight := int64(math.Round(mode * scale * mult))
		compat[grade-1] = hookSpawnCompatibilityTol(weight, c.hook, c.hookTol)
	}
	return compat
}

func (s *Server) applyHookBiteQuality(f *RolledFish, spec gearPhysicsSpec, tol float64) {
	if f == nil {
		return
	}
	f.BiteQuality = "solid"
	f.HookHoldP = 1
	if f.Grade >= 4 {
		// 蓝冠是低概率天命鱼：钩型、嘬饵、保持率全部免检。
		f.LuckProtected = true
		return
	}
	solid, hold := hookSolidHoldTol(f.WeightG, spec, tol)
	if p, ok := s.hookBiteProfileFor(spec.HookItemID); ok {
		solid, hold = applyHookBiteProfile(f.WeightG, spec, p, solid, hold)
	}
	f.HookHoldP = hold
	if s.hookNibbleEnabled() && rand.Float64() > solid {
		f.BiteQuality = "nibble"
	}
}

func hookRetentionFails(f *RolledFish) bool {
	return f != nil && !f.LuckProtected && f.BiteQuality == "solid" &&
		f.HookHoldP > 0 && rand.Float64() > f.HookHoldP
}

func (s *Server) fishFreshwaterHuntReadiness(ctx context.Context, key string, c fishRollContext, h *fishHuntState) fishHuntReadiness {
	out := fishHuntReadiness{Stage: fishHuntDaily}
	if h == nil || s.DB == nil || !s.groundbaitEnabled() {
		return out
	}
	// 从路亚搜索切回窝料钓法时，不把路亚的搜索时长直接继承成窝料发窝度。
	if h.Mode == fishHuntModeLure {
		h.ActiveSeconds, h.ValidBites, h.Pity = 0, 0, 0
		h.BigMiss, h.CompFish, h.CompGrade, h.CompUntil = 0, 0, 0, 0
	}
	h.Mode = fishHuntModeFreshwater
	s.huntTouch(h)
	gb := s.groundbaitSpotAt(ctx, h.UID, h.Loc, c.x, c.z, c.hasPos, h.Spot)
	if gb.units <= 0 {
		return out
	}
	S := s.groundbaitStrength(gb.units)
	overdose := s.groundbaitOverdoseP(gb.units)
	out.A = groundbaitClamp(S*overdose, 0, 1)
	m := s.groundbaitSpeciesMultiplier(ctx, c, gb, key, S, overdose)
	capM := s.tunableF("groundbait_cap", 2.60)
	if capM <= 1.01 {
		capM = 2.60
	}
	out.G = groundbaitClamp((m-1)/(capM-1), 0, 1)

	env := s.fishBiteEnvironmentWeight(ctx, key, c)
	eco := groundbaitClamp((env-0.35)/1.65, 0, 1)
	out.E = groundbaitClamp(.70*eco+.30*s.fishHuntBaitMatch(ctx, key, c), 0, 1)
	out.P = .65*(1-math.Exp(-h.ActiveSeconds/720)) + .35*(1-math.Exp(-float64(h.ValidBites)/12))
	out.P = groundbaitClamp(out.P, 0, 1)
	k := groundbaitClamp(.5+c.skill.EcologyInterest*2+c.skill.TrophyLuck*2, 0, 1)
	out.Q = groundbaitClamp(out.E*out.G*(.25+.75*out.P)*(.90+.10*k), 0, 1)
	out.Comp = h.CompFish > 0 && float64(time.Now().Unix()) <= h.CompUntil

	switch {
	case out.A < .25 || out.E < .45 || out.G < .25:
		out.Stage = fishHuntDaily
	case out.A >= .70 && out.Q >= .85:
		out.Stage = fishHuntGiant
	case out.A >= .55 && out.Q >= .60:
		out.Stage = fishHuntActive
	default:
		out.Stage = fishHuntGather
	}
	return out
}

// pityDue 判断某条轨是否已经走满整个周期：走满后下一竿直接锁定星级档，
// 不再只靠权重放大。换落点、换钓法都不重置（计数按 uid+loc 聚合）。
func (s *Server) pityDue(h *fishHuntState, lane string) bool {
	if s == nil || h == nil || s.tunableF("fish_pity_"+lane, 1) == 0 {
		return false
	}
	var n int64
	var nKey string
	var nBase float64
	if lane == "super" {
		n, nKey, nBase = h.SuperPity, "fish_pity_super_n", 50
	} else {
		n, nKey, nBase = h.TrophyPity, "fish_pity_trophy_n", 20
	}
	if v := s.tunableF(nKey, nBase); v > 0 {
		nBase = v
	}
	return float64(n) >= nBase
}

// pityLaneFactor 把双轨计数换算成权重放大倍数。
// 与 fishHuntWeights 分开：后者是包级函数且有既有测试签名，这里单独持有人依赖。
// lane="trophy" 放大 3 星档，lane="super" 放大 4 星档。
func (s *Server) pityLaneFactor(h *fishHuntState, lane string) float64 {
	if s == nil || h == nil {
		return 1
	}
	var n int64
	var nKey, ceilKey string
	var nDef, ceilDef float64
	if lane == "super" {
		n, nKey, nDef, ceilKey, ceilDef = h.SuperPity, "fish_pity_super_n", 50, "fish_pity_ceil_super", 6
	} else {
		n, nKey, nDef, ceilKey, ceilDef = h.TrophyPity, "fish_pity_trophy_n", 20, "fish_pity_ceil_trophy", 4
	}
	if n <= 0 || s.tunableF("fish_pity_"+lane, 1) == 0 {
		return 1
	}
	nBase := s.tunableF(nKey, nDef)
	if nBase <= 0 {
		nBase = nDef
	}
	ceil := s.tunableF(ceilKey, ceilDef)
	if ceil <= 1 {
		ceil = ceilDef
	}
	prog := math.Min(1, float64(n)/nBase)
	if g := s.tunableF("fish_pity_gamma", .75); g > 0 {
		prog = math.Pow(prog, g)
	}
	return 1 + (ceil-1)*prog
}

// twoStageWeightKeys 返回两档制四档权重的 tunable 键与默认值。
func twoStageWeightKeys(daily bool) ([4]string, [4]float64) {
	if daily {
		return [4]string{"two_stage_daily_below", "two_stage_daily_qualified",
				"two_stage_daily_trophy", "two_stage_daily_super"},
			[4]float64{10, 86.5, 3, .5}
	}
	return [4]string{"two_stage_baited_below", "two_stage_baited_qualified",
			"two_stage_baited_trophy", "two_stage_baited_super"},
		[4]float64{1, 92, 6, 1}
}

// twoStageGradeWeights 读取两档制四档权重：负值与非数值按 0 处理，
// 总和归一到 100（与后台滑杆保存口径一致），全 0 回退默认表。
func (s *Server) twoStageGradeWeights(daily bool) [4]float64 {
	keys, defs := twoStageWeightKeys(daily)
	w := [4]float64{}
	for i := range w {
		v := s.tunableF(keys[i], defs[i])
		if v < 0 || math.IsNaN(v) || math.IsInf(v, 0) {
			v = 0
		}
		w[i] = v
	}
	total := w[0] + w[1] + w[2] + w[3]
	if total <= 0 {
		return defs
	}
	for i := range w {
		w[i] = w[i] / total * 100
	}
	return w
}

func (s *Server) fishHuntWeights(mode, stage int, h *fishHuntState, trophyLuck float64,
	comp bool) (w1, w2, w3, w4 float64, forceTrophy bool) {
	if mode == fishHuntModeFreshwater && s.twoStageHuntEnabled() {
		// 两档制：日常与打窝各一张后台可调表；原聚鱼/发窝/巨物阶段全部按打窝出。
		w := s.twoStageGradeWeights(stage == fishHuntDaily)
		w1, w2, w3, w4 = w[0], w[1], w[2], w[3]
	} else {
		switch stage {
		case fishHuntGather:
			w1, w2, w3, w4 = 54.5, 43, 2, .5
		case fishHuntActive:
			w1, w2, w3, w4 = 30, 61.2, 6.8, 2
		case fishHuntGiant:
			w1, w2, w3, w4 = 10, 64, 20, 6
		default:
			w1, w2, w3, w4 = 74.7, 24, 1, .3
		}
	}
	if stage > fishHuntDaily && h != nil {
		switch {
		case h.Pity >= 50:
			w3 *= 2
		case h.Pity >= 30:
			w3 *= 1.5
		}
		w3 *= 1 + groundbaitClamp(trophyLuck, 0, .5)
		if comp {
			w3 *= 1.3
			if h.CompGrade >= 4 {
				w4 *= 1.5
			}
		}
		if h.Pity >= 60 || h.BigMiss >= 2 {
			forceTrophy = true
		}
	}
	return
}

func chooseHuntGrade(w1, w2, w3, w4 float64, forceTrophy bool) int64 {
	if forceTrophy {
		w1, w2 = 0, 0
		if w3+w4 <= 0 {
			w3 = 1
		}
	}
	total := w1 + w2 + w3 + w4
	if total <= 0 {
		return 1
	}
	r := rand.Float64() * total
	switch {
	case r < w1:
		return 1
	case r < w1+w2:
		return 2
	case r < w1+w2+w3:
		return 3
	default:
		return 4
	}
}

func triangularLog(lo, hi, mode float64) float64 {
	if lo <= 0 {
		lo = 1
	}
	if hi <= lo {
		return lo
	}
	if mode <= lo || mode >= hi {
		mode = math.Sqrt(lo * hi)
	}
	llo, lhi, lm := math.Log(lo), math.Log(hi), math.Log(mode)
	u := rand.Float64()
	f := (lm - llo) / (lhi - llo)
	switch {
	case u < f && lm > llo:
		return math.Exp(llo + math.Sqrt(u*(lhi-llo)*(lm-llo)))
	case lhi > lm:
		return math.Exp(lhi - math.Sqrt((1-u)*(lhi-llo)*(lhi-lm)))
	default:
		return mode
	}
}

func huntBandBounds(q, t, b float64, grade int64) (lo, hi, mode float64, ok bool) {
	if q <= 0 || t <= q || b <= t {
		return 0, 0, 0, false
	}
	switch grade {
	case 1:
		lo, hi, mode = math.Max(1, .15*q), .98*q, .45*q
	case 2:
		lo, hi, mode = 1.02*q, .98*t, math.Min(1.25*q, .92*t)
	case 3:
		lo, hi, mode = 1.02*t, .98*b, math.Min(1.15*t, .92*b)
	default:
		lo, hi, mode = 1.02*b, 1.60*b, 1.10*b
	}
	return lo, hi, mode, true
}

func clampHuntBandWeight(weight, low, high, modeMult, capG float64) float64 {
	if modeMult > 1 {
		weight *= math.Min(modeMult, 2)
	}
	if weight < low {
		weight = low
	}
	if weight > high {
		weight = high
	}
	if capG > 0 && weight > capG {
		weight = capG
	}
	return weight
}

func (s *Server) sampleHuntBand(ctx context.Context, key string, grade int64, modeMult, scale, capG float64, c fishRollContext) int64 {
	lines := s.cachedSettleStatics(ctx, c, key)
	lo, hi, mode, ok := huntBandBounds(float64(lines[0]), float64(lines[1]), float64(lines[2]), grade)
	if !ok {
		return 0
	}
	if capG > 0 {
		cap := capG / math.Max(scale, .0001)
		if cap < lo {
			return 0
		}
		if hi > cap {
			hi = cap
		}
		if mode > hi {
			mode = hi
		}
	}
	w := triangularLog(lo, hi, mode) * scale
	if w < 1 {
		w = 1
	}
	w = clampHuntBandWeight(w, lo*scale, hi*scale, modeMult, capG)
	return int64(math.Round(w))
}

// huntForcedGrade 返回本竿被硬保底锁定的星级（0=没触发）。
// 超稀有优先（4 星 > 3 星）；两档制/档位比例同样要走这里，否则保底只在鱼猎链路生效。
func (s *Server) huntForcedGrade(h *fishHuntState) int64 {
	if s == nil || h == nil || !s.huntStateEnabled() {
		return 0
	}
	if s.pityDue(h, "super") {
		return 4
	}
	if s.pityDue(h, "trophy") {
		return 3
	}
	return 0
}

func (s *Server) sampleHuntWeight(ctx context.Context, key string, modeMult, scale, capG float64, c fishRollContext,
	h *fishHuntState) (int64, int64, fishHuntReadiness) {
	readiness := s.fishHuntReadiness(ctx, key, c, h)
	if s.gradeRatioEnabled() {
		// 档位比例是总开关：不再叠加鱼猎阶段、钩型软匹配。
		// 但**保底必须继续生效**——曾因为这里直接早退，蓝冠硬保底整整两天没触发过。
		forcedGrade := s.huntForcedGrade(h)
		if forcedGrade > 0 {
			weight := int64(0)
			for _, candidate := range []int64{forcedGrade, 3, 2, 1} {
				if candidate > forcedGrade {
					continue
				}
				if weight = s.sampleHuntBand(ctx, key, candidate, modeMult, scale, 0, c); weight > 0 {
					break
				}
			}
			if weight <= 0 {
				weight = s.sampleHuntBand(ctx, key, 2, modeMult, scale, 0, c)
			}
			if weight > 0 {
				s.log.Info("熟练度保底触发", "uid", c.uid, "loc", c.loc, "鱼种", key,
					"星级", forcedGrade, "蓝冠轨", h.SuperPity, "奖杯轨", h.TrophyPity)
				return weight, forcedGrade, readiness
			}
		}
		weight := s.sampleWeightDB(ctx, key, modeMult, scale, capG, c)
		if weight <= 0 {
			return 0, 0, readiness
		}
		return weight, s.rollGradeForRoll(ctx, key, weight, c), readiness
	}
	w1, w2, w3, w4, force := s.fishHuntWeights(readiness.Mode, readiness.Stage, h, c.skill.TrophyLuck, readiness.Comp)
	// 本地服双轨保底：两条轨独立爬坡，全阶段生效（含日常）。
	// trophy 轨只推 3 星，super 轨只推 4 星，互不覆盖。
	w3 *= s.pityLaneFactor(h, "trophy")
	w4 *= s.pityLaneFactor(h, "super")
	// 本地服硬保底：轨走满周期后，下一竿直接锁定星级，不再只靠权重放大。
	// 超稀有优先（4 星 > 3 星），并且这竿不吃装备承重上限——否则小钩子会把
	// 星级档位权重直接清零（capG < 奖杯线时 w3/w4 归零），保底永远不触发。
	forcedGrade := s.huntForcedGrade(h)
	if forcedGrade > 0 {
		capG = 0
	}
	// 原版「日常」阶段的奖杯权重只有 1%、蓝冠 0.3%，没打窝时几乎不可能见到星级鱼。
	// 这里按 tunable 等比放大这两个档位（默认 5 倍 → 奖杯约 4.8%、蓝冠约 1.4%），
	// 设为 1 即完全恢复原版行为。
	if readiness.Stage == fishHuntDaily {
		if b := s.tunableF("fish_hunt_daily_trophy_boost", 3); b > 0 && b != 1 {
			w3 *= b
			w4 *= b
		}
		// 原版只有打窝后的三档才吃奖杯幸运，日常阶段点了技能等于白点；
		// 这里补上，让 TrophyLuck 在所有阶段都生效（打窝档在 fishHuntWeights 内部已加过，不重复）。
		if luck := groundbaitClamp(c.skill.TrophyLuck, 0, .5); luck > 0 {
			w3 *= 1 + luck
		}
	}
	if strength := s.effectiveHookSpawnMatchStrength(); strength > 0 && !force {
		compat := s.hookSpawnCompatibilityByGrade(ctx, c, key, scale, modeMult)
		applyHookSpawnMatch(&w1, &w2, &w3, &w4, compat, strength)
	}
	if capG > 0 {
		lines := s.cachedSettleStatics(ctx, c, key)
		if capG/math.Max(scale, .0001) < float64(lines[2]) {
			w4 = 0
		}
		if capG/math.Max(scale, .0001) < float64(lines[1]) {
			w3, w4 = 0, 0
		}
	}
	grade := chooseHuntGrade(w1, w2, w3, w4, force)
	if forcedGrade > 0 {
		grade = forcedGrade
	}
	weight := int64(0)
	selectedGrade := grade
	for _, candidate := range []int64{grade, 3, 2, 1} {
		if candidate > grade {
			continue
		}
		if weight = s.sampleHuntBand(ctx, key, candidate, modeMult, scale, capG, c); weight > 0 {
			selectedGrade = candidate
			break
		}
	}
	if weight <= 0 {
		// 档位线缺失时仍保留旧模型，避免未知鱼种完全不咬钩。
		fallbackMult := 1.0
		if modeMult > 1 {
			fallbackMult = math.Min(modeMult, 2)
		}
		weight = s.sampleWeightDB(ctx, key, fallbackMult, scale, capG, c)
		selectedGrade = s.rollGradeForRoll(ctx, key, weight, c)
	}
	lines := s.cachedSettleStatics(ctx, c, key)
	if weight > 0 && lines[2] > 0 {
		if maxBlue := int64(math.Round(float64(lines[2]) * 1.60)); weight > maxBlue {
			weight = maxBlue
		}
	}
	if capG > 0 && weight > int64(capG) {
		weight = int64(capG)
	}
	return weight, selectedGrade, readiness
}

func (s *Server) recordHuntBite(ctx context.Context, h *fishHuntState) {
	if !s.huntStateEnabled() || h == nil {
		return
	}
	h.ValidBites++
	s.saveFishHuntState(ctx, h)
}

// recordHuntCatch 只对同一 FishGUID 计一次，避免 14/4 重放重复推进保底。
func (s *Server) recordHuntCatch(ctx context.Context, uid int64, loc string, spot uint64, f *RolledFish) {
	if f == nil || !s.huntStateEnabled() {
		return
	}
	h := s.loadFishHuntState(ctx, uid, loc, spot)
	if h.LastCatchGUID == f.FishGUID {
		return
	}
	h.LastCatchGUID = f.FishGUID
	consumeFishHuntCompensation(h, time.Now().Unix())
	// 本地服双轨保底：两条轨独立计数，与旧 Pity 并存但不互相覆盖。
	// trophy：普通鱼 +step；3 星或 4 星归零。
	// super ：普通鱼 +step；3 星 +superFromTrophy；4 星归零。
	if f.Grade >= 4 {
		h.TrophyPity, h.SuperPity = 0, 0
	} else if f.Grade == 3 {
		h.TrophyPity = 0
		h.SuperPity += int64(s.tunableF("fish_pity_super_from_trophy", 2))
	} else {
		h.TrophyPity += int64(s.tunableF("fish_pity_trophy_step", 1))
		h.SuperPity += int64(s.tunableF("fish_pity_super_step", 1))
	}
	if f.Grade >= 3 {
		h.Pity = 0
		h.BigMiss = 0
		if f.Grade >= 4 {
			h.ActiveSeconds *= .2
			h.ValidBites = int64(float64(h.ValidBites) * .2)
		} else {
			h.ActiveSeconds *= .5
			h.ValidBites = int64(float64(h.ValidBites) * .5)
		}
	} else if f.HuntStageKnown && f.HuntStage >= fishHuntGather {
		h.Pity++
	}
	s.saveFishHuntState(ctx, h)
}

func (s *Server) recordHuntEscape(ctx context.Context, uid int64, loc string, spot uint64, f *RolledFish) {
	if f == nil || f.Grade < 3 || !s.huntStateEnabled() {
		return
	}
	h := s.loadFishHuntState(ctx, uid, loc, spot)
	h.BigMiss++
	h.CompFish = 3
	h.CompGrade = f.Grade
	h.CompUntil = float64(time.Now().Unix() + fishHuntCompWindowSeconds)
	if f.Grade >= 4 {
		h.CompFish = 4
	}
	s.saveFishHuntState(ctx, h)
	s.log.Info("大鱼脱钩补偿", "uid", uid, "loc", loc, "spot", spot,
		"grade", f.Grade, "big_miss", h.BigMiss, "until", h.CompUntil)
}

func consumeFishHuntCompensation(h *fishHuntState, now int64) {
	if h == nil || h.CompFish <= 0 {
		return
	}
	if h.CompUntil <= float64(now) {
		h.CompFish, h.CompGrade, h.CompUntil = 0, 0, 0
		return
	}
	h.CompFish--
	if h.CompFish <= 0 {
		h.CompFish, h.CompGrade, h.CompUntil = 0, 0, 0
	}
}

func (s *Server) fishHuntStageName(mode, stage int) string {
	if mode == fishHuntModeFreshwater && s.twoStageHuntEnabled() {
		if stage == fishHuntDaily {
			return "日常"
		}
		return "打窝"
	}
	switch stage {
	case fishHuntGather:
		return "聚鱼"
	case fishHuntActive:
		return "发窝"
	case fishHuntGiant:
		return "巨物发窝"
	default:
		return "日常"
	}
}
