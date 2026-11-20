package gameserver

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"rf4server/internal/store"
)

// slotBaitRefill 保留实际挂载槽位，包括食笼内的窝料。
// 自制窝料同时匹配配方，不能只按共用的物品定义替换。
type slotBaitRefill struct {
	Owner     string  `json:"owner"`
	NumericID int64   `json:"numeric_id"`
	Recipe    string  `json:"recipe"`
	Slot      rigComp `json:"slot"`
	RigType   int64   `json:"rig_type"`
	Hook      string  `json:"hook,omitempty"`
	Lure      string  `json:"lure,omitempty"`
	Feeder    string  `json:"feeder,omitempty"`
}

func rememberBaitRefillTx(ctx context.Context, tx *sql.Tx, rod, bait *store.Item, slots map[int64]rigComp) error {
	owner := rod
	if bait.RigGUID != rod.GUID {
		var err error
		owner, err = getOwnedItemTx(ctx, tx, rod.UID, bait.RigGUID)
		if err != nil {
			return err
		}
		fields, err := FieldsFromJSON(owner.FieldsJSON)
		if err != nil {
			return err
		}
		slots = rigCompsOf(protoFieldAt(fields, owner.WireClass, csRigObject))
	}
	_, slot, ok := rigCompKeyForGUID(slots, bait.GUID)
	if !ok {
		return fmt.Errorf("消耗饵料缺少装配槽位")
	}
	fields, err := FieldsFromJSON(owner.FieldsJSON)
	if err != nil {
		return err
	}
	_, rt, _ := rigIdentity(protoFieldAt(fields, owner.WireClass, csRigObject))
	r := slotBaitRefill{Owner: owner.GUID, NumericID: bait.NumericID, Slot: slot, RigType: rt}
	if slot.Category == catGroundbaitMix {
		if feeders := rigCompKeysOfCategory(slots, 31); len(feeders) == 1 {
			r.Feeder = slots[feeders[0]].GUID
		}
	}
	if err := tx.QueryRowContext(ctx, `SELECT mats_json FROM groundbait_recipe WHERE instance_guid=?`, bait.GUID).Scan(&r.Recipe); err != nil && err != sql.ErrNoRows {
		return err
	}
	if slot.Category == catBait {
		// 先保留已选钩位；没有记录时只推断唯一直属钩或唯一拟饵的活动钩。
		var selected string
		if err := tx.QueryRowContext(ctx, `SELECT value FROM rod_setting WHERE guid=? AND key=?`, rod.GUID, "bait_hook:"+bait.GUID).Scan(&selected); err != nil && err != sql.ErrNoRows {
			return err
		}
		hooks := rigCompKeysOfCategory(slots, catHook)
		for _, k := range hooks {
			if slots[k].GUID == selected || selected == "" && len(hooks) == 1 {
				r.Hook = slots[k].GUID
				break
			}
		}
		for _, k := range rigCompKeysOfCategory(slots, catLure) {
			if r.Hook != "" && r.Lure == "" {
				break
			}
			lure, err := getOwnedItemTx(ctx, tx, rod.UID, slots[k].GUID)
			if err != nil {
				return err
			}
			lf, err := FieldsFromJSON(lure.FieldsJSON)
			if err != nil {
				return err
			}
			if len(lf) == 0 || lf[0].Val == nil {
				continue
			}
			items := lf[0].Val.Items
			hook := lureHookItemGUID(items, lureActiveOrdinal(lf, len(items)))
			if hook == "" {
				for i := range items {
					if hook = lureHookItemGUID(items, i); hook != "" {
						break
					}
				}
			}
			if hook != "" && (selected == "" || selected == hook) {
				if r.Hook != "" {
					return fmt.Errorf("无法唯一确定拟饵补饵钩位")
				}
				r.Hook, r.Lure = hook, lure.GUID
			}
		}
		if r.Hook == "" {
			return nil
		}
	}
	value, err := json.Marshal(r)
	if err != nil {
		return err
	}
	key := fmt.Sprintf("refill:%s:%d:%d", owner.GUID, slot.Category, slot.Extra)
	if _, err = tx.ExecContext(ctx, `INSERT INTO rod_setting(guid,key,value,updated_ts) VALUES(?,?,?,?) ON CONFLICT(guid,key) DO UPDATE SET value=excluded.value,updated_ts=excluded.updated_ts`, rod.GUID, key, string(value), float64(time.Now().Unix())); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `DELETE FROM rod_setting WHERE guid=? AND key=?`, rod.GUID, "bait_hook:"+bait.GUID)
	return err
}

