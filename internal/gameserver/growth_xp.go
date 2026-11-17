package gameserver

import (
	"context"
	"math"

	"rf4server/internal/store"
)

// 成长经验口径：按鱼重折算，1 kg = 100 点；升到下一级需要 3000×(当前等级+1) 点。
// 也就是说 25 kg 的鱼约值 2500 点，一条 30 kg 的鱼刚好把 0 级推到 1 级。
const (
	growthXPPerKg   = 100.0
	growthLevelBase = 3000.0
)

// growthXPForCatch 把渔获重量折算成套装的成长经验。
func growthXPForCatch(fishWeightKg float64) int64 {
	if fishWeightKg <= 0 {
		return 0
	}
	return int64(math.Round(fishWeightKg * growthXPPerKg))
}

// growthLevelFromXP 按累计经验反解等级，封顶在 cap。
// 每级门槛递增（3000、6000、9000…），所以越到后面越需要靠大鱼。
func growthLevelFromXP(xp int64, cap int64) int64 {
	if xp <= 0 || cap <= 0 {
		return 0
	}
	level := int64(0)
	for level < cap {
		cost := int64(growthLevelBase * float64(level+1))
		if xp < cost {
			break
		}
		xp -= cost
		level++
	}
	return level
}

// prepareGrowthXP 在结算事务开始之前算出本次渔获要让哪些套装涨多少级。
//
// 一个钓组里可能同时挂着多套的件（换装过但没卸干净），所以按 set_key 去重，
// 同一套在一次渔获里只涨一次经验。事务内只做写入，避免单连接 SQLite 等待自身。
func (s *Server) prepareGrowthXP(ctx context.Context, uid int64, rigGUID string, fishWeightKg float64) []store.GrowthSetState {
	if s == nil || s.DB == nil || rigGUID == "" {
		return nil
	}
	xpGain := growthXPForCatch(fishWeightKg)
	if xpGain <= 0 {
		return nil
	}
	components, err := s.DB.ListRigComponents(ctx, rigGUID)
	if err != nil {
		return nil
	}
	guids := make([]string, 0, len(components)+1)
	guids = append(guids, rigGUID)
	for _, c := range components {
		if c != nil && c.GUID != "" {
			guids = append(guids, c.GUID)
		}
	}
	seenGUID := map[string]bool{}
	seenSet := map[string]bool{}
	var out []store.GrowthSetState
	for _, guid := range guids {
		if guid == "" || seenGUID[guid] {
			continue
		}
		seenGUID[guid] = true
		it, gerr := s.DB.GrowthItemByGUID(ctx, guid)
		if gerr != nil || it == nil || it.UID != uid || seenSet[it.SetKey] {
			continue
		}
		spec, ok := growthSetSpecFor(it.SetKey)
		if !ok {
			continue
		}
		seenSet[it.SetKey] = true
		st, serr := s.DB.GrowthSetStateGet(ctx, uid, it.SetKey)
		if serr != nil {
			continue
		}
		st.XP += xpGain
		level := growthLevelFromXP(st.XP, spec.LevelCap)
		if level < st.Level {
			level = st.Level
		}
		st.Level = level
		out = append(out, st)
	}
	return out
}
