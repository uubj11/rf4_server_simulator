package gameserver

import "context"

// growthMultiplierForItem 返回某物品实例在当前套装等级下的承力倍率。
// 不属于成长套装、没登记、等级为 0 时一律返回 1.0，调用方可以无条件相乘。
//
// 倍率按「该件自己的满级目标 / 该件起始值」线性插值，所以同等级时套装内每件
// 完成比例一致，满级同时到达各自目标，竿永远不会被配件拖成短板。
func (s *Server) growthMultiplierForItem(ctx context.Context, guid string) float64 {
	if s == nil || s.DB == nil || guid == "" {
		return 1.0
	}
	it, err := s.DB.GrowthItemByGUID(ctx, guid)
	if err != nil || it == nil {
		return 1.0
	}
	spec, ok := growthSetSpecFor(it.SetKey)
	if !ok {
		return 1.0
	}
	st, err := s.DB.GrowthSetStateGet(ctx, it.UID, it.SetKey)
	if err != nil || st.Level <= 0 {
		return 1.0
	}
	var base float64
	for _, m := range spec.Members {
		if m.Role == it.Role {
			base = m.BaseLoad
			break
		}
	}
	return growthItemMultiplier(spec.targetFor(it.Role), base, st.Level, spec.LevelCap)
}

// growthPartFloorForRod 给出「玩家自选末端件」的承力托底：
// 托底值 = 套装零件目标承力（TargetLoad × 1.05）× 等级进度（level/cap）。
//
// 路亚这类套装把钩和拟饵交给玩家自选（rig_Lure 的组件表没有独立钩槽，钩挂在拟饵上），
// 自选件不在 growth_item 里、拿不到自己的倍率。若不托底，一把低等级弱钩配满级竿，
// 钩就成了新的最弱环，"套装能钓大鱼"是假话。规则只托不削：
// 玩家自带的件强过托底时用他自己的数值，由 grownPartKg 保证。
//
// 只对承担力的角色生效；浮漂、铅、鱼饵返回 0。
func (s *Server) growthPartFloorForRod(ctx context.Context, rodGUID, role string) float64 {
	if s == nil || s.DB == nil || rodGUID == "" {
		return 0
	}
	switch role {
	case growthRoleHook, growthRoleSwivel, growthRoleLeader:
	default:
		return 0
	}
	it, err := s.DB.GrowthItemByGUID(ctx, rodGUID)
	if err != nil || it == nil || it.Role != growthRoleRod {
		return 0
	}
	spec, ok := growthSetSpecFor(it.SetKey)
	if !ok || spec.LevelCap <= 0 {
		return 0
	}
	st, err := s.DB.GrowthSetStateGet(ctx, it.UID, it.SetKey)
	if err != nil || st.Level <= 0 {
		return 0
	}
	level := st.Level
	if level > spec.LevelCap {
		level = spec.LevelCap
	}
	return spec.partTargetLoadKg() * (float64(level) / float64(spec.LevelCap))
}

// hookToleranceForRig 返回该竿的钩宽容进度（0~1）：只有挂在成长套装竿上的钩才放宽。
// 普通竿、未登记的竿、等级 0 一律返回 0，保证原版手感不变。
func (s *Server) hookToleranceForRig(ctx context.Context, uid int64, rodGUID string) float64 {
	if s == nil || s.DB == nil || uid == 0 || rodGUID == "" {
		return 0
	}
	it, err := s.DB.GrowthItemByGUID(ctx, rodGUID)
	if err != nil || it == nil || it.Role != growthRoleRod || it.UID != uid {
		return 0
	}
	spec, ok := growthSetSpecFor(it.SetKey)
	if !ok || spec.LevelCap <= 0 {
		return 0
	}
	st, err := s.DB.GrowthSetStateGet(ctx, it.UID, it.SetKey)
	if err != nil {
		return 0
	}
	// 0 级套装也有起步宽容度（hookToleranceBase），满级到 1.0。
	return growthToleranceLevel(growthToleranceProgress(st.Level, spec.LevelCap))
}

// grownPartKg 取「自身强度」与「套装托底」中较大的那个，保证升级只补不削。
func grownPartKg(baseKg, floorKg float64) float64 {
	if floorKg > baseKg {
		return floorKg
	}
	return baseKg
}

// growthPurchaseBlocked 判断玩家是否已持有同类型成长套装。
// 购买成功时礼盒本体就登记了归属，所以「买了没开箱」也算已拥有，一人一套从购买那一刻成立。
func (s *Server) growthPurchaseBlocked(ctx context.Context, uid int64, setKey string) (bool, error) {
	if s == nil || s.DB == nil || setKey == "" {
		return false, nil
	}
	owned, err := s.DB.GrowthSetKeysOwned(ctx, uid)
	if err != nil {
		return false, err
	}
	return owned[setKey], nil
}
