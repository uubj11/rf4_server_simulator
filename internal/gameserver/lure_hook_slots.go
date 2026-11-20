package gameserver

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"rf4server/internal/proto"
	"rf4server/internal/store"
)

// FieldsJSON stores installed hooks; this record preserves their slot identities.
const lureHookPositionsKey = "lure_hook_positions:"

func saveLureHookPositionsTx(ctx context.Context, tx *sql.Tx, guid string, positions []string) error {
	b, err := json.Marshal(positions)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO kv(k,v) VALUES(?,?) ON CONFLICT(k) DO UPDATE SET v=excluded.v`,
		lureHookPositionsKey+guid, string(b))
	return err
}

// Reconcile by GUID, not compact array index. Unknown legacy positions cannot
// be recovered; factory GUIDs carry an index, other legacy hooks use free slots.
func loadLureHookPositionsTx(ctx context.Context, tx *sql.Tx, lure *store.Item, items []*proto.Value) ([]string, error) {
	def, err := itemDefByNumericIDTx(ctx, tx, lure.NumericID)
	if err != nil {
		return nil, err
	}
	cfgs, _ := defAttrs(def)[lureHooksAttr].([]any)
	var raw string
	err = tx.QueryRowContext(ctx, `SELECT v FROM kv WHERE k=?`, lureHookPositionsKey+lure.GUID).Scan(&raw)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	return reconcileLureHookPositions(lure.GUID, len(cfgs), raw, items)
}

func (s *Server) loadLureHookPositions(ctx context.Context, lure *store.Item, items []*proto.Value) ([]string, error) {
	def, err := s.DB.GetItemDef(ctx, lure.NumericID)
	if err != nil {
		return nil, fmt.Errorf("lure definition unavailable: %w", err)
	}
	if def == nil {
		return nil, fmt.Errorf("lure definition missing")
	}
	cfgs, _ := defAttrs(def)[lureHooksAttr].([]any)
	raw, _, err := s.DB.KVGet(ctx, lureHookPositionsKey+lure.GUID)
	// Purchased and legacy lures may not have a position record yet.
	// Match the transactional reader: missing metadata uses factory positions.
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	return reconcileLureHookPositions(lure.GUID, len(cfgs), raw, items)
}

func reconcileLureHookPositions(lureGUID string, capacity int, raw string, items []*proto.Value) ([]string, error) {
	if capacity < 0 || capacity > maxLureHookSlots || len(items) > capacity {
		return nil, fmt.Errorf("invalid lure hook capacity")
	}
	positions := make([]string, capacity)
	if raw != "" {
		if err := json.Unmarshal([]byte(raw), &positions); err != nil {
			return nil, err
		}
		if len(positions) != capacity {
			return nil, fmt.Errorf("lure hook capacity changed")
		}
	}
	guids := lureHookItemGUIDs(items)
	if len(guids) != len(items) {
		return nil, fmt.Errorf("hook without GUID")
	}
	seen := map[string]bool{}
	for _, guid := range guids {
		if seen[guid] {
			return nil, fmt.Errorf("duplicate embedded hook")
		}
		seen[guid] = true
	}
	seen = map[string]bool{}
	for i, guid := range positions {
		if guid == "" || !slices.Contains(guids, guid) || seen[guid] {
			positions[i] = ""
			continue
		}
		seen[guid] = true
	}
	// Reserve known factory positions before assigning untracked player hooks.
	for _, guid := range guids {
		if i := lureOriginalFactorySlot(lureGUID, guid); !seen[guid] && i >= 0 && i < len(positions) && positions[i] == "" {
			positions[i], seen[guid] = guid, true
		}
	}
	for _, guid := range guids {
		if !seen[guid] {
			i := slices.Index(positions, "")
			if i < 0 {
				return nil, fmt.Errorf("no free hook position")
			}
			positions[i], seen[guid] = guid, true
		}
	}
	return positions, nil
}

// Convert a stable client slot to the compact storage index. An empty slot
// maps to append; an invalid request must never replace a different hook.
func lureHookStorageOrdinal(extra int64, positions []string, items []*proto.Value) int {
	if extra >= lureHookIndexBase {
		extra -= lureHookIndexBase
	}
	if extra < 0 || extra >= int64(len(positions)) {
		return -1
	}
	guid := positions[extra]
	if guid == "" {
		return len(items)
	}
	for i := range items {
		if lureHookItemGUID(items, i) == guid {
			return i
		}
	}
	return -1
}

func lureHookConfigs(def *store.ItemDef) ([]any, error) {
	if def == nil {
		return nil, fmt.Errorf("lure definition missing")
	}
	if def.Category != 17 {
		return nil, fmt.Errorf("lure definition category mismatch: %d", def.Category)
	}
	var attrs map[string]any
	if err := json.Unmarshal([]byte(def.AttrsJSON), &attrs); err != nil {
		return nil, fmt.Errorf("lure definition attributes invalid: %w", err)
	}
	raw, ok := attrs[lureHooksAttr]
	if !ok || raw == nil {
		return nil, nil
	}
	cfgs, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("lure hook configuration malformed")
	}
	return cfgs, nil
}

// Project slot positions only into outgoing trees. No nulls are written to
// FieldsJSON and no inventory items are created for empty positions.
func (s *Server) projectLureHookSlots(ctx context.Context, row *store.Item, obj *proto.Value) error {
	idx := lureHookFieldIndex(row.WireClass)
	if idx < 0 || idx >= len(obj.Fields) {
		return fmt.Errorf("lure hook field missing")
	}
	def, err := s.DB.GetItemDef(ctx, row.NumericID)
	if err != nil {
		return fmt.Errorf("lure definition unavailable: %w", err)
	}
	cfgs, err := lureHookConfigs(def)
	if err != nil {
		return err
	}
	list := obj.Fields[idx].Val
	if list == nil || list.Kind != proto.KindArgs {
		// 装饰性吸引成分（彩珠/八爪鱼/虾等）的 gpoaffnjbdo 出厂钩配置为 null，
		// 物化后 f0 保持 Null，没有钩位需要投影。这类拟饵是合法在售商品，
		// 必须发送空 args(60) 而不是 null；有钩定义或配置损坏仍要报错。
		if len(cfgs) == 0 {
			obj.Fields[idx].Val = proto.Args(proto.S(lureHookArgsCode))
			return nil
		}
		return fmt.Errorf("lure hook array missing")
	}
	positions, err := s.loadLureHookPositions(ctx, row, list.Items)
	if err != nil {
		return err
	}
	byGUID := make(map[string]*proto.Value, len(list.Items))
	for i, item := range list.Items {
		byGUID[lureHookItemGUID(list.Items, i)] = item
	}
	slots := make([]*proto.Value, len(positions))
	for i, guid := range positions {
		if guid == "" {
			slots[i] = proto.Null()
		} else {
			slots[i] = byGUID[guid]
		}
	}
	obj.Fields[idx].Val = proto.Args(proto.S(lureHookArgsCode), slots...)
	return nil
}

func replaceLureHookPosition(positions []string, oldGUID, newGUID string, extra int64) error {
	i := -1
	if oldGUID != "" {
		i = slices.Index(positions, oldGUID)
	} else {
		if extra >= lureHookIndexBase {
			extra -= lureHookIndexBase
		}
		if extra >= 0 && extra < int64(len(positions)) && positions[extra] == "" {
			i = int(extra)
		} else {
			i = slices.Index(positions, "")
		}
	}
	if i < 0 {
		return fmt.Errorf("hook position unavailable")
	}
	positions[i] = newGUID
	return nil
}
