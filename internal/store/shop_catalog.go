package store

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// shop_kind_defs.json 保存商店定义与 7 类基础货架数据。它只提供默认货架，
// 不替代玩家库中的门店货架或 shop_item_override 覆盖数据。
//
//go:embed assets/shop_kind_defs.json
var embeddedShopKindDefs []byte

// shop_houses.json 保存房产商店（housestore_*）的实抓货架。kind 7 没有真实
// 见证记录，目录模板误用了 farmstore 的农场货架，客户端打开买房界面拉取
// 22/1 时看不到房子商品，无法发起购买。房价与 22/5 面板（gameserver 侧
// shop_boats.json）保持一致，避免展示价与结算价不一致。
//
//go:embed assets/shop_houses.json
var embeddedShopHouses []byte

type shopCatalogSKU struct {
	ItemID string `json:"item_id"`
	Silver int64  `json:"silver"`
	Gold   int64  `json:"gold"`
	Stock  int64  `json:"stock"`
}

type shopStoreDefinition struct {
	Kind   int              `json:"kind"`
	Brands []string         `json:"brands"`
	MapKey string           `json:"map_key"`
	Books  []string         `json:"books"`
	Shelf  []shopCatalogSKU `json:"shelf,omitempty"`
}

type shopKindCatalog struct {
	Stores  map[string]shopStoreDefinition `json:"stores"`
	Kinds   map[string][]shopCatalogSKU    `json:"kinds"`
	Sources map[string]string              `json:"sources"`
}

type shopHouseRow struct {
	StoreID string `json:"store_id"`
	ItemID  string `json:"item_id"`
	Silver  int64  `json:"silver"`
	Gold    int64  `json:"gold"`
}

var (
	shopCatalogOnce sync.Once
	shopCatalogData *shopKindCatalog
	shopCatalogErr  error

	shopHousesOnce sync.Once
	shopHouseErr   error
	shopHouseShelf map[string][]*ShopPrice
)

func embeddedShopKindCatalog() (*shopKindCatalog, error) {
	shopCatalogOnce.Do(func() {
		var catalog shopKindCatalog
		if err := json.Unmarshal(embeddedShopKindDefs, &catalog); err != nil {
			shopCatalogErr = fmt.Errorf("解析内嵌商店目录: %w", err)
			return
		}
		if len(catalog.Stores) == 0 {
			shopCatalogErr = fmt.Errorf("内嵌商店目录没有 stores")
			return
		}
		shopCatalogData = &catalog
	})
	return shopCatalogData, shopCatalogErr
}

// embeddedShopHouseShelf 返回房产商店货架（store_id → 单行房子商品）。
func embeddedShopHouseShelf() (map[string][]*ShopPrice, error) {
	shopHousesOnce.Do(func() {
		var payload struct {
			Rows []shopHouseRow `json:"rows"`
		}
		if err := json.Unmarshal(embeddedShopHouses, &payload); err != nil {
			shopHouseErr = fmt.Errorf("解析内嵌房产货架: %w", err)
			return
		}
		shopHouseShelf = make(map[string][]*ShopPrice, len(payload.Rows))
		for _, row := range payload.Rows {
			if row.StoreID == "" || row.ItemID == "" {
				continue
			}
			shopHouseShelf[row.StoreID] = append(shopHouseShelf[row.StoreID], &ShopPrice{
				ItemID: row.ItemID,
				Silver: row.Silver,
				Gold:   row.Gold,
				Stock:  9999,
			})
		}
	})
	return shopHouseShelf, shopHouseErr
}

type shopOverrideValue struct {
	Silver sql.NullInt64
	Gold   sql.NullInt64
	Stock  sql.NullInt64
}

const (
	shopShelfSourceRecorded = "recorded"
	shopShelfSourceStore    = "store_definition"
	shopShelfSourceBrand    = "brand_reuse"
	shopShelfSourceKind     = "kind_template"
	shopShelfSourceMissing  = "missing_evidence"
	shopShelfSourceUnknown  = "unknown_store"
	shopShelfSourceHouse    = "house_store"
)

type shopShelfResolution struct {
	Base         []*ShopPrice
	Effective    []*ShopPrice
	Overrides    map[string]shopOverrideValue
	Definition   shopStoreDefinition
	CatalogKnown bool
	Source       string
}

