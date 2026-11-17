package gameserver

// 切鱼（craft_bait_cut_fish）：把一条鱼护渔获切成鱼肉块。
// 上游配方 materials_json 为空，服务端此前对零材料配方一律放行，导致
// 「切鱼不消耗任何鱼、每次固定 30 块」；这里补上渔获消耗与按重量出块。

import (
	"context"

	"rf4server/internal/store"
)

// cutFishRecipeID 切鱼配方 id。
const cutFishRecipeID = "craft_bait_cut_fish"

// fishPieceCountForWeight 按整鱼出肉率连续计算可切出的鱼块数量，不再分档。
//
// 模型：鱼肉块每块 7g；整鱼可切出的鱼肉占体重 28%（小鱼，头骨内脏占比高）
// 随体重线性升到 45%（25kg 及以上，大鱼骨肉比更优），即
//
//	pieces = round(weightG * ratio / 7)，ratio = 28‰ + 170‰ * min(weightG,25000)/25000
//
// 全程整数运算，避免浮点误差；重量越大块数越多且严格单调不减，
// 20.9kg（本档历史最大渔获）约出 1260 块（约 8.8kg 鱼肉），不会出现
// 「一条大鱼只切出几块」。
func fishPieceCountForWeight(weightG int64) int64 {
	if weightG <= 0 {
		return 1
	}
	// 出肉率（千分比）：280‰ → 450‰，按 25kg 线性升到顶。
	permille := int64(280)
	if weightG >= 25000 {
		permille = 450
	} else {
		permille = 280 + weightG*170/25000
	}
	// 每块 7g：pieces = weightG * permille / 1000 / 7，四舍五入。
	pieces := (weightG*permille + 3500) / 7000
	if pieces < 1 {
		return 1
	}
	return pieces
}

// resolveCutFishEntry 事务外挑选本次要切的鱼：
// 材料槽里指向本 uid 鱼护条目的 GUID 优先（玩家拖进来的那条），
// 否则取鱼护里 ts 最小（最旧）的一条，避免默认吃掉刚钓到的大鱼。
// 同 uid 之外的条目一律不采用。没有可用鱼返回 nil。
func (s *Server) resolveCutFishEntry(ctx context.Context, uid int64, slots []craftSlot) *store.KeepnetEntry {
	for _, sl := range slots {
		if sl.GUID == "" {
			continue
		}
		e, err := s.DB.KeepnetGet(ctx, uid, sl.GUID)
		if err != nil || e == nil {
			continue
		}
		return e
	}
	list, err := s.DB.KeepnetList(ctx, uid)
	if err != nil || len(list) == 0 {
		return nil
	}
	return list[0]
}
