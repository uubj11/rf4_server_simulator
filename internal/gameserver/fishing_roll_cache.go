package gameserver

import (
	"context"
	"sync"

	"rf4server/internal/store"
)

type cachedGradeLines struct {
	val, trophy, blue *int64
	// minG 是该鱼种的自然最小体重（species_weight.min_g），用于给「未达标」
	// 档定下界。nil 表示没有可用的 min_g。
	minG *int64
}

type cachedBaitDef struct {
	bait ruleBait
	ok   bool
}

// legacyBaitIDs 把 item_catalog 里的新 id 映射回 companion_bait_fish 快照中的
// 旧 id。两张表同名不同 id 并存（craft_bait_dough_cottage 等），只认新 id 会
// 让这批证据永远查不到。
var legacyBaitIDs = map[string]string{
	"dough_cottage": "craft_bait_dough_cottage",
	"dough_garlic":  "craft_bait_dough_garlic",
	"porridge_corn": "craft_bait_kukuruz_kasha",
	"dough_honey":   "craft_bait_med_testo",
	"c_bleak_lf":    "c_bleak",
}

type localBaitEvidenceKey struct {
	loc  string
	bait string
}

// cachedGroundbaitAff 缓存「这一锅窝料 x 这条鱼」的食性契合度结论。
// 契合度与天气无关，只跟成分和鱼种食性有关，一次判鱼里重复率极高。
type cachedGroundbaitAff struct {
	row    groundbaitAffinityRow
	status int
}

// fishingRollCache 只在一次判鱼会话或测试批量采样内复用静态资料。
// 运行时鱼口覆盖等可变数据不进入缓存。
type fishingRollCache struct {
	mu                   sync.Mutex
	statLocs             map[string]string
	rosters              map[string][]*store.FishRosterRow
	gradeLines           map[string]cachedGradeLines
	settleStatics        map[string][6]int64
	lengthKP             map[string][2]float64
	baitTiers            map[string]map[string]int64
	baitTierLoaded       map[string]bool
	companionEvidence    map[string]int64
	companionEvidenceSet bool
	baitEvidence         map[string]map[string]int64
	baitEvidenceLoaded   map[string]bool
	evidenceAvg          map[string]float64
	evidenceAvgLoaded    bool
	baitDefs             map[string]cachedBaitDef
	fishNames            map[string]string
	fishNamesLoaded      bool
	fishNamesOK          bool
	localEvidence        map[localBaitEvidenceKey]map[string]bool
	fishEcology          map[string]fishEcologyProfile
	fishEcologyLoaded    map[string]bool
	gbAff                map[groundbaitAffKey]cachedGroundbaitAff
	gbFishDiet           map[string]map[string]float64
	speciesMinG          map[string]int64
	speciesMinGLoaded    map[string]bool
}

func newFishingRollCache() *fishingRollCache {
	return &fishingRollCache{
		statLocs:           map[string]string{},
		rosters:            map[string][]*store.FishRosterRow{},
		gradeLines:         map[string]cachedGradeLines{},
		settleStatics:      map[string][6]int64{},
		lengthKP:           map[string][2]float64{},
		baitTiers:          map[string]map[string]int64{},
		baitTierLoaded:     map[string]bool{},
		baitEvidence:       map[string]map[string]int64{},
		baitEvidenceLoaded: map[string]bool{},
		baitDefs:           map[string]cachedBaitDef{},
		localEvidence:      map[localBaitEvidenceKey]map[string]bool{},
		fishEcology:        map[string]fishEcologyProfile{},
		fishEcologyLoaded:  map[string]bool{},
		gbAff:              map[groundbaitAffKey]cachedGroundbaitAff{},
		gbFishDiet:         map[string]map[string]float64{},
		speciesMinG:        map[string]int64{},
		speciesMinGLoaded:  map[string]bool{},
	}
}

func rollCache(c fishRollContext) *fishingRollCache { return c.cache }

// cachedSpeciesMinG 取该鱼种的自然最小体重（species_weight.min_g），
// 供「未达标」档计算下界。找不到数据或值非正时返回 ok=false。
func (s *Server) cachedSpeciesMinG(ctx context.Context, c fishRollContext, key string) (int64, bool) {
	if s == nil || s.DB == nil || key == "" {
		return 0, false
	}
	cache := rollCache(c)
	if cache != nil {
		cache.mu.Lock()
		if cache.speciesMinGLoaded[key] {
			v := cache.speciesMinG[key]
			cache.mu.Unlock()
			return v, v > 0
		}
		cache.mu.Unlock()
	}
	var v int64
	if err := s.DB.SQL.QueryRowContext(ctx,
		`SELECT COALESCE(min_g,0) FROM species_weight WHERE fish_key=?`, key).Scan(&v); err != nil {
		v = 0
	}
	if cache != nil {
		cache.mu.Lock()
		cache.speciesMinG[key] = v
		cache.speciesMinGLoaded[key] = true
		cache.mu.Unlock()
	}
	return v, v > 0
}

