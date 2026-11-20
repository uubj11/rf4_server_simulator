package gameserver

import (
	"context"
	"math"
	"sort"
	"time"
)

// meanBiteDelay averages the production delay AFTER applying its clamps.
// It does not sample RNG or include client wait, occupied rods or landing time.
func meanBiteDelay(cfg biteDelaySettings, lure bool, weather, speed float64) float64 {
	if cfg.fixed > 0 {
		return math.Max(.05, cfg.fixed)
	}
	if !lure {
		var sum float64
		for _, sample := range officialPushDelayEligible {
			sum += standardBiteDelay(sample*weather, speed, cfg.maxDelay, cfg.scale)
		}
		if len(officialPushDelayEligible) > 0 {
			return sum / float64(len(officialPushDelayEligible))
		}
		// Same rounded uniform bands as the production fallback.
		for _, band := range pushDelayBands {
			for cent := int(math.Floor(band.lo * 100)); cent <= int(math.Ceil(band.hi*100)); cent++ {
				x := float64(cent) / 100
				width := math.Max(0, math.Min(band.hi, x+.005)-math.Max(band.lo, x-.005))
				sum += band.p * width / (band.hi - band.lo) * standardBiteDelay(x*weather, speed, cfg.maxDelay, cfg.scale)
			}
		}
		return sum
	}
	// Uniform [1.5,6], transformed by piecewise-linear clamps. Integration at
	// each segment midpoint is exact; averaging the raw window is not.
	points := []float64{1.5, 6}
	k := weather * speed
	for _, x := range []float64{.8 / k, cfg.lureMax / k, .8 / (k * cfg.scale)} {
		if x > 1.5 && x < 6 {
			points = append(points, x)
		}
	}
	sort.Float64s(points)
	var sum float64
	for i := 1; i < len(points); i++ {
		a, b := points[i-1], points[i]
		sum += (b - a) * lureBiteDelay((a+b)/2*weather, speed, cfg.lureMax, cfg.scale)
	}
	return sum / 4.5
}

func (s *Server) fishQueryMeanDelay(st *FishState, weather fishingWeather, at time.Time) float64 {
	cfg := s.effectiveBiteDelaySettings(st)
	if cfg.fixed > 0 {
		return meanBiteDelay(cfg, false, 1, 1)
	}
	return meanBiteDelay(cfg, rigTypeLureFamily[st.RigType], s.weatherBiteDelayFactor(st, weather, at), st.Bonus.BiteSpeed)
}

// fishQueryPaceState 查鱼助手的节奏因子：按候选饵算对口分（查询假设无窝料，
// Ground=1；打窝的实际手感以实钓为准）。c 携带请求级缓存，同一次查询内
// 名册与饵证据只查一次。
func (s *Server) fishQueryPaceState(ctx context.Context, c fishRollContext, st *FishState) fishPaceState {
	if !s.paceEnabled() || st == nil {
		return neutralFishPaceState()
	}
	score := s.baitPaceScore(ctx, c, st.Loc, []string{st.Bait}, st.RigType)
	factor, prob, lureProb, tier := s.paceTierFor(score)
	return fishPaceState{Known: true, Score: score, Factor: factor, Prob: prob, LureProb: lureProb, Ground: 1, Tier: tier}
}

func (p *FishQueryPlan) setTheoreticalRate(delay float64) {
	if !finiteSonarNumber(delay) || delay <= 0 || !finiteSonarNumber(p.Chance) {
		return
	}
	p.MeanDelaySeconds = delay
	p.TheoreticalPerMinute = 60 * clampFloat(p.Chance, 0, 1) / delay
	p.TheoreticalPerHour = 60 * p.TheoreticalPerMinute
}

func fishQueryPlanBetter(a, b FishQueryPlan) bool {
	if math.Abs(a.TheoreticalPerHour-b.TheoreticalPerHour) < 1e-12 {
		return a.WaitSeconds < b.WaitSeconds
	}
	return a.TheoreticalPerHour > b.TheoreticalPerHour
}
