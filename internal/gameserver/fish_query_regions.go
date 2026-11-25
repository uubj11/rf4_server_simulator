package gameserver

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"time"

	"rf4server/internal/fishmaps"
	"rf4server/internal/store"
)

var errFishQueryRegionMapUnsupported = errors.New("暂不支持该地图的区域查询")

const (
	fishQueryCoarseRegionLimit = 32
	fishQueryRegionAxisLimit   = 76 // Includes the offset 3000-unit islands terrain.
)

// 纸地图标定与贴图名单一事实源在 internal/fishmaps（admin 后台地图编辑器共用）；
// 这里保留本地别名，查鱼扫描与既有测试的类型名不变。
type fishQueryRegionScene = fishmaps.Scene

type fishQueryPaperMap = fishmaps.PaperMap

var fishQueryRegionScenes = fishmaps.Scenes

// FishQueryRegion is a half-open rectangle in continuous HUD/map coordinates,
// not a selected cast point or a guarantee of water/shore access.
type FishQueryRegion struct {
	MinX float64 `json:"min_x"`
	MinZ float64 `json:"min_z"`
	MaxX float64 `json:"max_x"`
	MaxZ float64 `json:"max_z"`
}

type fishQueryRegionalWeight struct {
	key                string
	weight, acceptance float64
}

type fishQueryRegions struct {
	areas                             []string
	bounds                            []FishQueryRegion
	now                               time.Time
	enabled                           bool
	factors                           map[string][]float64
	scanned                           int
	worldBounds                       [4]float64
	minCellX, minCellZ, width, height int
	waterFiltered                     bool
	// zones 是该水域的区域固定鱼种名册；cells 记录每个区域代表点的世界坐标
	// （与 fishArea 用的格角同源），供区域评分乘名册系数。
	zones []store.FishZone
	cells [][2]float64
}

