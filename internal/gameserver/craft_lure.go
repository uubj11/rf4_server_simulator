package gameserver

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"rf4server/internal/proto"
	"rf4server/internal/store"
)

type craftedLureHooks struct {
	positions []craftSlot
}

// Only these two slot names have request evidence. The third string is a
// model consistency check, never a replacement for inventory GUID ownership.
func prepareCraftedLureHooks(row *store.Item, def *store.ItemDef, materials []craftSlot) (*craftedLureHooks, []craftSlot, error) {
	var plan *craftedLureHooks
	if isLureRow(row) {
		cfgs, _ := defAttrs(def)[lureHooksAttr].([]any)
		if len(cfgs) > maxLureHookSlots {
			return nil, nil, fmt.Errorf("unknown crafted lure capacity")
		}
		plan = &craftedLureHooks{positions: make([]craftSlot, len(cfgs))}
	}
	ordinary := make([]craftSlot, 0, len(materials))
	selected := map[string]bool{}
	for _, slot := range materials {
		if !strings.HasPrefix(slot.ItemID, "plug_hook") {
			ordinary = append(ordinary, slot)
			continue
		}
		i := -1
		switch slot.ItemID {
		case "plug_hook":
			i = 0
		case "plug_hook_2":
			i = 1
		}
		if plan == nil || i < 0 || i >= len(plan.positions) || slot.GUID == "" ||
			plan.positions[i].GUID != "" || selected[slot.GUID] {
			return nil, nil, fmt.Errorf("invalid or duplicate crafting hook slot")
		}
		plan.positions[i] = slot
		selected[slot.GUID] = true
	}
	for _, slot := range ordinary {
		if selected[slot.GUID] {
			return nil, nil, fmt.Errorf("hook also used as crafting material")
		}
	}
	return plan, ordinary, nil
}

// Called inside the crafting transaction after ordinary material consumption.
// Failure to attach even one selected hook rolls back the entire craft.
func (s *Server) materializeCraftedLureTx(ctx context.Context, tx *sql.Tx, row *store.Item, plan *craftedLureHooks) ([]string, error) {
	fields, err := FieldsFromJSON(row.FieldsJSON)
	if err != nil {
		return nil, err
	}
	idx := lureHookFieldIndex(row.WireClass)
	if idx < 0 || idx >= len(fields) {
		return nil, fmt.Errorf("lure hook field missing")
	}
	list := proto.Args(proto.S(lureHookArgsCode))
	positions := make([]string, len(plan.positions))
	var attached []string
	for i, slot := range plan.positions {
		if slot.GUID == "" {
			continue
		}
		hook, err := s.DB.GetItemByGUIDTx(ctx, tx, slot.GUID)
		if err != nil || hook == nil {
			return nil, fmt.Errorf("selected hook unavailable: %s", slot.ItemID)
		}
		if slot.Species != "" && slot.Species != hook.ItemID {
			return nil, fmt.Errorf("selected hook model mismatch")
		}
		tree, err := s.attachLureHookTx(ctx, tx, row.UID, row.GUID, hook)
		if err != nil {
			return nil, err
		}
		list.Items = append(list.Items, tree)
		positions[i] = hook.GUID
		attached = append(attached, hook.GUID)
	}
	fields[idx] = proto.Field{Op: "VAL", Val: list}
	row.FieldsJSON, err = FieldsToJSON(fields)
	if err != nil {
		return nil, err
	}
	return attached, saveLureHookPositionsTx(ctx, tx, row.GUID, positions)
}
