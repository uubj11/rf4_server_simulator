package gameserver

// 切鱼（craft_bait_cut_fish）：把鱼护里选中的那条鱼切成鱼肉块。
//
// 本服口径（0.9.67 起为唯一产量口径，优先于上游官服阶梯）：
// 按整鱼出肉率连续计算，不受档位/鱼种影响，重量越大块数越多且严格单调不减。

// fishPieceCountForWeight 按整鱼出肉率连续计算可切出的鱼块数量。
//
// 模型：鱼肉块每块 7g；整鱼可切出的鱼肉占体重 28%（小鱼，头骨内脏占比高）
// 随体重线性升到 45%（25kg 及以上，大鱼骨肉比更优），即
//
//	pieces = round(weightG * ratio / 7)，ratio = 28‰ + 170‰ * min(weightG,25000)/25000
//
// 全程整数运算，避免浮点误差；20.9kg（本档历史最大渔获）约出 1260 块（约 8.8kg
// 鱼肉），不会出现「一条大鱼只切出几块」。
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
