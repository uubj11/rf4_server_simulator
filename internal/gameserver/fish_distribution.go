package gameserver

import (
	"fmt"
	"math"
	"sync/atomic"
	"time"
)

// distributionPeriodRealHours 是鱼群聚散（以及饵效抖动）的重排周期，单位是
// 真实小时：每 12 真实小时重排一次。周期内同一格子同一鱼种的结果
// 固定，不因玩家移动或换饵重抽。
const distributionPeriodRealHours = 12

// 鱼群聚散里的「今天这条鱼完全不出现」只留给少数鱼：图内权重份额越高的鱼
// 越不容易被刷掉，份额达到 distributionAlwaysPresentShare 的鱼永不缺席。
// 否则会出现「这张图的主打鱼整整一天一条都钓不到」，与基础权重的语义矛盾。
const (
	distributionBaseZeroFloor      = 0.10
	distributionAlwaysPresentShare = 0.05
	// distributionRarityFloorStep 是稀有度对缺席判定线的抬升量：每高一级抬
	// 5 个百分点。鳗鱼这类稀有鱼在热门点位被蹲得渔获量很高、份额能进前五，
	// 如果只看份额就会变成「天天有」，与稀有度语义冲突，所以用稀有度把线抬回去。
	distributionRarityFloorStep = 0.05
	distributionZeroFloorCap    = 0.35
)

// fishRaritySnapshot 缓存 fish_key -> 物种稀有度（0 普通 / 1 稀有 / 2 超稀有），
// 供缺席判定使用。由 attachDB 在接入数据库时灌入，未加载时一律按普通鱼处理。
var fishRaritySnapshot atomic.Value // map[string]int64

func setFishRaritySnapshot(m map[string]int64) { fishRaritySnapshot.Store(m) }

func fishRarityOf(key string) int64 {
	if m, ok := fishRaritySnapshot.Load().(map[string]int64); ok {
		return m[key]
	}
	return 0
}

// distributionZeroFloor 返回该鱼在该图的缺席判定线：散布值低于这条线就整天不出现。
// 只有稀有鱼才会整天缺席，普通鱼（rarity 0）永不缺席——它们是这个水域的常驻鱼，
// 「某天一条都钓不到」只会让玩家以为鱼没刷出来。稀有度每高一级抬 5 个百分点；
// 份额 ≥ distributionAlwaysPresentShare 的稀有鱼打对折，避免热门稀有鱼被蹲成常驻。
// 上限 distributionZeroFloorCap，防止超稀有鱼变成整天钓不到。
func distributionZeroFloor(share float64, rarity int64) float64 {
	if rarity <= 0 {
		return 0
	}
	floor := distributionRarityFloorStep * float64(rarity)
	if share >= distributionAlwaysPresentShare {
		floor /= 2
	}
	return math.Min(floor, distributionZeroFloorCap)
}

// 区域以玩家世界位置划分；缺位置时使用客户端场景与钓点编号。
func fishArea(loc string, c fishRollContext) string {
	if c.hasPos && !math.IsNaN(c.x) && !math.IsNaN(c.z) && !math.IsInf(c.x, 0) && !math.IsInf(c.z, 0) {
		return fmt.Sprintf("%s/%.0f/%.0f", loc, math.Floor(c.x/40), math.Floor(c.z/40))
	}
	return fmt.Sprintf("%s/%d", loc, c.spot)
}

// activityMultiplier 保留非活跃时段的少量杂口，未知鱼种使用中性昼夜权重。
func activityMultiplier(species string, hour int64) float64 {
	night := hour >= 19 || hour < 6
	switch species {
	case "ruffe", "ruffe_n", "eel", "burbot", "w_catfish", "w_catfish_a", "bullhead_syb", "ch_catfish", "fhcatfish":
		if night {
			return 1.8
		}
		return 0.35
	case "n.pike", "perch", "c_bleak", "rudd", "barb_toni":
		if night {
			return 0.45
		}
		return 1.4
	}
	return 1
}

func distributionMultiplier(area, species string, now time.Time, share float64) float64 {
	_, hour := worldDayAndHour(now.Unix())
	period := (now.Unix() - worldDayEpochUnix) / (distributionPeriodRealHours * 3600)
	v := fishVariation(fmt.Sprintf("%s/%s/%d", area, species, period))
	// 鱼群聚散只留一档「明显不来」（v<0.10，约十分之一的格子），其余时刻
	// 只做 0.64~1.45 的强弱区分。原来的 v<0.30 + 0.15+3.85v² 会让同一期
	// 三成鱼种直接归零、非零跨度 26 倍，实测单图单期有效鱼种中位只有 7.5、
	// 单饵最高占比 25.5%（90 分位 42%），体感就是「一款饵只上个别几种鱼」。
	// 改后同口径 10.2 / 19.8%（90 分位 31%）。
	if v < distributionZeroFloor(share, fishRarityOf(species)) {
		return 0
	}
	return (0.55 + 0.90*v) * activityMultiplier(species, hour)
}

func applyFishDistribution(loc string, keys []string, weights []float64, c fishRollContext) {
	area := fishArea(loc, c)
	total := 0.0
	for _, w := range weights {
		if w > 0 {
			total += w
		}
	}
	for i, key := range keys {
		share := 0.0
		if total > 0 && weights[i] > 0 {
			share = weights[i] / total
		}
		m := distributionMultiplier(area, key, c.now, share)
		// 保底杂口：本时段本格散布为 0 的鱼保底 0.15，避免连续空杆死寂；
		// 仅 pityEnv 重掷路径生效，正常判鱼不动。
		if m == 0 && c.pityEnv {
			m = 0.15
		}
		weights[i] *= m
	}
}