func (s *Server) cachedResolveStatLoc(ctx context.Context, c fishRollContext, loc string) string {
	if s.DB == nil {
		return ""
	}
	cache := rollCache(c)
	if cache == nil {
		return s.DB.ResolveStatLoc(ctx, loc)
	}
	cache.mu.Lock()
	if value, ok := cache.statLocs[loc]; ok {
		cache.mu.Unlock()
		return value
	}
	cache.mu.Unlock()
	value := s.DB.ResolveStatLoc(ctx, loc)
	cache.mu.Lock()
	cache.statLocs[loc] = value
	cache.mu.Unlock()
	return value
}

func (s *Server) cachedLocRoster(ctx context.Context, c fishRollContext, statLoc string) ([]*store.FishRosterRow, error) {
	if s.DB == nil {
		return nil, nil
	}
	cache := rollCache(c)
	if cache == nil {
		return s.DB.LocRoster(ctx, statLoc)
	}
	cache.mu.Lock()
	if rows, ok := cache.rosters[statLoc]; ok {
		cache.mu.Unlock()
		return rows, nil
	}
	cache.mu.Unlock()
	rows, err := s.DB.LocRoster(ctx, statLoc)
	if err != nil {
		return nil, err
	}
	cache.mu.Lock()
	cache.rosters[statLoc] = rows
	cache.mu.Unlock()
	return rows, nil
}

func (s *Server) cachedGradeLines(ctx context.Context, c fishRollContext, key string) cachedGradeLines {
	if s.DB == nil {
		return cachedGradeLines{}
	}
	cache := rollCache(c)
	if cache == nil {
		val, trophy, blue := s.DB.GradeLines(ctx, key)
		return cachedGradeLines{val: val, trophy: trophy, blue: blue}
	}
	cache.mu.Lock()
	if lines, ok := cache.gradeLines[key]; ok {
		cache.mu.Unlock()
		return lines
	}
	cache.mu.Unlock()
	val, trophy, blue := s.DB.GradeLines(ctx, key)
	lines := cachedGradeLines{val: val, trophy: trophy, blue: blue}
	cache.mu.Lock()
	cache.gradeLines[key] = lines
	cache.mu.Unlock()
	return lines
}

func (s *Server) cachedSpeciesLengthKP(ctx context.Context, c fishRollContext, key string) (float64, float64) {
	if s.DB == nil {
		return defaultSpeciesLengthK, defaultSpeciesLengthP
	}
	cache := rollCache(c)
	if cache == nil {
		k, p := s.DB.SpeciesLengthKP(ctx, key)
		return calibratedLengthKP(key, k, p)
	}
	cache.mu.Lock()
	if values, ok := cache.lengthKP[key]; ok {
		cache.mu.Unlock()
		return values[0], values[1]
	}
	cache.mu.Unlock()
	k, p := s.DB.SpeciesLengthKP(ctx, key)
	k, p = calibratedLengthKP(key, k, p)
	cache.mu.Lock()
	cache.lengthKP[key] = [2]float64{k, p}
	cache.mu.Unlock()
	return k, p
}

func (s *Server) cachedBaitTiers(ctx context.Context, c fishRollContext, bait string) map[string]int64 {
	if s.DB == nil {
		return nil
	}
	cache := rollCache(c)
	if cache == nil {
		return s.DB.BaitTiers(ctx, bait)
	}
	cache.mu.Lock()
	if cache.baitTierLoaded[bait] {
		tiers := cache.baitTiers[bait]
		cache.mu.Unlock()
		return tiers
	}
	cache.mu.Unlock()
	tiers := s.DB.BaitTiers(ctx, bait)
	if len(tiers) == 0 {
		// companion_bait_fish 预置快照用的是旧 item_id（craft_bait_* / c_bleak），
		// 这些 id 在当前 item_def 里已不存在；不改名会让 264 行证据永久失效。
		if old, ok := legacyBaitIDs[bait]; ok {
			tiers = s.DB.BaitTiers(ctx, old)
		}
	}
	cache.mu.Lock()
	cache.baitTiers[bait] = tiers
	cache.baitTierLoaded[bait] = true
	cache.mu.Unlock()
	return tiers
}

func (s *Server) cachedCompanionEvidence(ctx context.Context, c fishRollContext) map[string]int64 {
	if s.DB == nil {
		return nil
	}
	cache := rollCache(c)
	if cache == nil {
		return s.DB.CompanionEvidence(ctx)
	}
	cache.mu.Lock()
	if cache.companionEvidenceSet {
		evidence := cache.companionEvidence
		cache.mu.Unlock()
		return evidence
	}
	cache.mu.Unlock()
	evidence := s.DB.CompanionEvidence(ctx)
	cache.mu.Lock()
	cache.companionEvidence = evidence
	cache.companionEvidenceSet = true
	cache.mu.Unlock()
	return evidence
}