// reloadSlotBait 在同一事务提交库存转移、f5 投影与补充记录。
// 任一写入失败全部回滚，不发送部分更新。
func (s *Server) reloadSlotBait(ctx context.Context, uid int64, rodGUID string) (baitChange, error) {
	var out baitChange
	tx, err := beginEconomyTx(ctx, s.DB)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	rod, err := getOwnedItemTx(ctx, tx, uid, rodGUID)
	if err == sql.ErrNoRows {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT key,value FROM rod_setting WHERE guid=? AND key LIKE 'refill:%' ORDER BY key`, rodGUID)
	if err != nil {
		return out, err
	}
	type pending struct{ key, value string }
	var requests []pending
	for rows.Next() {
		var p pending
		if err := rows.Scan(&p.key, &p.value); err != nil {
			rows.Close()
			return out, err
		}
		requests = append(requests, p)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	for _, p := range requests {
		var r slotBaitRefill
		if err := json.Unmarshal([]byte(p.value), &r); err != nil {
			return baitChange{}, err
		}
		clear := func() error {
			_, err := tx.ExecContext(ctx, `DELETE FROM rod_setting WHERE guid=? AND key=?`, rodGUID, p.key)
			return err
		}
		owner, err := getOwnedItemTx(ctx, tx, uid, r.Owner)
		if err == sql.ErrNoRows {
			if err := clear(); err != nil {
				return baitChange{}, err
			}
			continue
		}
		if err != nil {
			return baitChange{}, err
		}
		// 已拆下的食笼不能再替原竿补料。
		ancestor := owner
		seen := map[string]bool{}
		for ancestor.GUID != rodGUID && ancestor.RigGUID != "" && !seen[ancestor.GUID] {
			seen[ancestor.GUID] = true
			ancestor, err = getOwnedItemTx(ctx, tx, uid, ancestor.RigGUID)
			if err != nil {
				return baitChange{}, err
			}
		}
		if ancestor.GUID != rodGUID || rod.Durability <= 0 {
			if err := clear(); err != nil {
				return baitChange{}, err
			}
			continue
		}
		fields, err := FieldsFromJSON(owner.FieldsJSON)
		if err != nil {
			return baitChange{}, err
		}
		idx := fieldIdxByCS(owner.WireClass, csRigObject)
		if idx < 0 || idx >= len(fields) || fields[idx].Val == nil {
			return baitChange{}, fmt.Errorf("补饵父对象缺少钓组")
		}
		f5 := fields[idx].Val
		name, rt, _ := rigIdentity(f5)
		comps := rigCompsOf(f5)
		occupied := false
		for _, c := range comps {
			if r.Slot.Extra > 0 && c.Extra == r.Slot.Extra || r.Slot.Extra == 0 && c.Category == r.Slot.Category && c.Ordinal == r.Slot.Ordinal {
				occupied = true
			}
		}
		validHook := r.Hook == ""
		if r.Hook != "" && r.Lure == "" {
			for _, k := range rigCompKeysOfCategory(comps, catHook) {
				if comps[k].GUID == r.Hook {
					validHook = true
				}
			}
		}
		if r.Lure != "" {
			if _, _, ok := rigCompKeyForGUID(comps, r.Lure); ok {
				lure, err := getOwnedItemTx(ctx, tx, uid, r.Lure)
				if err != nil {
					return baitChange{}, err
				}
				lf, err := FieldsFromJSON(lure.FieldsJSON)
				if err != nil {
					return baitChange{}, err
				}
				if len(lf) > 0 && lf[0].Val != nil {
					for _, hook := range lureHookItemGUIDs(lf[0].Val.Items) {
						if hook == r.Hook {
							validHook = true
						}
					}
				}
			}
		}
		validFeeder := true
		if r.Feeder != "" {
			_, _, validFeeder = rigCompKeyForGUID(comps, r.Feeder)
		}
		if occupied || rt != r.RigType || !validHook || !validFeeder {
			if err := clear(); err != nil {
				return baitChange{}, err
			}
			continue
		}
		source, err := scanTxItem(tx.QueryRowContext(ctx, `SELECT `+txItemCols+` FROM item WHERE uid=? AND numeric_id=? AND container='backpack' AND COALESCE(rig_guid,'')='' AND qty>0 AND COALESCE((SELECT mats_json FROM groundbait_recipe WHERE instance_guid=item.guid),'')=? ORDER BY created_ts,guid LIMIT 1`, uid, r.NumericID, r.Recipe))
		if err == sql.ErrNoRows {
			continue
		}
		if err != nil {
			return baitChange{}, err
		}
		if baitQuantity(source) <= 0 {
			return baitChange{}, fmt.Errorf("补饵来源数量无效")
		}
		child := *source
		child.GUID, child.Container, child.RigGUID, child.Slot, child.Qty = NewItemGUID(), "rig", owner.GUID, r.Slot.Category, 1
		child.CreatedTS = float64(time.Now().Unix())
		cf, err := FieldsFromJSON(source.FieldsJSON)
		if err != nil {
			return baitChange{}, err
		}
		setItemQty(child.WireClass, cf, 1)
		setWireParentFields(cf, child.WireClass, owner.GUID)
		if i := fieldIdxByCS(child.WireClass, csGUID); i >= 0 && i < len(cf) {
			cf[i].Hex = child.GUID
		}
		if i := fieldIdxByCS(child.WireClass, csSplitSource); i >= 0 && i < len(cf) {
			cf[i].Hex = ""
			if source.Qty > 1 {
				cf[i].Hex = source.GUID
			}
		}
		bumpCreatedTicks(cf, child.WireClass)
		child.FieldsJSON, err = FieldsToJSON(cf)
		if err != nil {
			return baitChange{}, err
		}
		if err := insertItemTx(ctx, tx, &child); err != nil {
			return baitChange{}, err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO groundbait_recipe(instance_guid,mats_json,created_ts) SELECT ?,mats_json,created_ts FROM groundbait_recipe WHERE instance_guid=?`, child.GUID, source.GUID); err != nil {
			return baitChange{}, err
		}
		if _, err := consumeItemQtyTx(ctx, tx, source, 1); err != nil {
			return baitChange{}, err
		}
		slot := r.Slot
		slot.GUID = child.GUID
		// 前面的组件消耗后数组可能紧缩，保留真实 Extra 槽位。
		for {
			if _, exists := comps[rigCompKey(slot.Category, slot.Ordinal)]; !exists {
				break
			}
			slot.Ordinal++
		}
		comps[rigCompKey(slot.Category, slot.Ordinal)] = slot
		fields[idx].Val = buildRig153Like(f5, name, rt, comps, rigDepthOf(f5, 100), rigClipOf(f5))
		if err := updateItemFieldsTx(ctx, s.DB, tx, owner, fields); err != nil {
			return baitChange{}, err
		}
		projection := rigProjection(owner.WireClass, fields, fields[idx].Val)
		if err := s.DB.ReplaceRigComponentsTx(ctx, tx, owner.GUID, projection); err != nil {
			return baitChange{}, err
		}
		if r.Hook != "" {
			if _, err := tx.ExecContext(ctx, `INSERT INTO rod_setting(guid,key,value,updated_ts) VALUES(?,?,?,?) ON CONFLICT(guid,key) DO UPDATE SET value=excluded.value`, rodGUID, "bait_hook:"+child.GUID, r.Hook, float64(time.Now().Unix())); err != nil {
				return baitChange{}, err
			}
		}
		if err := clear(); err != nil {
			return baitChange{}, err
		}
		out.Removed = append(out.Removed, source.GUID)
		out.Updated = append(out.Updated, rodGUID, owner.GUID, child.GUID, source.GUID)
	}
	if err := tx.Commit(); err != nil {
		return baitChange{}, err
	}
	return out, nil
}
