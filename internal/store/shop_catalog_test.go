package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"strings"
	"testing"
)

func openShopCatalogTestDB(t *testing.T) *DB {
	t.Helper()
	db, err := Open(filepath.Join(t.TempDir(), "shop-catalog.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func findShopPrice(t *testing.T, rows []*ShopPrice, itemID string) *ShopPrice {
	t.Helper()
	for _, row := range rows {
		if row.ItemID == itemID {
			return row
		}
	}
	t.Fatalf("货架缺少 %s", itemID)
	return nil
}

func TestEmbeddedShopCatalogMatchesEvidenceAndCoversAllStores(t *testing.T) {
	sum := sha256.Sum256(embeddedShopKindDefs)
	const wantHash = "98fb9e2872d23c64559f8e1f6ec0eb2ff999f39cff96d959a0f3e6abac0e2381"
	if got := hex.EncodeToString(sum[:]); got != wantHash {
		t.Fatalf("商店目录 hash=%s want=%s", got, wantHash)
	}
	catalog, err := embeddedShopKindCatalog()
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.Stores) != 205 {
		t.Fatalf("商店目录 stores=%d want=205", len(catalog.Stores))
	}
	if len(catalog.Kinds) != 8 {
		t.Fatalf("商店目录 kinds=%d want=8", len(catalog.Kinds))
	}

	db := openShopCatalogTestDB(t)
	stores, err := db.ListAdminStores(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(stores) != 205 {
		t.Fatalf("管理后台商店数=%d want=205", len(stores))
	}
	var nonempty, missingEvidence int
	for _, view := range stores {
		if !view.CatalogKnown {
			t.Fatalf("目录内商店被标记为未知: %s", view.StoreID)
		}
		if view.ItemCount > 0 {
			nonempty++
			continue
		}
		missingEvidence++
		if view.Kind != 2 && view.Kind != 8 {
			t.Fatalf("有货架模板的 kind=%d 商店为空: %s", view.Kind, view.StoreID)
		}
		if view.ShelfSource != shopShelfSourceMissing {
			t.Fatalf("缺证据商店来源=%q want=%q", view.ShelfSource, shopShelfSourceMissing)
		}
	}
	if nonempty != 187 || missingEvidence != 18 {
		t.Fatalf("205 店覆盖异常: nonempty=%d missing=%d", nonempty, missingEvidence)
	}
}

func TestShopCatalogRecordedStoreAndBrandReusePrecedence(t *testing.T) {
	db := openShopCatalogTestDB(t)
	ctx := context.Background()
	if _, err := db.SQL.Exec(`INSERT INTO shop_item(store_id,item_id,silver,gold,stock) VALUES
		('fishingstore_006','exact_recorded_item',321,0,7),
		('monkeycola_001','brand_recorded_item',55,0,9)`); err != nil {
		t.Fatal(err)
	}

	exact, err := db.ShopItems(ctx, "fishingstore_006")
	if err != nil {
		t.Fatal(err)
	}
	if len(exact) != 1 || exact[0].ItemID != "exact_recorded_item" {
		t.Fatalf("逐店实录未优先于 kind 模板: %+v", exact)
	}

	reused, err := db.ShopItems(ctx, "monkeycola_002")
	if err != nil {
		t.Fatal(err)
	}
	if len(reused) != 1 || reused[0].ItemID != "brand_recorded_item" || reused[0].Stock != 9 {
		t.Fatalf("同 kind+同品牌实录未复用: %+v", reused)
	}
}

func TestShopCatalogStoreShelfPrecedenceAndCopies(t *testing.T) {
	catalog, err := embeddedShopKindCatalog()
	if err != nil {
		t.Fatal(err)
	}
	base, source, _, known := resolveShopBase("tungus_015", catalog, nil)
	if !known || source != shopShelfSourceStore || len(base) != 2 {
		t.Fatalf("逐店目录必须优先于kind模板: known=%v source=%s base=%+v", known, source, base)
	}
	if p := findShopPrice(t, base, "tungus_bot"); p.Silver != 0 || p.Gold != 22 || p.Stock != 1 {
		t.Fatalf("逐店目录价格不符: %+v", p)
	}
	base[0].Gold = 999
	again, _, _, _ := resolveShopBase("tungus_015", catalog, nil)
	if findShopPrice(t, again, "tungus_bot").Gold != 22 {
		t.Fatal("解析返回值不得修改内嵌目录")
	}
	recorded := map[string][]*ShopPrice{
		"tungus_015": {
			{ItemID: "tungus_bot", Gold: 25, Stock: 3},
			{ItemID: "shad_2556", Silver: 9950, Stock: 9999},
		},
	}
	exact, source, _, _ := resolveShopBase("tungus_015", catalog, recorded)
	if source != shopShelfSourceRecorded || len(exact) != 3 || exact[0].Gold != 25 || exact[0].Stock != 3 {
		t.Fatalf("逐店目录只补缺失商品，不得覆盖实录价格库存: source=%s base=%+v", source, exact)
	}
	if p := findShopPrice(t, exact, "shad_2556"); p.Silver != 9950 || p.Gold != 0 || p.Stock != 9999 {
		t.Fatalf("上游原有商品不得替换: %+v", p)
	}
	if p := findShopPrice(t, exact, "tungus_12pack"); p.Silver != 0 || p.Gold != 129 || p.Stock != 1 {
		t.Fatalf("缺失的礼盒应按目录补齐: %+v", p)
	}
	exact[0].Gold = 999
	findShopPrice(t, exact, "tungus_12pack").Gold = 999
	again, _, _, _ = resolveShopBase("tungus_015", catalog, recorded)
	if len(recorded["tungus_015"]) != 2 || len(again) != 3 || findShopPrice(t, again, "tungus_bot").Gold != 25 || findShopPrice(t, again, "tungus_12pack").Gold != 129 {
		t.Fatalf("合并返回值不得修改实录或内嵌目录: %+v", again)
	}
	recorded["tungus_015"] = append(recorded["tungus_015"], &ShopPrice{ItemID: "tungus_12pack", Gold: 150, Stock: 4})
	complete, source, _, _ := resolveShopBase("tungus_015", catalog, recorded)
	if source != shopShelfSourceRecorded || len(complete) != 3 || findShopPrice(t, complete, "tungus_12pack").Gold != 150 {
		t.Fatalf("完整实录不得产生重复SKU或被目录覆盖: source=%s base=%+v", source, complete)
	}
}

func TestTungusShopAdminAndPurchaseShelvesAgree(t *testing.T) {
	db, err := OpenWithStatic(filepath.Join(t.TempDir(), "tungus-admin.db"),
		filepath.Join("..", "..", "server_data", "static.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	if err := db.SetAdminShopOverride(ctx, "tungus_015", "tungus_bot", 0, 30, 0, "test"); err != nil {
		t.Fatal(err)
	}
	items, err := db.ListAdminStoreItems(ctx, "tungus_015", "", 100)
	if err != nil || len(items) != 95 {
		t.Fatalf("管理后台应保留93件上游商品并补齐两件特产: count=%d err=%v", len(items), err)
	}
	var totalStock int64
	for _, row := range items {
		price, err := db.ShopItemGet(ctx, "tungus_015", row.ItemID)
		if err != nil || price.Silver != row.Silver || price.Gold != row.Gold || price.Stock != row.Stock {
			t.Fatalf("后台与成交价不一致: admin=%+v purchase=%+v err=%v", row, price, err)
		}
		if row.ItemID == "tungus_bot" && (row.Gold != 30 || row.Silver != 0 || row.Stock != 1000) {
			t.Fatalf("后台应展示覆盖价格和自动补货库存: %+v", row)
		}
		totalStock += row.Stock
	}
	var recordedCount, recordedStock int64
	if err := db.SQL.QueryRow(`SELECT COUNT(*), SUM(stock) FROM static.shop_item WHERE store_id='tungus_015'`).Scan(&recordedCount, &recordedStock); err != nil {
		t.Fatal(err)
	}
	if recordedCount != 93 || totalStock != recordedStock+1001 {
		t.Fatalf("后台应保留上游库存，仅对蜜饮自动补货: recorded=%d total=%d want=%d", recordedCount, totalStock, recordedStock+1001)
	}
	stores, err := db.ListAdminStores(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range stores {
		if row.StoreID == "tungus_015" {
			if row.ItemCount != 95 || row.TotalStock != totalStock || row.OverrideCount != 1 || row.ShelfSource != shopShelfSourceRecorded {
				t.Fatalf("后台汇总应与补齐并自动补货后的实录货架一致: %+v", row)
			}
			return
		}
	}
	t.Fatal("管理后台缺少通古斯卡特产店")
}

func TestShopCatalogPreservesCurrencyAndPerStoreOverrides(t *testing.T) {
	db := openShopCatalogTestDB(t)
	ctx := context.Background()
	if _, err := db.SQL.Exec(`INSERT INTO item_def(numeric_id,item_id,category,kind,brand,attrs_json)
		VALUES(990001,'wallet_50',24,'','','{}')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL.Exec(`INSERT INTO item_catalog(item_id,name,name_zh,kind,numeric_id)
		VALUES('wallet_50','Wallet 50','ATM 钱包','',990001)`); err != nil {
		t.Fatal(err)
	}

	goldRows, err := db.ShopItems(ctx, "atm_006")
	if err != nil {
		t.Fatal(err)
	}
	if len(goldRows) != 5 {
		t.Fatalf("ATM 模板行数=%d want=5", len(goldRows))
	}
	for _, row := range goldRows {
		if row.Silver != 0 || row.Gold <= 0 {
			t.Fatalf("金币-only 模板被破坏: %+v", row)
		}
	}

	silverRows, err := db.ShopItems(ctx, "boatstation_006")
	if err != nil {
		t.Fatal(err)
	}
	if len(silverRows) != 4 {
		t.Fatalf("船站模板行数=%d want=4", len(silverRows))
	}
	for _, row := range silverRows {
		if row.Silver <= 0 || row.Gold != 0 {
			t.Fatalf("银币-only 模板被破坏: %+v", row)
		}
	}

	otherBase := findShopPrice(t, goldRows, "wallet_50")
	if err := db.SetAdminShopOverride(ctx, "atm_006", "wallet_50", 0, 60, 7, "test"); err != nil {
		t.Fatal(err)
	}
	if err := db.SetAdminShopOverride(ctx, "atm_009", "wallet_50", 0, 70, 13, "test"); err != nil {
		t.Fatal(err)
	}
	first, err := db.ShopItemGet(ctx, "atm_006", "wallet_50")
	if err != nil {
		t.Fatal(err)
	}
	second, err := db.ShopItemGet(ctx, "atm_009", "wallet_50")
	if err != nil {
		t.Fatal(err)
	}
	if first.Gold != 60 || first.Stock != 7 || second.Gold != 70 || second.Stock != 13 {
		t.Fatalf("覆盖值串店: first=%+v second=%+v", first, second)
	}
	if otherBase.Stock == first.Stock || otherBase.Stock == second.Stock {
		t.Fatalf("基础货架被原地修改: base=%+v", otherBase)
	}

	items, err := db.ListAdminStoreItems(ctx, "atm_006", "钱包", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || !items[0].HasOverride || items[0].Name != "ATM 钱包" ||
		items[0].Silver != 0 || items[0].Gold != 60 || items[0].Stock != 7 {
		t.Fatalf("后台生效货架不符: %+v", items)
	}
	if err := db.ResetAdminShopOverride(ctx, "atm_006", "wallet_50"); err != nil {
		t.Fatal(err)
	}
	reset, _ := db.ShopItemGet(ctx, "atm_006", "wallet_50")
	if reset.Gold != otherBase.Gold || reset.Stock != otherBase.Stock {
		t.Fatalf("重置未恢复模板: got=%+v base=%+v", reset, otherBase)
	}
	stillOverridden, _ := db.ShopItemGet(ctx, "atm_009", "wallet_50")
	if stillOverridden.Gold != 70 || stillOverridden.Stock != 13 {
		t.Fatalf("重置一店影响了另一店: %+v", stillOverridden)
	}
}

func TestShopCatalogUnknownStoreDoesNotFabricateShelf(t *testing.T) {
	db := openShopCatalogTestDB(t)
	ctx := context.Background()
	if _, err := db.SQL.Exec(`INSERT INTO item_price(item_id,silver,gold,stock)
		VALUES('global_only',100,1,99)`); err != nil {
		t.Fatal(err)
	}
	rows, err := db.ShopItems(ctx, "not_a_catalog_store")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("未知店不得用全局价目伪造货架: %+v", rows)
	}
	if _, err := db.ShopItemGet(ctx, "not_a_catalog_store", "global_only"); err == nil {
		t.Fatal("未知店不应命中全局价目")
	}
	if err := db.SetAdminShopOverride(ctx, "not_a_catalog_store", "global_only", 100, 1, 99, "test"); err == nil ||
		!strings.Contains(err.Error(), "生效货架") {
		t.Fatalf("未知店覆盖应被拒绝，得到 %v", err)
	}
}

func TestShopSupplementPricesRestockAndAdminStayAligned(t *testing.T) {
	db := openShopCatalogTestDB(t)
	ctx := context.Background()
	if _, err := db.SQL.Exec(`INSERT INTO item_def(numeric_id,item_id,category,kind,brand,attrs_json) VALUES
 (991001,'base_line',11,'line','brand_test','{}'),(991002,'gold_line',11,'line','brand_test','{}'),
 (991003,'silver_line',11,'line','brand_test','{}'),(991004,'unpriced_line',11,'line','brand_test','{}'),
 (991005,'foreign_food',26,'food','','{}');
 INSERT INTO shop_item(store_id,item_id,silver,gold,stock) VALUES('fishingstore_009','base_line',100,2,0);
 INSERT INTO item_price(item_id,silver,gold,stock) VALUES('gold_line',0,15,0),('silver_line',200,0,0),('foreign_food',20,0,0);`); err != nil {
		t.Fatal(err)
	}
	rows, err := db.ShopItems(ctx, "fishingstore_009")
	if err != nil || len(rows) != 4 {
		t.Fatalf("incomplete or cross-category shelf: %+v %v", rows, err)
	}
	for _, row := range rows {
		if row.Stock <= 0 {
			t.Fatal("shelf not restocked")
		}
	}
	gold := findShopPrice(t, rows, "gold_line")
	if gold.Gold != 15 || gold.Silver != 0 {
		t.Fatal("currency changed during supplementation")
	}
	if findShopPrice(t, rows, "unpriced_line").Silver <= 0 {
		t.Fatal("upstream estimate missing")
	}
	if err := db.SetAdminShopOverride(ctx, "fishingstore_009", "gold_line", 0, 23, 0, "test"); err != nil {
		t.Fatal(err)
	}
	bought, err := db.ShopItemGet(ctx, "fishingstore_009", "gold_line")
	if err != nil || bought.Gold != 23 || bought.Stock != 1000 {
		t.Fatalf("override/restock inconsistent: %+v %v", bought, err)
	}
	admin, err := db.ListAdminStoreItems(ctx, "fishingstore_009", "gold_line", 10)
	if err != nil || len(admin) != 1 || admin[0].Gold != bought.Gold || admin[0].Stock != bought.Stock || !admin[0].HasOverride {
		t.Fatalf("admin differs: %+v %v", admin, err)
	}
	stores, err := db.ListAdminStores(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, v := range stores {
		if v.StoreID == "fishingstore_009" {
			found = v.ItemCount == 4 && v.TotalStock == 4000 && v.OverrideCount == 1
		}
	}
	if !found {
		t.Fatal("admin summary omits augmented shelf")
	}
	if err := db.ResetAdminShopOverride(ctx, "fishingstore_009", "gold_line"); err != nil {
		t.Fatal(err)
	}
	restored, _ := db.ShopItemGet(ctx, "fishingstore_009", "gold_line")
	if restored.Gold != 15 || restored.Stock <= 0 {
		t.Fatal("reset broke supplement/restock")
	}
}

func TestAdminStoreBookShelvesByRegion(t *testing.T) {
	db, err := OpenWithStatic(filepath.Join(t.TempDir(), "shop-books.sqlite3"),
		filepath.Join("..", "..", "server_data", "static.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	all := []string{"seafish_book", "feeder_book", "float_fishing_book", "spinning_book"}
	cases := []struct {
		store string
		want  []string
	}{
		{"admin_012", []string{"seafish_book"}},
		{"admin_18", []string{"feeder_book"}},
		{"admin_01", []string{"float_fishing_book", "spinning_book"}},
		{"admin_19", []string{"float_fishing_book", "spinning_book"}},
	}
	for _, tc := range cases {
		t.Run(tc.store, func(t *testing.T) {
			rows, err := db.ShopItems(ctx, tc.store)
			if err != nil {
				t.Fatal(err)
			}
			byID := make(map[string]*ShopPrice, len(rows))
			for _, r := range rows {
				byID[r.ItemID] = r
			}
			wantSet := make(map[string]bool, len(tc.want))
			for _, id := range tc.want {
				wantSet[id] = true
			}
			for _, id := range all {
				row, ok := byID[id]
				// 凡是上架的技能书，有效价都必须是银 998500 / 金 500。
				if ok && (row.Silver != 998500 || row.Gold != 500) {
					t.Fatalf("%s@%s 技能书有效价不符: %+v", tc.store, id, row)
				}
				if !wantSet[id] {
					if ok {
						t.Fatalf("%s 不应上架 %s", tc.store, id)
					}
					continue
				}
				if !ok {
					t.Fatalf("%s 缺少 %s", tc.store, id)
				}
			}
		})
	}
}

// 咖啡馆（铜湖 cafestore_18）没有逐店 shop_item 实录，cafe_ukleyka 产品线的
// 品牌落在 item_def.kind 列，通用品牌补齐（只比 brand 列）永远补不到。这里
// 钉住逐店 shelf 解析结果，防止回退到 kind 1 食品模板。
func TestCafeStoreShelfComesFromStoreDefinition(t *testing.T) {
	db, err := OpenWithStatic(filepath.Join(t.TempDir(), "cafe-shelf.db"),
		filepath.Join("..", "..", "server_data", "static.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	catalog, err := embeddedShopKindCatalog()
	if err != nil {
		t.Fatal(err)
	}
	def := catalog.Stores["cafestore_18"]
	if len(def.Shelf) != 3 {
		t.Fatalf("咖啡馆逐店货架应登记 3 件: %+v", def.Shelf)
	}
	// 价格必须与静态价目一致（内部整数单位，4/6/1 = 界面 0.04/0.06/0.01 金币）。
	for _, sku := range def.Shelf {
		p, err := db.ItemPriceGet(ctx, sku.ItemID)
		if err != nil || p == nil {
			t.Fatalf("咖啡馆商品缺少静态价目: %s err=%v", sku.ItemID, err)
		}
		if p.Silver != sku.Silver || p.Gold != sku.Gold || p.Stock != sku.Stock {
			t.Fatalf("%s 逐店货架与静态价目不一致: shelf=%+v price=%+v", sku.ItemID, sku, p)
		}
		if p.Silver != 0 || p.Gold <= 0 {
			t.Fatalf("%s 应只提供金币价: %+v", sku.ItemID, p)
		}
	}

	rows, err := db.ShopItems(ctx, "cafestore_18")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"small_coffee": true, "big_coffee": true, "hd_box": true}
	if len(rows) != len(want) {
		t.Fatalf("咖啡馆生效货架应只含 3 件本店商品，实际 %d 件", len(rows))
	}
	for _, row := range rows {
		if !want[row.ItemID] {
			t.Fatalf("咖啡馆不应上架非本店商品: %s", row.ItemID)
		}
		delete(want, row.ItemID)
	}
	if len(want) != 0 {
		t.Fatalf("咖啡馆货架缺少本店商品: %v", want)
	}
	// 解析来源必须是本店逐店 shelf，而不是 kind 1 食品模板——后者正是
	// “客户端能点到咖啡、22/3 却查不到价目”的成因。
	shelf, err := db.effectiveShopShelf(ctx, "cafestore_18")
	if err != nil {
		t.Fatal(err)
	}
	if shelf.Source != shopShelfSourceStore {
		t.Fatalf("咖啡馆货架来源=%q want=%q", shelf.Source, shopShelfSourceStore)
	}
}

// hasShopPrice 判断货架里是否有指定商品，用于“不应补入”的断言。
func hasShopPrice(rows []*ShopPrice, itemID string) bool {
	for _, row := range rows {
		if row.ItemID == itemID {
			return true
		}
	}
	return false
}

// 通用地图钓鱼站共享价格表：货架上已有、但缺银价或金币价的商品，
// 用别的钓鱼站实录里的真实价格补上；不新增商品，也不覆盖已有价格。
func TestShopCatalogFillsMissingPricesAcrossFishingStores(t *testing.T) {
	db := openShopCatalogTestDB(t)
	ctx := context.Background()
	if _, err := db.SQL.Exec(`INSERT INTO shop_item(store_id,item_id,silver,gold,stock) VALUES
		('craft_baits_012','need_silver',9999,100,1),
		('craft_baits_012','need_gold',1234,150,1),
		('fishing_store_01','need_silver',0,100,5),
		('fishing_store_01','need_gold',1234,0,6),
		('fishing_store_01','keep_both',100,10,7),
		('fishing_store_02','need_silver',5678,100,9),
		('fishing_store_02','keep_both',999,99,9),
		('fishing_store_02','only_in_02',999,0,1);
		INSERT INTO item_price(item_id,silver,gold,stock) VALUES('need_gold',0,200,0);`); err != nil {
		t.Fatal(err)
	}

	// fishingstore_006 复用 fishing_store_01 实录：应在原条目上补价格。
	other, err := db.ShopItems(ctx, "fishingstore_006")
	if err != nil {
		t.Fatal(err)
	}
	if len(other) != 3 {
		t.Fatalf("补价格不得新增或删除商品: %+v", other)
	}
	if p := findShopPrice(t, other, "need_silver"); p.Silver != 5678 || p.Gold != 100 || p.Stock != 5 {
		t.Fatalf("缺银价应取多店中的最小正值: %+v", p)
	}
	if p := findShopPrice(t, other, "need_gold"); p.Silver != 1234 || p.Gold != 150 || p.Stock != 6 {
		t.Fatalf("缺金币价应取实录与 item_price 中的最小正值: %+v", p)
	}
	if p := findShopPrice(t, other, "keep_both"); p.Silver != 100 || p.Gold != 10 {
		t.Fatalf("已有价格不得被覆盖: %+v", p)
	}
	if hasShopPrice(other, "only_in_02") {
		t.Fatal("不得新增别店独有的商品")
	}

	// 源店自己已完整的行保持原样。
	second, err := db.ShopItems(ctx, "fishing_store_02")
	if err != nil {
		t.Fatal(err)
	}
	if p := findShopPrice(t, second, "keep_both"); p.Silver != 999 || p.Gold != 99 {
		t.Fatalf("源店原价被改动: %+v", p)
	}
	if p := findShopPrice(t, second, "only_in_02"); p.Silver != 999 || p.Gold != 0 {
		t.Fatalf("源店独有商品被改动: %+v", p)
	}

	// 品牌店不参与通用钓鱼站的价格补齐。
	brand, err := db.ShopItems(ctx, "ny_bazaarStore_001")
	if err != nil {
		t.Fatal(err)
	}
	if hasShopPrice(brand, "need_silver") {
		t.Fatal("品牌店不得补入通用钓鱼站的商品")
	}
}

// 运行时按 item_price 补出来的商品（别的钓鱼站没有实录行），
// 也要能从有实录的钓鱼站补到缺失的银价。
func TestShopCatalogFillsSilverForSupplementedFishingItems(t *testing.T) {
	db := openShopCatalogTestDB(t)
	ctx := context.Background()
	if _, err := db.SQL.Exec(`INSERT INTO item_def(numeric_id,item_id,category,kind,brand,attrs_json) VALUES
		(991101,'base_rod',6,'rod','','{}'),
		(991102,'spin_shared',6,'rod_spinning','','{}');
		INSERT INTO item_catalog(item_id,name,name_zh,kind,numeric_id) VALUES
		('base_rod','Base Rod','Base Rod','rod',991101),
		('spin_shared','Shared Rod 10000','Shared Rod 10000','rod_spinning',991102);
		INSERT INTO item_price(item_id,silver,gold,stock) VALUES('spin_shared',0,100,0);
		INSERT INTO shop_item(store_id,item_id,silver,gold,stock) VALUES
		('fishing_store_01','base_rod',100,0,1),
		('fishing_store_02','spin_shared',5678,100,1);`); err != nil {
		t.Fatal(err)
	}

	// fishingstore_006 复用 01 的 base_rod，augment 会按 item_price 补出
	// spin_shared（只有金币价），随后应从 02 实录补上银价 5678。
	rows, err := db.ShopItems(ctx, "fishingstore_006")
	if err != nil {
		t.Fatal(err)
	}
	p := findShopPrice(t, rows, "spin_shared")
	if p.Silver != 5678 || p.Gold != 100 {
		t.Fatalf("运行时补出的商品缺银价时未从别店实录补上: %+v", p)
	}
}