// cachedBaitEvidence 缓存「该饵对各鱼种的真实证据」，与 cachedBaitTiers 同生命周期。
func (s *Server) cachedBaitEvidence(ctx context.Context, c fishRollContext, bait string) map[string]int64 {
	if s.DB == nil {
		return nil
	}
	cache := rollCache(c)
	if cache == nil {
		return s.DB.BaitEvidence(ctx, bait)
	}
	cache.mu.Lock()
	if cache.baitEvidenceLoaded[bait] {
		ev := cache.baitEvidence[bait]
		cache.mu.Unlock()
		return ev
	}
	cache.mu.Unlock()
	ev := s.DB.BaitEvidence(ctx, bait)
	cache.mu.Lock()
	cache.baitEvidence[bait] = ev
	cache.baitEvidenceLoaded[bait] = true
	cache.mu.Unlock()
	return ev
}

// cachedCompanionEvidenceAvg 缓存「该鱼种全部饵的平均证据」，作为连续加权的分母。
func (s *Server) cachedCompanionEvidenceAvg(ctx context.Context, c fishRollContext) map[string]float64 {
	if s.DB == nil {
		return nil
	}
	cache := rollCache(c)
	if cache == nil {
		return s.DB.CompanionEvidenceAvg(ctx)
	}
	cache.mu.Lock()
	if cache.evidenceAvgLoaded {
		avg := cache.evidenceAvg
		cache.mu.Unlock()
		return avg
	}
	cache.mu.Unlock()
	avg := s.DB.CompanionEvidenceAvg(ctx)
	cache.mu.Lock()
	cache.evidenceAvg = avg
	cache.evidenceAvgLoaded = true
	cache.mu.Unlock()
	return avg
}

func (s *Server) cachedBaitDefinition(ctx context.Context, c fishRollContext, id string) (ruleBait, bool) {
	if s.DB == nil {
		return ruleBait{}, false
	}
	cache := rollCache(c)
	if cache == nil {
		return s.loadRuleBait(ctx, id)
	}
	cache.mu.Lock()
	if cached, ok := cache.baitDefs[id]; ok {
		cache.mu.Unlock()
		return cached.bait, cached.ok
	}
	cache.mu.Unlock()
	bait, ok := s.loadRuleBait(ctx, id)
	cache.mu.Lock()
	cache.baitDefs[id] = cachedBaitDef{bait: bait, ok: ok}
	cache.mu.Unlock()
	return bait, ok
}

func (s *Server) cachedFishNames(ctx context.Context, c fishRollContext) (map[string]string, bool) {
	if s.DB == nil {
		return map[string]string{}, false
	}
	cache := rollCache(c)
	if cache == nil {
		return s.loadFishNames(ctx)
	}
	cache.mu.Lock()
	if cache.fishNamesLoaded {
		names, ok := cache.fishNames, cache.fishNamesOK
		cache.mu.Unlock()
		return names, ok
	}
	cache.mu.Unlock()
	names, ok := s.loadFishNames(ctx)
	cache.mu.Lock()
	cache.fishNames = names
	cache.fishNamesOK = ok
	cache.fishNamesLoaded = true
	cache.mu.Unlock()
	return names, ok
}

func (s *Server) cachedLocalBaitEvidence(ctx context.Context, c fishRollContext, loc, baitID, baitName string) map[string]bool {
	if s.DB == nil {
		return map[string]bool{}
	}
	cache := rollCache(c)
	if cache == nil {
		return s.loadLocalBaitEvidence(ctx, loc, baitID, baitName)
	}
	key := localBaitEvidenceKey{loc: loc, bait: baitID}
	cache.mu.Lock()
	if evidence, ok := cache.localEvidence[key]; ok {
		cache.mu.Unlock()
		return evidence
	}
	cache.mu.Unlock()
	evidence := s.loadLocalBaitEvidence(ctx, loc, baitID, baitName)
	cache.mu.Lock()
	cache.localEvidence[key] = evidence
	cache.mu.Unlock()
	return evidence
}

func (s *Server) cachedSettleStatics(ctx context.Context, c fishRollContext, key string) [6]int64 {
	cache := rollCache(c)
	if cache == nil {
		return s.settleStatics(ctx, key)
	}
	cache.mu.Lock()
	if values, ok := cache.settleStatics[key]; ok {
		cache.mu.Unlock()
		return values
	}
	cache.mu.Unlock()
	values := s.settleStatics(ctx, key)
	cache.mu.Lock()
	cache.settleStatics[key] = values
	cache.mu.Unlock()
	return values
}

func (s *Server) cachedFishEcology(ctx context.Context, c fishRollContext, key string) fishEcologyProfile {
	cache := rollCache(c)
	if cache == nil {
		return s.fishEcology(ctx, key)
	}
	cache.mu.Lock()
	if cache.fishEcologyLoaded[key] {
		profile := cache.fishEcology[key]
		cache.mu.Unlock()
		return profile
	}
	cache.mu.Unlock()
	profile := s.fishEcology(ctx, key)
	cache.mu.Lock()
	cache.fishEcology[key] = profile
	cache.fishEcologyLoaded[key] = true
	cache.mu.Unlock()
	return profile
}
