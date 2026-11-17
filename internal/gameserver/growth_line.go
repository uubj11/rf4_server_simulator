package gameserver

import (
	"context"
)

// syncGrowthLineLength 把某套成长套装的主线补到当前等级应有的长度（单位米）。
//
// 鱼线长度是物品实例字段（fields_json 的 csQty 槽 = ljnnpokapig），不是定义值：
// 静态定义里 braid_15048 只有 110 米，装进 Venga SBH 10000（0.48 毫米 / 502 米）
// 只填了 22%，抛投距离与可放线量都被这条线卡死。等级换算见 growthSetSpec.lineLengthAt。
//
// 规则只补不削：
//   - 当前长度已 ≥ 目标（换了一盘更长的线、或还没被扣过线），原样不动；
//   - 低于目标才补到目标，断线扣掉的米数在下一尾鱼结算/升级时补回来，
//     成长套装的主线不会因为一次断线永久废掉。
//
// 只处理登记为成长件的主线；非成长线、没登记、该套线长不参与成长的都直接跳过。
func (s *Server) syncGrowthLineLength(ctx context.Context, uid int64, setKey string, level int64) error {
	if s == nil || s.DB == nil || setKey == "" {
		return nil
	}
	spec, ok := growthSetSpecFor(setKey)
	if !ok {
		return nil
	}
	target := spec.lineLengthAt(level)
	if target <= 0 {
		return nil
	}
	roles, err := s.DB.GrowthItemRoles(ctx, uid, setKey)
	if err != nil {
		return err
	}
	it, ok := roles[growthRoleLine]
	if !ok || it.GUID == "" {
		return nil
	}
	row, err := s.DB.GetItemByGUID(ctx, it.GUID)
	if err != nil || row == nil {
		return err
	}
	fields, err := FieldsFromJSON(row.FieldsJSON)
	if err != nil {
		return err
	}
	idx := fieldIdxByCS(row.WireClass, csQty)
	if idx < 0 || idx >= len(fields) {
		return nil
	}
	if int64(u64Of(fields[idx].Num)) >= target {
		return nil
	}
	if !setItemLengthField(row, fields, target) {
		return nil
	}
	encoded, err := FieldsToJSON(fields)
	if err != nil {
		return err
	}
	return s.DB.UpdateItemFields(ctx, row.GUID, encoded, row.Qty, row.Durability, row.Workmanship, row.RigGUID)
}