func newFishQueryRegions(loc string, scene fishQueryRegionScene, now time.Time, enabled bool, zones []store.FishZone) (*fishQueryRegions, error) {
	minWorldX, minWorldZ := scene.OriginX, scene.OriginZ
	maxWorldX, maxWorldZ := scene.OriginX+scene.Size, scene.OriginZ+scene.Size
	minCellX, minCellZ := math.Floor(scene.OriginX/fishQueryAreaSize), math.Floor(scene.OriginZ/fishQueryAreaSize)
	maxCellX, maxCellZ := math.Ceil(maxWorldX/fishQueryAreaSize), math.Ceil(maxWorldZ/fishQueryAreaSize)
	if !validFishQueryCoordinate(scene.Size) || scene.Size <= 0 ||
		!validFishQueryCoordinate(scene.OriginX) || !validFishQueryCoordinate(scene.OriginZ) ||
		!validFishQueryCoordinate(maxWorldX) || !validFishQueryCoordinate(maxWorldZ) ||
		maxWorldX <= scene.OriginX || maxWorldZ <= scene.OriginZ ||
		maxCellX-minCellX > fishQueryRegionAxisLimit || maxCellZ-minCellZ > fishQueryRegionAxisLimit {
		return nil, fmt.Errorf("区域扫描范围无效")
	}
	if p := scene.Paper; p != nil {
		scale, offsetX, offsetZ := float64(p.Scale), float64(p.OffsetX), float64(p.OffsetZ)
		if !validFishQueryCoordinate(scale) || scale <= 0 ||
			!validFishQueryCoordinate(offsetX) || !validFishQueryCoordinate(offsetZ) ||
			!validFishQueryCoordinate(p.Width) || !validFishQueryCoordinate(p.Height) || p.Width <= 0 || p.Height <= 0 {
			return nil, fmt.Errorf("纸地图范围无效")
		}
		// Intersect in world space before scoring. Do not clamp a winning result
		// afterwards: that would move its probability to a different fish cell.
		minWorldX = math.Max(minWorldX, scene.OriginX-offsetX/scale)
		minWorldZ = math.Max(minWorldZ, scene.OriginZ-offsetZ/scale)
		maxWorldX = math.Min(maxWorldX, scene.OriginX+(p.Width-offsetX)/scale)
		maxWorldZ = math.Min(maxWorldZ, scene.OriginZ+(p.Height-offsetZ)/scale)
		if minWorldX >= maxWorldX || minWorldZ >= maxWorldZ {
			return nil, fmt.Errorf("纸地图与地形没有有效交集")
		}
		minCellX, minCellZ = math.Floor(minWorldX/fishQueryAreaSize), math.Floor(minWorldZ/fishQueryAreaSize)
		maxCellX, maxCellZ = math.Ceil(maxWorldX/fishQueryAreaSize), math.Ceil(maxWorldZ/fishQueryAreaSize)
	}
	g := &fishQueryRegions{now: now, enabled: enabled, factors: map[string][]float64{},
		worldBounds: [4]float64{minWorldX, minWorldZ, maxWorldX, maxWorldZ},
		minCellX:    int(minCellX), minCellZ: int(minCellZ), width: int(maxCellX - minCellX), height: int(maxCellZ - minCellZ),
		zones: zones}
	if !enabled {
		return g, nil
	}
	frame := fishQueryMapFrame{originX: scene.OriginX, originZ: scene.OriginZ}
	// Align with production's world grid, not the scene origin. An offset scene
	// or paper map can begin/end in a cell; clip both ends before projecting.
	for ix := minCellX; ix < maxCellX; ix++ {
		for iz := minCellZ; iz < maxCellZ; iz++ {
			x, z := math.Max(ix*fishQueryAreaSize, minWorldX), math.Max(iz*fishQueryAreaSize, minWorldZ)
			mx, mz, _ := frame.project(x, z)
			maxX, maxZ, _ := frame.project(math.Min((ix+1)*fishQueryAreaSize, maxWorldX), math.Min((iz+1)*fishQueryAreaSize, maxWorldZ))
			g.bounds = append(g.bounds, FishQueryRegion{MinX: mx, MinZ: mz, MaxX: maxX, MaxZ: maxZ})
			g.areas = append(g.areas, fishArea(loc, fishRollContext{x: x, z: z, hasPos: true}))
			g.cells = append(g.cells, [2]float64{x, z})
		}
	}
	g.scanned = len(g.areas)
	return g, nil
}

// distributionFactorCacheKey 把 share 的缺席判定档一并纳入缓存键。
// distributionZeroFloor 只依赖 share 是否达到 5%，因此按该阈值分桶即可；
// shortlist 与 best 使用不同权重集时不能复用同一份因子。
func distributionFactorCacheKey(key string, share float64) string {
	bucket := "0"
	if share >= distributionAlwaysPresentShare {
		bucket = "1"
	}
	return key + "\x00" + bucket
}

func (g *fishQueryRegions) distributionFactors(ctx context.Context, key string, share float64) ([]float64, error) {
	cacheKey := distributionFactorCacheKey(key, share)
	if factors, ok := g.factors[cacheKey]; ok {
		return factors, nil
	}
	factors := make([]float64, len(g.areas))
	for i, area := range g.areas {
		if i%64 == 0 && ctx.Err() != nil {
			return nil, ctx.Err()
		}
		factors[i] = distributionMultiplier(area, key, g.now, share)
		if len(g.zones) > 0 && i < len(g.cells) {
			factors[i] *= fishZoneFactorForPoint(g.zones, g.cells[i][0], g.cells[i][1], key)
		}
	}
	g.factors[cacheKey] = factors
	return factors, nil
}

