package gameserver

import "math"

// 出鱼重量下限（"发鱼保底"）。
//
// 各鱼档的重量带由结算尺切分，未达标档的下界历史上是 1g，所以会发出
// 虹鳟 22g/10cm、湖鳟 26g 这种"鱼苗"。这里给一个可调下限：
// 任何个体都不低于 Record（达标线）的 spawn_min_ratio 倍。
//
// ratio=0 关闭本服加严，下界回到官服口径（min_g 优先，缺失时 15% 钳 [1,50] g）；
// ratio=1 表示未达标档整体消失。
// 只影响重量采样，不改档位判定与上鱼率手感。
const (
	tunableSpawnMinRatio = "spawn_min_ratio"
	defSpawnMinRatio     = 0.6
)

// spawnFloorG 返回该鱼种的重量下限（克）。达标线缺失或关闭时返回 1。
func (s *Server) spawnFloorG(qualifiedG float64) float64 {
	if qualifiedG <= 0 {
		return 1
	}
	ratio := s.tunableF(tunableSpawnMinRatio, defSpawnMinRatio)
	if ratio <= 0 {
		return 1
	}
	floor := math.Round(qualifiedG * ratio)
	if floor < 1 {
		return 1
	}
	return floor
}
