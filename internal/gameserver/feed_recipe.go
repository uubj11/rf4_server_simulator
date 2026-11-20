package gameserver

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"rf4server/internal/proto"
	"rf4server/internal/store"
	"rf4server/internal/wiregen"
)

// 同一投影用于库存读取和制作事务，事务内必须通过 tx 查询。
type feedRecipeQuery interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func validFeedArray(v *proto.Value, code string, element uint32) bool {
	if v == nil || v.Kind != proto.KindArgs || v.Code == nil || *v.Code != code {
		return false
	}
	for _, item := range v.Items {
		if item == nil || item.Kind != proto.KindObj || item.TypeID != element {
			return false
		}
	}
	return true
}

func feedRecipeMaterials(ctx context.Context, db feedRecipeQuery, guid string) ([]string, error) {
	var raw string
	err := db.QueryRowContext(ctx, `SELECT mats_json FROM groundbait_recipe WHERE instance_guid=?`, guid).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var mats []string
	if err := json.Unmarshal([]byte(raw), &mats); err != nil {
		return nil, err
	}
	return mats, nil
}

// 客户端按 $mix/$arm/$add 读取 f0/f1/f2，数组注册码由 4.0.25034
// 的类型注册表核实；连空配方也必须提供三个有类型的数组。
func projectFeedIngredients(ctx context.Context, db feedRecipeQuery, feed *proto.Value, mats []string) error {
	arrays := []*proto.Value{proto.Args(proto.S("50")), proto.Args(proto.S("52")), proto.Args(proto.S("51"))}
	seen := make(map[string]bool)
	for _, id := range mats {
		if seen[id] {
			continue
		}
		seen[id] = true
		var cat int64
		err := db.QueryRowContext(ctx, `SELECT category FROM item_def WHERE item_id=?`, id).Scan(&cat)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return err
		}
		var idx int
		var elem *proto.Value
		switch cat {
		case 18:
			idx, elem = 0, proto.Obj("gfkbinccond", 121, proto.FStr("STR", proto.S(id)))
		case 20:
			idx, elem = 1, proto.Obj("kiignikjoog", 123, proto.FStr("STR", proto.S(id)))
		case 45, catGroundbaitMix, catPVAStick:
			// 网袋与仅有定义名的旧干混合记录不能充当原料配方。
			continue
		default:
			idx, elem = 2, proto.Obj("kcepjnmgbkf", 122,
				proto.FStr("STR", proto.S(id)), proto.FStr("STR", proto.S("")))
		}
		arrays[idx].Items = append(arrays[idx].Items, elem)
	}
	for idx, array := range arrays {
		if !validFeedArray(feed.Fields[idx].Val, *array.Code, []uint32{121, 123, 122}[idx]) {
			feed.Fields[idx].Val = array
		}
	}
	return nil
}

// 无干混合实例的 PVA（贩卖机或旧存档）只从本身的用料恢复配方。
// 不按 item_id 猜测另一个玩家实例，也不生成新的库存行。
func (s *Server) newFeedRecipe(ctx context.Context, db feedRecipeQuery, mats []string) (*proto.Value, error) {
	def := &store.ItemDef{ItemID: "feed_dry", Category: catGroundbaitMix}
	err := db.QueryRowContext(ctx, `SELECT numeric_id, attrs_json FROM item_def WHERE item_id='feed_dry'`).
		Scan(&def.NumericID, &def.AttrsJSON)
	if err != nil {
		return nil, err
	}
	feed, err := wiregen.MakeObj("cilmgoaoofg", nil)
	if err != nil {
		return nil, err
	}
	s.applyDefAttrs(def, feed.Class, feed)
	feed.Fields[fieldIdxByCS(feed.Class, csNumericID)].Num = ptrU64(uint64(def.NumericID))
	if err := projectFeedIngredients(ctx, db, feed, mats); err != nil {
		return nil, err
	}
	return feed, nil
}

func (s *Server) projectFeedRecipe(ctx context.Context, db feedRecipeQuery, obj *proto.Value, guid string) error {
	if obj.TypeID != 120 && obj.TypeID != 145 {
		return nil
	}
	if obj.TypeID == 120 && validFeedArray(obj.Fields[0].Val, "50", 121) &&
		validFeedArray(obj.Fields[1].Val, "52", 123) && validFeedArray(obj.Fields[2].Val, "51", 122) {
		return nil
	}
	if obj.TypeID == 145 {
		if feed := obj.Fields[1].Val; feed != nil && feed.Kind == proto.KindObj && feed.TypeID == 120 {
			return s.projectFeedRecipe(ctx, db, feed, feed.Fields[11].Hex)
		}
	}
	mats, err := feedRecipeMaterials(ctx, db, guid)
	if err != nil {
		return err
	}
	if obj.TypeID == 120 {
		return projectFeedIngredients(ctx, db, obj, mats)
	}
	obj.Fields[1].Val, err = s.newFeedRecipe(ctx, db, mats)
	return err
}

func (s *Server) captureFeedRecipeTx(ctx context.Context, tx *sql.Tx, row *store.Item) (*proto.Value, error) {
	fields, err := FieldsFromJSON(row.FieldsJSON)
	if err != nil {
		return nil, err
	}
	feed := proto.Obj(row.WireClass, uint32(row.TypeID), alignFieldsToOps(row, fields)...)
	if err := s.projectFeedRecipe(ctx, tx, feed, row.GUID); err != nil {
		return nil, err
	}
	return feed, nil
}

func (s *Server) savePVARecipeTx(ctx context.Context, tx *sql.Tx, row *store.Item, feed *proto.Value, mats []string) error {
	if row.TypeID != 145 {
		return nil
	}
	var err error
	if feed == nil {
		feed, err = s.newFeedRecipe(ctx, tx, mats)
		if err != nil {
			return err
		}
	}
	fields, err := FieldsFromJSON(row.FieldsJSON)
	if err != nil {
		return err
	}
	if len(fields) < 2 {
		return fmt.Errorf("PVA 物品缺少配方字段")
	}
	fields[1].Val = feed
	row.FieldsJSON, err = FieldsToJSON(fields)
	return err
}