func (d *DB) loadRecordedShopShelves(ctx context.Context) (map[string][]*ShopPrice, error) {
	rows, err := d.SQL.QueryContext(ctx, `
		SELECT store_id, item_id, silver, gold, stock
		FROM shop_item ORDER BY store_id, item_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string][]*ShopPrice)
	for rows.Next() {
		var storeID string
		p := &ShopPrice{}
		if err := rows.Scan(&storeID, &p.ItemID, &p.Silver, &p.Gold, &p.Stock); err != nil {
			return nil, err
		}
		out[storeID] = append(out[storeID], p)
	}
	return out, rows.Err()
}

func (d *DB) loadShopOverrides(ctx context.Context, storeID string) (map[string]shopOverrideValue, error) {
	rows, err := d.SQL.QueryContext(ctx, `
		SELECT item_id, silver, gold, stock
		FROM shop_item_override WHERE store_id=?`, storeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]shopOverrideValue)
	for rows.Next() {
		var itemID string
		var v shopOverrideValue
		if err := rows.Scan(&itemID, &v.Silver, &v.Gold, &v.Stock); err != nil {
			return nil, err
		}
		out[itemID] = v
	}
	return out, rows.Err()
}

func (d *DB) loadAllShopOverrides(ctx context.Context) (map[string]map[string]shopOverrideValue, error) {
	rows, err := d.SQL.QueryContext(ctx, `
		SELECT store_id, item_id, silver, gold, stock FROM shop_item_override`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]map[string]shopOverrideValue)
	for rows.Next() {
		var storeID, itemID string
		var v shopOverrideValue
		if err := rows.Scan(&storeID, &itemID, &v.Silver, &v.Gold, &v.Stock); err != nil {
			return nil, err
		}
		m := out[storeID]
		if m == nil {
			m = make(map[string]shopOverrideValue)
			out[storeID] = m
		}
		m[itemID] = v
	}
	return out, rows.Err()
}

func shopBrandKey(brands []string) string {
	if len(brands) == 0 {
		return ""
	}
	cp := append([]string(nil), brands...)
	sort.Strings(cp)
	return strings.Join(cp, "\x00")
}

func copyShopPrices(rows []*ShopPrice) []*ShopPrice {
	out := make([]*ShopPrice, 0, len(rows))
	for _, row := range rows {
		if row == nil {
			continue
		}
		v := *row
		out = append(out, &v)
	}
	return out
}

func catalogShopPrices(rows []shopCatalogSKU) []*ShopPrice {
	out := make([]*ShopPrice, 0, len(rows))
	for _, row := range rows {
		out = append(out, &ShopPrice{
			ItemID: row.ItemID,
			Silver: row.Silver,
			Gold:   row.Gold,
			Stock:  row.Stock,
		})
	}
	return out
}

// resolveShopBase 优先保留逐店实录及其价格，仅用明确登记的逐店目录补齐缺失商品。
// 没有逐店实录时才用逐店目录兜底，再依次尝试同类型同品牌实录和同类型模板。
// 返回的货架不与实录或内嵌目录共享可变行，门店覆盖在选择基础货架后应用。
//
// resolveShopBase preserves exact recordings and their prices, adding only missing
// items explicitly registered in the per-store definition. Without exact recordings,
// it tries per-store definitions, same-kind/brand recordings, then kind templates.
// Returned rows do not alias either source; overrides are applied afterward.
func resolveShopBase(storeID string, catalog *shopKindCatalog, recorded map[string][]*ShopPrice) ([]*ShopPrice, string, shopStoreDefinition, bool) {
	def, known := catalog.Stores[storeID]
	if rows := recorded[storeID]; len(rows) > 0 {
		base := copyShopPrices(rows)
		if len(def.Shelf) > 0 {
			have := make(map[string]bool, len(base)+len(def.Shelf))
			for _, row := range base {
				have[row.ItemID] = true
			}
			for _, row := range catalogShopPrices(def.Shelf) {
				if !have[row.ItemID] {
					base = append(base, row)
					have[row.ItemID] = true
				}
			}
		}
		return base, shopShelfSourceRecorded, def, known
	}
	if !known {
		return nil, shopShelfSourceUnknown, def, false
	}
	if len(def.Shelf) > 0 {
		return catalogShopPrices(def.Shelf), shopShelfSourceStore, def, true
	}

	kindKey := strconv.Itoa(def.Kind)
	brandKey := shopBrandKey(def.Brands)

	// Unbranded stores are a single franchise family. Prefer the explicitly
	// documented witness store for this kind when it has a recording.
	if len(def.Brands) == 0 {
		if source := catalog.Sources[kindKey]; source != "" {
			if rows := recorded[source]; len(rows) > 0 {
				return copyShopPrices(rows), shopShelfSourceBrand, def, true
			}
		}
	}

	recordedStores := make([]string, 0, len(recorded))
	for sid := range recorded {
		recordedStores = append(recordedStores, sid)
	}
	sort.Strings(recordedStores)
	for _, sid := range recordedStores {
		other, ok := catalog.Stores[sid]
		if !ok || other.Kind != def.Kind {
			continue
		}
		if shopBrandKey(other.Brands) != brandKey {
			continue
		}
		if rows := recorded[sid]; len(rows) > 0 {
			return copyShopPrices(rows), shopShelfSourceBrand, def, true
		}
	}

	if rows := catalog.Kinds[kindKey]; len(rows) > 0 {
		return catalogShopPrices(rows), shopShelfSourceKind, def, true
	}
	return nil, shopShelfSourceMissing, def, true
}

// shopItemPrice 是通用地图钓鱼站实录里同一商品的实际价格。
type shopItemPrice struct {
	Silver int64
	Gold   int64
}

// unbrandedFishingPrices 汇总所有通用地图钓鱼站实录里每个商品的价格。
// 只收 kind 0 且没有品牌限制的店（fishing_store_01/02、craft_baits_012 等），
// 品牌店与杂货店不参与，避免把通用渔具的价格套到品牌货架。
// 同一商品多个店都有价时取最小正值，即 max(min(price1, price2), 0)；
// 0 表示缺失，不参与比较；全部缺失时保持 0。
func unbrandedFishingPrices(catalog *shopKindCatalog, recorded map[string][]*ShopPrice) map[string]shopItemPrice {
	storeIDs := make([]string, 0, len(recorded))
	for storeID := range recorded {
		def, ok := catalog.Stores[storeID]
		if !ok || def.Kind != 0 || len(def.Brands) > 0 {
			continue
		}
		storeIDs = append(storeIDs, storeID)
	}
	sort.Strings(storeIDs)

	out := make(map[string]shopItemPrice)
	for _, storeID := range storeIDs {
		for _, row := range recorded[storeID] {
			if row == nil {
				continue
			}
			price := out[row.ItemID]
			if row.Silver > 0 && (price.Silver <= 0 || row.Silver < price.Silver) {
				price.Silver = row.Silver
			}
			if row.Gold > 0 && (price.Gold <= 0 || row.Gold < price.Gold) {
				price.Gold = row.Gold
			}
			out[row.ItemID] = price
		}
	}
	return out
}

// loadFishingPrices 汇总通用地图钓鱼站的价格：先取各店实录，
// 再用全局 item_price 补上实录里缺失的银价或金币价。
// 多来源都有价时取最小正值：max(min(price1, price2), 0)。
// item_price 是全服静态价目，不作为商品上架依据，只用于补已有条目的价格。
func (d *DB) loadFishingPrices(ctx context.Context, catalog *shopKindCatalog, recorded map[string][]*ShopPrice) (map[string]shopItemPrice, error) {
	prices := unbrandedFishingPrices(catalog, recorded)

	rows, err := d.SQL.QueryContext(ctx, `SELECT item_id, silver, gold FROM item_price`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var itemID string
		var price shopItemPrice
		if err := rows.Scan(&itemID, &price.Silver, &price.Gold); err != nil {
			return nil, err
		}
		cur := prices[itemID]
		if price.Silver > 0 && (cur.Silver <= 0 || price.Silver < cur.Silver) {
			cur.Silver = price.Silver
		}
		if price.Gold > 0 && (cur.Gold <= 0 || price.Gold < cur.Gold) {
			cur.Gold = price.Gold
		}
		prices[itemID] = cur
	}
	return prices, rows.Err()
}

// fillMissingPrices 只补货架上已有条目缺的价格，不新增商品、不改库存。
// 银币价和金币价分别判断：缺哪个补哪个，已有价格保持原样。
func fillMissingPrices(shelf []*ShopPrice, prices map[string]shopItemPrice) {
	for _, row := range shelf {
		if row == nil {
			continue
		}
		price, ok := prices[row.ItemID]
		if !ok {
			continue
		}
		if row.Silver <= 0 && price.Silver > 0 {
			row.Silver = price.Silver
		}
		if row.Gold <= 0 && price.Gold > 0 {
			row.Gold = price.Gold
		}
	}
}

func applyShopOverrides(base []*ShopPrice, overrides map[string]shopOverrideValue) []*ShopPrice {
	out := make([]*ShopPrice, 0, len(base))
	for _, row := range base {
		if row == nil {
			continue
		}
		v := *row
		if override, ok := overrides[v.ItemID]; ok {
			if override.Silver.Valid {
				v.Silver = override.Silver.Int64
			}
			if override.Gold.Valid {
				v.Gold = override.Gold.Int64
			}
			if override.Stock.Valid {
				v.Stock = override.Stock.Int64
			}
		}
		// 本地商店持续供货，零库存自动补回基础批量。
		if v.Stock <= 0 {
			v.Stock = 1000
		}
		out = append(out, &v)
	}
	return out
}

func (d *DB) effectiveShopShelf(ctx context.Context, storeID string) (*shopShelfResolution, error) {
	storeID = strings.TrimSpace(storeID)
	catalog, err := embeddedShopKindCatalog()
	if err != nil {
		return nil, err
	}
	// 房产商店走实抓货架：kind 7 模板是误用的农场货架，补齐逻辑还会把
	// 同类目的其他房子估价塞进来，都不该出现在买房界面。
	houses, houseErr := embeddedShopHouseShelf()
	if houseErr != nil {
		return nil, houseErr
	}
	if rows, ok := houses[storeID]; ok {
		base := copyShopPrices(rows)
		var overrides map[string]shopOverrideValue
		if len(base) > 0 {
			overrides, err = d.loadShopOverrides(ctx, storeID)
			if err != nil {
				return nil, err
			}
		}
		def := catalog.Stores[storeID]
		return &shopShelfResolution{
			Base:         base,
			Effective:    applyShopOverrides(base, overrides),
			Overrides:    overrides,
			Definition:   def,
			CatalogKnown: true,
			Source:       shopShelfSourceHouse,
		}, nil
	}
	recorded, err := d.loadRecordedShopShelves(ctx)
	if err != nil {
		return nil, err
	}
	base, source, def, known := resolveShopBase(storeID, catalog, recorded)
	// 通用地图钓鱼站共享价格表：货架上已有、但缺银价或金币价的商品，
	// 用别的地图钓鱼站实录和全局 item_price 里的真实价格补上；
	// 不新增商品、不改库存。品牌店、杂货店与未知店不参与。
	// 补价必须放在 augmentShopShelf 之后：有些商品是运行时按 item_price
	// 补出来的（例如只在 02 实录里带银价的款式），要在条目出现后再补价。
	shareFishingPrices := known && def.Kind == 0 && len(def.Brands) == 0
	var fishingPrices map[string]shopItemPrice
	if shareFishingPrices {
		fishingPrices, err = d.loadFishingPrices(ctx, catalog, recorded)
		if err != nil {
			return nil, err
		}
	}
	base, _, err = d.augmentShopShelf(ctx, def, base)
	if err != nil {
		return nil, err
	}
	if shareFishingPrices {
		fillMissingPrices(base, fishingPrices)
	}
	overrides := map[string]shopOverrideValue{}
	if len(base) > 0 {
		overrides, err = d.loadShopOverrides(ctx, storeID)
		if err != nil {
			return nil, err
		}
	}
	effective := applyShopOverrides(base, overrides)
	// 补齐货架：历史抓取/内嵌模板缺的新商品（例如 2025 款）在客户端看来是
	// “无库存”，这里按“同店内已有品类 +（品牌店）同品牌”补上，价格缺失时
	// 用同类商品中位价估价。
	effective, _, err = d.augmentShopShelf(ctx, def, effective)
	if err != nil {
		return nil, err
	}
	if shareFishingPrices {
		fillMissingPrices(effective, fishingPrices)
	}
	// 补价只填缺失字段；显式覆盖可能合法地把某一种货币设为 0，
	// 必须在补价后重新应用，否则 0 会被当成缺价再次填回。
	if shareFishingPrices && len(overrides) > 0 {
		effective = applyShopOverrides(effective, overrides)
	}
	return &shopShelfResolution{
		Base:         base,
		Effective:    effective,
		Overrides:    overrides,
		Definition:   def,
		CatalogKnown: known,
		Source:       source,
	}, nil
}
