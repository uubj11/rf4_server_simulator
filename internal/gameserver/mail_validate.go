package gameserver

import (
	"context"
	"fmt"
	"math"
	"strings"

	"rf4server/internal/store"
)

// ValidateMailAttachment 在后台发件前对单个附件做一次干跑校验。
//
// 用途（见 specs/changes/mail-claim-visible-failure）：后台过去能发出
// "永远领不出来"的邮件。发件校验不能只看字符串形式，必须按领取路径的
// 真实规则验证——物品是否存在、组合包内含物是否能展开、数量是否越界。
//
// 这里复用 prepareBundleItems / bundleOpenable 的判据，但不构造物品对象，
// 因此是纯读、可安全地在 HTTP 请求里调用。
// 返回 nil 表示该附件在领取路径上可以发放。
func ValidateMailAttachment(ctx context.Context, db *store.DB, att store.MailItem) error {
	if db == nil {
		return fmt.Errorf("数据库不可用")
	}
	itemID := strings.TrimSpace(att.ItemID)
	if itemID == "" {
		return fmt.Errorf("附件缺少物品 ID")
	}
	if att.Qty <= 0 {
		return fmt.Errorf("附件 %s 数量必须大于 0", itemID)
	}
	def, err := db.GetItemDefByItemID(ctx, itemID)
	if err != nil || def == nil {
		return fmt.Errorf("附件 %s 物品不存在", itemID)
	}
	// 梦鱼券按张建行，领取侧 prepareCouponRows 有单次上限。发件侧必须先挡住，
	// 否则后台能发出「领取必然失败」的券邮件；与后台直接发放共用同一判据。
	if err := couponGrantQtyError(def, itemID, att.Qty); err != nil {
		return err
	}
	if def.Category != bundleCategory {
		return nil
	}
	if bundleOpenable(def) {
		// 礼盒：按包数逐件发放本体，一包一件。
		if att.Qty > maxBundlePreparedItems {
			return fmt.Errorf("附件 %s 数量 %d 超过单次领取上限 %d，玩家无法领取",
				itemID, att.Qty, maxBundlePreparedItems)
		}
		return nil
	}
	// 饵料盒：按内含物展开，需要真正验证每个内含物能否构造。
	packs := att.Qty
	if packs < 1 {
		packs = 1
	}
	if err := validateBundleExpandable(ctx, db, itemID, packs); err != nil {
		return fmt.Errorf("附件 %s 无法展开: %w", itemID, err)
	}
	return nil
}

// couponGrantQtyError 校验单次发放的券张数；非券类恒返回 nil。
// 邮件附件与后台 grant_item 共用，避免发件侧/领取侧口径漂移。
func couponGrantQtyError(def *store.ItemDef, itemID string, qty int64) error {
	if def.Category == couponCategory && qty > maxBundlePreparedItems {
		return fmt.Errorf("附件 %s 数量 %d 超过单次领取上限 %d，玩家无法领取",
			itemID, qty, maxBundlePreparedItems)
	}
	return nil
}

// ValidateCouponGrantQty 校验后台直接发放（grant_item）的券张数。
// 物品定义缺失时交给领取侧处理，这里只挡「券张数越界」这一件事。
func ValidateCouponGrantQty(ctx context.Context, db *store.DB, itemID string, qty int64) error {
	if db == nil {
		return fmt.Errorf("数据库不可用")
	}
	itemID = strings.TrimSpace(itemID)
	def, err := db.GetItemDefByItemID(ctx, itemID)
	if err != nil || def == nil {
		return nil
	}
	return couponGrantQtyError(def, itemID, qty)
}

// validateBundleExpandable 按 prepareBundleItems 的判据做只读校验：
// 内含物定义存在、不是嵌套组合包、类别可映射、数量合法、实例数不超上限。
func validateBundleExpandable(ctx context.Context, db *store.DB, itemID string, packs int64) error {
	def, err := db.GetItemDefByItemID(ctx, itemID)
	if err != nil || def == nil {
		return fmt.Errorf("组合包定义不存在: %s", itemID)
	}
	if def.Category != bundleCategory {
		return fmt.Errorf("不是组合包定义: %s", itemID)
	}
	attrs, err := bundleAttrs(def)
	if err != nil {
		return err
	}
	contents, ok := attrs["nbkhfhenbag"].([]any)
	if !ok || len(contents) == 0 {
		return fmt.Errorf("组合包没有内含物: %s", itemID)
	}
	instances := int64(0)
	totals := make(map[string]int64)
	for _, raw := range contents {
		entry, _ := raw.(map[string]any)
		inner, _ := entry["cemjmlpafmc"].(string)
		if strings.TrimSpace(inner) == "" {
			return fmt.Errorf("组合包内含物缺少 item_id: %s", itemID)
		}
		quantity, qerr := bundlePositiveQuantity(entry[csQty])
		if qerr != nil {
			return fmt.Errorf("内含物 %s 数量非法: %w", inner, qerr)
		}
		if quantity > math.MaxInt64/packs {
			return fmt.Errorf("内含物 %s 数量溢出", inner)
		}
		child, cerr := db.GetItemDefByItemID(ctx, inner)
		if cerr != nil || child == nil {
			// 这一条正是审查复现的问题：内含物不存在时领取必然失败。
			return fmt.Errorf("内含物物品不存在: %s", inner)
		}
		if child.Category == bundleCategory {
			return fmt.Errorf("不支持嵌套组合包: %s -> %s", itemID, inner)
		}
		class, ok := resolveWireClass(child)
		if !ok {
			return fmt.Errorf("内含物类别尚不支持: %s: %d", inner, child.Category)
		}
		units := quantity * packs
		count := units
		if countableCategories[child.Category] {
			count = 1
		}
		if count > maxBundlePreparedItems-instances {
			return fmt.Errorf("内含物实例数超过上限 %d: %s", maxBundlePreparedItems, inner)
		}
		instances += count

		if !countableCategories[child.Category] {
			continue
		}
		if itemQtyFieldIdx(class.Class) < 0 {
			return fmt.Errorf("组合包计件内含物缺少数量字段: %s", inner)
		}
		limit := bundleQuantityLimit(class.Class)
		childAttrs, aerr := bundleAttrs(child)
		if aerr != nil {
			return aerr
		}
		base := int64(1)
		if rawBase, exists := childAttrs[csQty]; exists {
			base, aerr = bundlePositiveQuantity(rawBase)
			if aerr != nil {
				return fmt.Errorf("组合包内含物单位量非法: %s: %w", inner, aerr)
			}
		}
		absolute, _ := entry[csQtyAbsolute].(bool)
		var total int64
		if absolute {
			// 绝对个数条目：units 即最终件数，不乘子物定义默认量。
			total = units
			if total > limit {
				return fmt.Errorf("组合包内含物数量超过数量槽上限: %s", inner)
			}
		} else {
			if base > limit/units {
				return fmt.Errorf("组合包内含物单位换算溢出: %s", inner)
			}
			total = base * units
		}
		if totals[inner] > limit-total {
			return fmt.Errorf("组合包内含物总量溢出: %s", inner)
		}
		totals[inner] += total
	}
	return nil
}
