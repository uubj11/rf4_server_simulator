package gameserver

import (
	"context"
	"database/sql"
	"math"
)

// growthDurabilityFloor 是成长套装件的耐久下限：磨损到底也保留可用性，永不彻底损坏。
const growthDurabilityFloor = 0.10

// 成长件单次渔获的回复口径（2026-11-16 在原有 0.002/kg 之上加「每尾保底 1%」）。
//
// 旧口径下 165 g 的小鱼只回 0.00033，抵掉 0.0002 竿磨损后净 +0.00013，界面取整后
// 损耗百分比一动不动，玩家会误判"钓鱼修不回来"。加保底后每尾至少回 1%，界面看得见；
// 大鱼仍按重量线性拉开差距（50 kg 才吃满 10% 封顶），养套装的主力依然是大鱼。
const (
	growthHealPerKg = 0.002 // 每公斤鱼重回复的耐久（原 A 方案口径，未改）
	growthHealFloor = 0.01  // 单尾最少回复量（保底 1%）
	growthHealCap   = 0.10  // 单尾最多回复量（封顶 10%）
)

// growthDurabilityAfterCatch 计算一次渔获后的耐久。
//
// 普通件照旧线性磨损到 0；成长件按钓上鱼的大小回复
// clamp(鱼重kg × growthHealPerKg, growthHealFloor, growthHealCap)，
// 结果不低于 growthDurabilityFloor、不高于 1.0。小鱼靠 1% 保底逐尾回血，
// 大鱼一尾就能吃掉一成磨损。
func growthDurabilityAfterCatch(before, wear, fishWeightKg float64, growth bool) float64 {
	if !growth {
		return math.Max(0, before-wear)
	}
	regain := math.Min(growthHealCap, math.Max(growthHealFloor, math.Max(0, fishWeightKg)*growthHealPerKg))
	d := before - wear + regain
	if d < growthDurabilityFloor {
		d = growthDurabilityFloor
	}
	if d > 1 {
		d = 1
	}
	return d
}

// durabilityChanged 判断耐久是否真的变了。浮点比较留 1e-9 容差：
// 成长件满耐久钓小鱼时磨损会被回复盖过并钳回 1.0，算出来与当前值完全相同，
// 这种「无变化」不能当成写入失败（setWireFloat 对无变化同样返回 false）。
func durabilityChanged(before, after float64) bool {
	return math.Abs(after-before) > 1e-9
}

// isGrowthItem 判断某个物品实例是否登记为成长套装件。
func (s *Server) isGrowthItem(ctx context.Context, guid string) bool {
	if s == nil || s.DB == nil || guid == "" {
		return false
	}
	it, err := s.DB.GrowthItemByGUID(ctx, guid)
	return err == nil && it != nil
}

// tunableGrowthItemIndestructible 是「成长套装件抗硬报废」开关：
// 1=开（默认，成长件断竿只压耐久到下限）；0=完全回到官服口径（耐久归零、组件销毁）。
// 放在 tunable 里是为了实机发现客户端不接受「10% 的断竿」时能一键回退，不用重编。
const tunableGrowthItemIndestructible = "growth_item_indestructible"

// growthBreakSparedTx 判断本次 17/9 硬报废是否应被成长套装豁免。
// 只对 growth_item 登记件生效；必须查询已开的写事务——sqlite 写锁已被
// 本事务占着，另开连接查 s.DB 会阻塞到 context 超时（实测 5 s 断件失败）。
func (s *Server) growthBreakSparedTx(ctx context.Context, tx *sql.Tx, guid string) bool {
	if s == nil || tx == nil || guid == "" {
		return false
	}
	if s.tunableF(tunableGrowthItemIndestructible, 1) <= 0 {
		return false
	}
	var one int
	err := tx.QueryRowContext(ctx, `SELECT 1 FROM growth_item WHERE guid=? LIMIT 1`, guid).Scan(&one)
	return err == nil && one == 1
}