// shortlist ranks regions before bait evaluation. Its bait-free weight share
// is only a coarse score, not the final probability of a successful bite.
func (g *fishQueryRegions) shortlist(ctx context.Context, fish string, weights map[string]float64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !g.enabled {
		return nil
	}
	keys := make([]string, 0, len(weights))
	weightSum := 0.0
	for key, weight := range weights {
		if weight > 0 {
			keys = append(keys, key)
			weightSum += weight
		}
	}
	sort.Strings(keys)
	total := make([]float64, len(g.areas))
	for _, key := range keys {
		share := 0.0
		if weightSum > 0 {
			share = weights[key] / weightSum
		}
		factors, err := g.distributionFactors(ctx, key, share)
		if err != nil {
			return err
		}
		for i, factor := range factors {
			total[i] += weights[key] * factor
		}
	}
	type scoredRegion struct {
		index int
		score float64
	}
	var ranked []scoredRegion
	if weights[fish] > 0 {
		fishShare := 0.0
		if weightSum > 0 {
			fishShare = weights[fish] / weightSum
		}
		for i, factor := range g.factors[distributionFactorCacheKey(fish, fishShare)] {
			if i%64 == 0 && ctx.Err() != nil {
				return ctx.Err()
			}
			if factor > 0 && total[i] > 0 {
				ranked = append(ranked, scoredRegion{i, weights[fish] * factor / total[i]})
			}
		}
	}
	sort.SliceStable(ranked, func(i, j int) bool { return ranked[i].score > ranked[j].score })
	ranked = ranked[:min(len(ranked), fishQueryCoarseRegionLimit)]
	areas := make([]string, len(ranked))
	bounds := make([]FishQueryRegion, len(ranked))
	for i, row := range ranked {
		areas[i], bounds[i] = g.areas[row.index], g.bounds[row.index]
	}
	for key, factors := range g.factors {
		selected := make([]float64, len(ranked))
		for i, row := range ranked {
			selected[i] = factors[row.index]
		}
		g.factors[key] = selected
	}
	g.areas, g.bounds = areas, bounds
	return nil
}

// best applies the shared regional distribution to each bait's captured species
// weights. It compares normalized probabilities, not just target abundance.
// No SQL, production RNG or point-by-point ecology evaluation occurs here.
func (g *fishQueryRegions) best(ctx context.Context, q *fishQueryCapture) (*FishQueryRegion, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !g.enabled {
		return nil, nil // uniform distribution: do not invent a winning location
	}
	q.chance, q.selection, q.fishShare = 0, 0, 0
	target := -1
	vectors := make([][]float64, len(q.regionalWeights))
	weightSum := 0.0
	for _, row := range q.regionalWeights {
		if row.weight > 0 {
			weightSum += row.weight
		}
	}
	for j, row := range q.regionalWeights {
		if row.weight <= 0 {
			continue
		}
		if row.key == q.fish {
			target = j
		}
		share := 0.0
		if weightSum > 0 {
			share = row.weight / weightSum
		}
		factors, err := g.distributionFactors(ctx, row.key, share)
		if err != nil {
			return nil, err
		}
		vectors[j] = factors
	}
	if target < 0 || q.gradeShare <= 0 {
		return nil, nil
	}
	best := -1
	for i := range g.areas {
		if i%64 == 0 && ctx.Err() != nil {
			return nil, ctx.Err()
		}
		targetWeight := q.regionalWeights[target].weight * vectors[target][i]
		if targetWeight <= 0 {
			continue
		}
		var total, successful float64
		for j, row := range q.regionalWeights {
			if row.weight > 0 {
				weight := row.weight * vectors[j][i]
				total += weight
				successful += weight * row.acceptance
			}
		}
		selection := targetWeight / total
		chance := q.biteProbability * selection * q.acceptance * q.gradeShare
		if chance > q.chance {
			best, q.chance, q.selection = i, chance, selection
			q.fishShare = targetWeight * q.acceptance / successful
		}
	}
	if best < 0 {
		return nil, nil
	}
	region := g.bounds[best]
	return &region, nil
}
