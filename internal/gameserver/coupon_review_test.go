package gameserver

import (
	"context"
	"fmt"
	"testing"

	"rf4server/internal/store"
	"rf4server/internal/wiregen"
)

// 本文件覆盖 PR #459 复审的三条阻塞项：
//   1. 通用库存数量槽口径回到 csQty（券除外），家族类写数量两槽同步。
//   2. 补偿迁移流水读取失败必须 fail closed：不补券、不写完成标记。
//   3. 券单次张数上限前移到发件/后台发放校验，领取侧不再必然失败。

// seedCat58Row 构造一行 cat58 dip_op_c：csQty 是真实剩余量，dnmfciolgep 预置
// 成定义常量 100（实机异常数据形态）。item.qty 故意写成 100 模拟被污染后的状态。
func seedCat58Row(t *testing.T, db *store.DB, uid int64, guid string, remaining uint64) *store.Item {
	t.Helper()
	ctx := context.Background()
	def, err := db.GetItemDefByItemID(ctx, "dip_op_c")
	if err != nil || def == nil {
		t.Skipf("静态库无 dip_op_c: %v", err)
	}
	if def.Category != 58 {
		t.Skipf("dip_op_c 类别 = %d，期望 58", def.Category)
	}
	cls, ok := resolveWireClass(def)
	if !ok {
		t.Fatal("dip_op_c 无 wire class")
	}
	obj, merr := wiregen.MakeObj(cls.Class, nil)
	if merr != nil {
		t.Fatalf("构造对象: %v", merr)
	}
	if idx := fieldIdxByCS(cls.Class, csGUID); idx >= 0 {
		obj.Fields[idx].Hex = guid
	}
	if idx := fieldIdxByCS(cls.Class, csNumericID); idx >= 0 {
		obj.Fields[idx].Num = ptrU64(uint64(def.NumericID))
	}
	if !setItemQty(cls.Class, obj.Fields, remaining) {
		t.Fatalf("写入数量槽失败: %s", cls.Class)
	}
	// 家族类的 dnmfciolgep 是常量语义，实机为 100；与 csQty 不一致才能测出误读。
	if idx := fieldIdxByCS(cls.Class, "dnmfciolgep"); idx >= 0 && idx < len(obj.Fields) {
		obj.Fields[idx].Num = ptrU64(100)
	}
	fj, jerr := FieldsToJSON(obj.Fields)
	if jerr != nil {
		t.Fatalf("编码字段: %v", jerr)
	}
	row := &store.Item{
		GUID: guid, UID: uid, ItemID: "dip_op_c", NumericID: def.NumericID,
		WireClass: cls.Class, TypeID: cls.TypeID, Container: "backpack",
		Qty: 100, FieldsJSON: fj, Durability: 1, Workmanship: 1,
	}
	if ierr := db.InsertItem(ctx, row); ierr != nil {
		t.Fatalf("插入物品: %v", ierr)
	}
	fresh, gerr := db.GetItemByGUID(ctx, guid)
	if gerr != nil {
		t.Fatalf("读回物品: %v", gerr)
	}
	return fresh
}

// TestCat58StackCountUsesCsQty —— 复审 P1：stackCountOf / alignItemRowQty /
// consumeItemQty 必须读 csQty（95），不能读 dnmfciolgep（100）。
func TestCat58StackCountUsesCsQty(t *testing.T) {
	t.Parallel()
	db := openRecipeTestDB(t)
	srv := newSkillsCraftServer(t, db)
	ctx := context.Background()
	const uid int64 = 2100060
	if _, err := db.EnsureProfile(ctx, uid); err != nil {
		t.Fatalf("建玩家档案: %v", err)
	}
	row := seedCat58Row(t, db, uid, fmt.Sprintf("%032x", uid), 95)

	if got := srv.stackCountOf(ctx, row); got != 95 {
		t.Fatalf("stackCountOf = %d，期望 95（csQty），dnmfciolgep 常量 100 不能当剩余量", got)
	}

	srv.alignItemRowQty(ctx, row)
	if row.Qty != 95 {
		t.Fatalf("alignItemRowQty 后 item.qty = %d，期望按 csQty 收敛到 95", row.Qty)
	}

	if got := srv.consumeItemQty(ctx, row, 1); got != 1 {
		t.Fatalf("consumeItemQty 返回 %d，期望 1", got)
	}
	after, err := db.GetItemByGUID(ctx, row.GUID)
	if err != nil {
		t.Fatalf("读回扣减后物品: %v", err)
	}
	af, err := FieldsFromJSON(after.FieldsJSON)
	if err != nil {
		t.Fatalf("解析扣减后字段: %v", err)
	}
	idx := fieldIdxByCS(after.WireClass, csQty)
	if idx < 0 || idx >= len(af) || af[idx].Num == nil || u64Of(af[idx].Num) != 94 {
		t.Fatalf("扣减后 csQty 应为 94，实得字段 %+v", af)
	}
}

// TestCouponQtyStillUsesMetaSlot —— 守护 P1 回退不误伤券：ahgeldclmdj 没有
// csQty，读写必须落在 dnmfciolgep（上游既有 setItemQty/itemQtyValue 行为保持）。
func TestCouponQtyStillUsesMetaSlot(t *testing.T) {
	t.Parallel()
	db := openRecipeTestDB(t)
	srv := newSkillsCraftServer(t, db)
	ctx := context.Background()
	const uid int64 = 2100062
	if _, err := db.EnsureProfile(ctx, uid); err != nil {
		t.Fatalf("建玩家档案: %v", err)
	}
	rows := grantCoupons(t, srv, uid, 1)
	fresh, err := db.GetItemByGUID(ctx, rows[0].GUID)
	if err != nil {
		t.Fatalf("读回券: %v", err)
	}
	fields, err := FieldsFromJSON(fresh.FieldsJSON)
	if err != nil {
		t.Fatalf("解析券字段: %v", err)
	}
	if n, ok := itemQtyValue(fresh.WireClass, fields); !ok || n != 1 {
		t.Fatalf("券 itemQtyValue = %d ok=%v，期望 1（dnmfciolgep）", n, ok)
	}
	if fieldIdxByCS(fresh.WireClass, csQty) >= 0 {
		t.Fatal("券实例类不应有 csQty 槽")
	}
	if !setItemQty(fresh.WireClass, fields, 1) {
		t.Fatal("券 setItemQty 应可写 dnmfciolgep")
	}
}

// TestCouponGrantRepairFailsClosedOnTxnReadError —— 复审 P2：txn 读取失败时
// 必须返回 error、不补券、不写完成标记（否则多补的券永久生效）。
func TestCouponGrantRepairFailsClosedOnTxnReadError(t *testing.T) {
	t.Parallel()
	db := openRecipeTestDB(t)
	srv := newSkillsCraftServer(t, db)
	ctx := context.Background()
	const uid int64 = 2100063
	if _, err := db.EnsureProfile(ctx, uid); err != nil {
		t.Fatalf("建玩家档案: %v", err)
	}
	// 保证 trophyBoardOutputIDs 非空，否则会在查询 txn 之前提前返回。
	if _, err := db.SQL.Exec(
		`INSERT OR IGNORE INTO item_def(numeric_id,item_id,category,kind,brand,attrs_json) VALUES(9900001,'review_trophy_board',71,'mount','','{}')`); err != nil {
		t.Fatalf("插入标本产物定义: %v", err)
	}
	seedCouponGrantAudit(t, db, uid, 999, 1000)
	rows := grantCoupons(t, srv, uid, 1)
	if err := srv.setCouponRowQuantity(ctx, rows[0].GUID, 999); err != nil {
		t.Fatalf("预置券数量: %v", err)
	}

	if _, err := db.SQL.Exec(`DROP TABLE txn`); err != nil {
		t.Fatalf("模拟 txn 读取失败: %v", err)
	}

	fixed, err := srv.repairCouponGrantQuantities(ctx)
	if err == nil {
		t.Fatal("txn 读取失败时补偿必须返回 error（fail closed）")
	}
	if fixed != 0 {
		t.Fatalf("txn 读取失败时 fixed = %d，期望 0", fixed)
	}
	if _, done, _ := db.KVGet(ctx, couponGrantRepairKey); done {
		t.Fatal("txn 读取失败时不得写完成标记")
	}
	fresh, err := db.GetItemByGUID(ctx, rows[0].GUID)
	if err != nil {
		t.Fatalf("读回券: %v", err)
	}
	fields, err := FieldsFromJSON(fresh.FieldsJSON)
	if err != nil {
		t.Fatalf("解析券字段: %v", err)
	}
	if n, ok := itemQtyValue(fresh.WireClass, fields); !ok || n != 999 {
		t.Fatalf("补偿失败后券数量被改动 = %d ok=%v，期望保持 999", n, ok)
	}
}

// TestCat58AddToStackAddsOnePiece —— 复审第三轮 P1 的现场用例：一行 csQty=95、
// dnmfciolgep=100 的 cat58 物品，AddToStack(...,1) 必须变成 96 件。修复前会把
// dnmfciolgep 的 100 当剩余量、再叠加一份定义单位量 100，落成 200。
func TestCat58AddToStackAddsOnePiece(t *testing.T) {
	t.Parallel()
	db := openRecipeTestDB(t)
	srv := newSkillsCraftServer(t, db)
	ctx := context.Background()
	const uid int64 = 2100064
	if _, err := db.EnsureProfile(ctx, uid); err != nil {
		t.Fatalf("建玩家档案: %v", err)
	}
	seedCat58Row(t, db, uid, fmt.Sprintf("%032x", uid), 95)

	row, merged, err := srv.AddToStack(ctx, uid, "dip_op_c", "backpack", 1)
	if err != nil {
		t.Fatalf("并堆: %v", err)
	}
	if !merged {
		t.Fatal("应并入已有堆，而不是新建一行")
	}
	if row.Qty != 96 {
		t.Fatalf("并堆后 row.qty = %d，期望 96（95+1 件）", row.Qty)
	}
	fresh, err := db.GetItemByGUID(ctx, row.GUID)
	if err != nil {
		t.Fatalf("读回物品: %v", err)
	}
	ff, err := FieldsFromJSON(fresh.FieldsJSON)
	if err != nil {
		t.Fatalf("解析字段: %v", err)
	}
	if idx := fieldIdxByCS(fresh.WireClass, csQty); idx < 0 || ff[idx].Num == nil || u64Of(ff[idx].Num) != 96 {
		t.Fatalf("csQty 应为 96，实得字段 %+v", ff)
	}
	if got := srv.stackCountOf(ctx, fresh); got != 96 {
		t.Fatalf("stackCountOf = %d，期望 96", got)
	}
}

// TestCouponGrantRepairFailsClosedOnCorruptAudit —— 复审第三轮 P2：发放记录
// 读不懂就等于少算发放量，必须直接返回错误、不补券、不写完成标记，而不是
// continue 之后照写全局 marker 把缺口永久隐藏。
func TestCouponGrantRepairFailsClosedOnCorruptAudit(t *testing.T) {
	t.Parallel()
	db := openRecipeTestDB(t)
	srv := newSkillsCraftServer(t, db)
	ctx := context.Background()
	const uid int64 = 2100065
	if _, err := db.EnsureProfile(ctx, uid); err != nil {
		t.Fatalf("建玩家档案: %v", err)
	}
	if _, err := db.SQL.Exec(
		`INSERT OR IGNORE INTO item_def(numeric_id,item_id,category,kind,brand,attrs_json) VALUES(9900002,'review_trophy_board2',71,'mount','','{}')`); err != nil {
		t.Fatalf("插入标本产物定义: %v", err)
	}
	rows := grantCoupons(t, srv, uid, 1)
	if err := srv.setCouponRowQuantity(ctx, rows[0].GUID, 999); err != nil {
		t.Fatalf("预置券数量: %v", err)
	}
	// detail 里带券 item_id（能被 LIKE 命中）但不是合法 JSON。
	if _, err := db.SQL.Exec(
		`INSERT INTO audit_log(admin,action,target,detail,ts) VALUES('admin','mail_send',?,?,?)`,
		fmt.Sprint(uid), `{"uid":`+fmt.Sprint(uid)+`,"items":[{"item_id":"`+couponChucheloItemID+`","qty":5}`, 1000.0); err != nil {
		t.Fatalf("写入损坏发放记录: %v", err)
	}

	fixed, err := srv.repairCouponGrantQuantities(ctx)
	if err == nil {
		t.Fatal("发放记录损坏时补偿必须返回 error（fail closed）")
	}
	if fixed != 0 {
		t.Fatalf("发放记录损坏时 fixed = %d，期望 0", fixed)
	}
	if _, done, _ := db.KVGet(ctx, couponGrantRepairKey); done {
		t.Fatal("发放记录损坏时不得写完成标记")
	}
	fresh, gerr := db.GetItemByGUID(ctx, rows[0].GUID)
	if gerr != nil {
		t.Fatalf("读回券: %v", gerr)
	}
	fields, ferr := FieldsFromJSON(fresh.FieldsJSON)
	if ferr != nil {
		t.Fatalf("解析券字段: %v", ferr)
	}
	if n, ok := itemQtyValue(fresh.WireClass, fields); !ok || n != 999 {
		t.Fatalf("补偿失败后券数量被改动 = %d ok=%v，期望保持 999", n, ok)
	}
}

// TestCouponConstructorsRejectWrongUnits —— 复审第三轮 P2：一券一实例必须在
// 所有构造入口强制，不能只有 CreateItem 一道闸。多张走 prepareCouponRows 逐张建行。
func TestCouponConstructorsRejectWrongUnits(t *testing.T) {
	t.Parallel()
	db := openRecipeTestDB(t)
	srv := newSkillsCraftServer(t, db)
	ctx := context.Background()
	const uid int64 = 2100066
	if _, err := db.EnsureProfile(ctx, uid); err != nil {
		t.Fatalf("建玩家档案: %v", err)
	}

	if _, err := srv.prepareCouponRows(ctx, uid, couponChucheloItemID, "backpack", 0); err == nil {
		t.Fatal("prepareCouponRows 收到 0 张必须报错，不能夹成 1 张")
	}
	if _, err := srv.prepareCouponRows(ctx, uid, couponChucheloItemID, "backpack", -3); err == nil {
		t.Fatal("prepareCouponRows 收到负数必须报错")
	}
	if _, err := srv.prepareNewItem(ctx, uid, couponChucheloItemID, "backpack", 2, ""); err == nil {
		t.Fatal("prepareNewItem 收到 units=2 必须报错")
	}
	if _, err := srv.prepareNewItemWithCount(ctx, uid, couponChucheloItemID, "backpack", 3, ""); err == nil {
		t.Fatal("prepareNewItemWithCount 收到 count=3 必须报错")
	}

	// 单张仍必须成功，否则闸门把正常发券也拦死。
	rows, err := srv.prepareCouponRows(ctx, uid, couponChucheloItemID, "backpack", 2)
	if err != nil {
		t.Fatalf("prepareCouponRows 发 2 张应成功: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("prepareCouponRows 发 2 张应得到 2 行，实得 %d", len(rows))
	}
	one, err := srv.prepareNewItem(ctx, uid, couponChucheloItemID, "backpack", 1, "")
	if err != nil {
		t.Fatalf("prepareNewItem units=1 应成功: %v", err)
	}
	if one.WireClass != couponWireClass {
		t.Fatalf("券实例类 = %s，期望 %s", one.WireClass, couponWireClass)
	}
}

// TestCat58TxPathStackCountUsesCsQty —— 复审第三轮 P1 的一半：非事务路径
// （stackCountOf）与事务路径（stackCountTx）必须读出同一个剩余量。事务路径
// 走的是 itemStackQtyValue，读 dnmfciolgep 会把 attrs 常量 100 当剩余量，
// 材料校验虚增、扣减后数量不动。
func TestCat58TxPathStackCountUsesCsQty(t *testing.T) {
	t.Parallel()
	db := openRecipeTestDB(t)
	srv := newSkillsCraftServer(t, db)
	ctx := context.Background()
	const uid int64 = 2100067
	if _, err := db.EnsureProfile(ctx, uid); err != nil {
		t.Fatalf("建玩家档案: %v", err)
	}
	row := seedCat58Row(t, db, uid, fmt.Sprintf("%032x", uid), 95)

	if got := srv.stackCountOf(ctx, row); got != 95 {
		t.Fatalf("stackCountOf = %d，期望 95（csQty）", got)
	}
	tx, err := beginEconomyTx(ctx, srv.DB)
	if err != nil {
		t.Fatalf("开事务: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	if got := stackCountTx(ctx, tx, row); got != 95 {
		t.Fatalf("stackCountTx = %d，期望 95（csQty，不能读 dnmfciolgep 的 100）", got)
	}
}

// TestCat58GrantPreparedMergesIntoCsQtyStack —— 复审第三轮 P1 的另一半：
// grantPreparedItemTx(merge=true) 的合并基线也必须来自 csQty。修复前基线读
// dnmfciolgep(100)，再叠加一条新建满量行(100)，落成 200；现在基线是 95。
//
// 注意：这里刻意断言 195 而不是复审给的 96——一条新建的满量行本身是 100 件
// （prepareNewItem 的 units 仍是「份数」），所以 95+100=195。要让这条也变成
// 95+1=96，必须把新建路径的 units 语义一并改成「件数」，那会把所有发放入口的
// 实物量除以定义单位量（饵料 ×2 从 60 件掉到 2 件），属于赠送量语义变更，
// 见 spec「并堆语义变更」的 [边界] 段，需维护者单独定调。
func TestCat58GrantPreparedMergesIntoCsQtyStack(t *testing.T) {
	t.Parallel()
	db := openRecipeTestDB(t)
	srv := newSkillsCraftServer(t, db)
	ctx := context.Background()
	const uid int64 = 2100068
	if _, err := db.EnsureProfile(ctx, uid); err != nil {
		t.Fatalf("建玩家档案: %v", err)
	}
	seedCat58Row(t, db, uid, fmt.Sprintf("%032x", uid), 95)

	prepared, perr := srv.prepareNewItem(ctx, uid, "dip_op_c", "backpack", 1, "")
	if perr != nil {
		t.Fatalf("构造一条满量行: %v", perr)
	}
	if prepared.Qty != 100 {
		t.Fatalf("一条新建满量行应为 100 件，实得 %d", prepared.Qty)
	}
	tx, err := beginEconomyTx(ctx, srv.DB)
	if err != nil {
		t.Fatalf("开事务: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	merged, ok, gerr := grantPreparedItemTx(ctx, tx, prepared, true)
	if gerr != nil {
		t.Fatalf("并入已有堆: %v", gerr)
	}
	if !ok {
		t.Fatal("应并入已有堆，而不是新建一行")
	}
	if merged.Qty != 195 {
		t.Fatalf("并堆后 qty = %d，期望 195（csQty 95 + 一条满量 100；修复前是 200）", merged.Qty)
	}
}

// TestFamilyQtyWriteSyncsBothSlots —— 复审 P1 的「更新家族类数量时同时同步
// 两个槽」：setItemQty 对 cat58 必须同时写 csQty（真实剩余量）与 dnmfciolgep
// （meta 主槽），否则事务内读 dnmfciolgep、通用库存读 csQty 会各读各的。
func TestFamilyQtyWriteSyncsBothSlots(t *testing.T) {
	t.Parallel()
	db := openRecipeTestDB(t)
	ctx := context.Background()
	def, err := db.GetItemDefByItemID(ctx, "dip_op_c")
	if err != nil || def == nil {
		t.Skipf("静态库无 dip_op_c: %v", err)
	}
	if def.Category != 58 {
		t.Skipf("dip_op_c 类别 = %d，期望 58", def.Category)
	}
	cls, ok := resolveWireClass(def)
	if !ok {
		t.Fatal("dip_op_c 无 wire class")
	}
	obj, merr := wiregen.MakeObj(cls.Class, nil)
	if merr != nil {
		t.Fatalf("构造对象: %v", merr)
	}
	if !setItemQty(cls.Class, obj.Fields, 42) {
		t.Fatal("setItemQty 应可写 cat58 数量槽")
	}
	cs := fieldIdxByCS(cls.Class, csQty)
	meta := fieldIdxByCS(cls.Class, "dnmfciolgep")
	if cs < 0 || meta < 0 {
		t.Fatalf("cat58 应同时具备 csQty 与 dnmfciolgep 槽：cs=%d meta=%d", cs, meta)
	}
	if obj.Fields[cs].Num == nil || u64Of(obj.Fields[cs].Num) != 42 {
		t.Fatalf("csQty 未同步为 42: %+v", obj.Fields[cs])
	}
	if obj.Fields[meta].Num == nil || u64Of(obj.Fields[meta].Num) != 42 {
		t.Fatalf("dnmfciolgep 未同步为 42: %+v", obj.Fields[meta])
	}
}

// TestFamilyStackCountWritesBothSlots —— 复审 P1 的另一半：setStackCount /
// setStackCountTx 改家族类数量时也必须同步两个槽。只写 csQty 会让 dnmfciolgep
// 停在 attrs 常量上，堆叠整理的一致性断言会把正常行判成「数量字段不一致」。
func TestFamilyStackCountWritesBothSlots(t *testing.T) {
	t.Parallel()
	db := openRecipeTestDB(t)
	srv := newSkillsCraftServer(t, db)
	ctx := context.Background()
	const uid int64 = 2100069
	if _, err := db.EnsureProfile(ctx, uid); err != nil {
		t.Fatalf("建档案: %v", err)
	}
	def, err := db.GetItemDefByItemID(ctx, "dip_op_c")
	if err != nil || def == nil {
		t.Skipf("静态库无 dip_op_c: %v", err)
	}
	cls, ok := resolveWireClass(def)
	if !ok {
		t.Fatal("dip_op_c 无 wire class")
	}
	csIdx := fieldIdxByCS(cls.Class, csQty)
	metaIdx := fieldIdxByCS(cls.Class, "dnmfciolgep")
	if csIdx < 0 || metaIdx < 0 {
		t.Fatalf("cat58 应同时具备两槽：cs=%d meta=%d", csIdx, metaIdx)
	}
	guid := fmt.Sprintf("%032x", uid)
	obj, merr := wiregen.MakeObj(cls.Class, nil)
	if merr != nil {
		t.Fatalf("构造对象: %v", merr)
	}
	fields := obj.Fields
	fields[csIdx].Num = ptrU64(100)
	fields[metaIdx].Num = ptrU64(100)
	if idx := fieldIdxByCS(cls.Class, csGUID); idx >= 0 {
		fields[idx].Hex = guid
	}
	if idx := fieldIdxByCS(cls.Class, csNumericID); idx >= 0 {
		fields[idx].Num = ptrU64(uint64(def.NumericID))
	}
	fj, jerr := FieldsToJSON(fields)
	if jerr != nil {
		t.Fatalf("序列化: %v", jerr)
	}
	row := &store.Item{
		GUID: guid, UID: uid, ItemID: "dip_op_c", NumericID: def.NumericID,
		WireClass: cls.Class, TypeID: cls.TypeID, Container: "backpack",
		Qty: 100, FieldsJSON: fj, Durability: 1, Workmanship: 1,
	}
	if err := db.InsertItem(ctx, row); err != nil {
		t.Fatalf("插入行: %v", err)
	}

	srv.setStackCount(ctx, row, 96)

	fresh, err := db.GetItemByGUID(ctx, guid)
	if err != nil || fresh == nil {
		t.Fatalf("读回行: %v", err)
	}
	got, err := FieldsFromJSON(fresh.FieldsJSON)
	if err != nil {
		t.Fatalf("解析字段: %v", err)
	}
	if n := u64Of(got[csIdx].Num); n != 96 {
		t.Fatalf("setStackCount 后 csQty = %d，期望 96", n)
	}
	if n := u64Of(got[metaIdx].Num); n != 96 {
		t.Fatalf("setStackCount 后 dnmfciolgep = %d，期望同步为 96", n)
	}
	if fresh.Qty != 96 {
		t.Fatalf("setStackCount 后 item.qty = %d，期望 96", fresh.Qty)
	}
	// 堆叠整理的一致性断言：itemQtyValue（families 读 dnmfciolgep）必须等于 item.qty。
	if v, okv := itemQtyValue(fresh.WireClass, got); !okv || int64(v) != fresh.Qty {
		t.Fatalf("itemQtyValue = %d ok=%v，与 item.qty %d 不一致（堆叠整理会判为脏数据）", v, okv, fresh.Qty)
	}
}

// TestValidateMailAttachmentRejectsOversizedCoupon —— 复审 P2：10001 张券
// 发件校验必须拒绝（领取侧 prepareCouponRows 上限 10000），10000 张放行。
func TestValidateMailAttachmentRejectsOversizedCoupon(t *testing.T) {
	t.Parallel()
	db := openRecipeTestDB(t)
	ctx := context.Background()
	var over int64 = maxBundlePreparedItems + 1
	if err := ValidateMailAttachment(ctx, db, store.MailItem{ItemID: couponChucheloItemID, Qty: over}); err == nil {
		t.Fatalf("发件校验接受了 %d 张券，领取侧必然失败", over)
	}
	if err := ValidateMailAttachment(ctx, db, store.MailItem{ItemID: couponChucheloItemID, Qty: maxBundlePreparedItems}); err != nil {
		t.Fatalf("%d 张券应放行: %v", maxBundlePreparedItems, err)
	}
	// 后台 grant_item 复用同一判据。
	if err := ValidateCouponGrantQty(ctx, db, couponChucheloItemID, over); err == nil {
		t.Fatalf("后台直接发放校验接受了 %d 张券", over)
	}
	if err := ValidateCouponGrantQty(ctx, db, couponChucheloItemID, maxBundlePreparedItems); err != nil {
		t.Fatalf("%d 张券后台发放应放行: %v", maxBundlePreparedItems, err)
	}
	// 非券类不受影响。
	if err := ValidateMailAttachment(ctx, db, store.MailItem{ItemID: "dip_op_c", Qty: over}); err != nil {
		t.Fatalf("非券类不应受券张数上限影响: %v", err)
	}
}

// TestCouponGrantRepairFailsClosedWhenTrophyDefsMissing —— 补偿必须能识别
// cat71 标本产物定义缺失：此时无法计算已用券数，不能把 used 当成 0 后补满
// 全部发放量并写完成标记。静态库不完整时应保持原券数并返回 error，下次再试。
func TestCouponGrantRepairFailsClosedWhenTrophyDefsMissing(t *testing.T) {
	t.Parallel()
	db := openRecipeTestDB(t)
	srv := newSkillsCraftServer(t, db)
	ctx := context.Background()
	const uid int64 = 2100070
	if _, err := db.EnsureProfile(ctx, uid); err != nil {
		t.Fatalf("建玩家档案: %v", err)
	}
	seedCouponGrantAudit(t, db, uid, 999, 1000)
	coupon, err := srv.CreateItem(ctx, uid, couponChucheloItemID, "backpack", 1)
	if err != nil {
		t.Fatalf("创建券失败: %v", err)
	}
	if _, err := db.SQL.Exec(`DELETE FROM item_def WHERE category=?`, trophyMountCategory); err != nil {
		t.Fatalf("删除标本产物定义: %v", err)
	}

	fixed, err := srv.repairCouponGrantQuantities(ctx)
	if err == nil {
		t.Fatal("缺少 cat71 产物定义时补偿必须 fail closed")
	}
	if fixed != 0 {
		t.Fatalf("fail closed 时 fixed=%d，期望 0", fixed)
	}
	if _, done, _ := db.KVGet(ctx, couponGrantRepairKey); done {
		t.Fatal("fail closed 时不得写完成标记")
	}
	fresh, err := db.GetItemByGUID(ctx, coupon.GUID)
	if err != nil || fresh == nil {
		t.Fatalf("读回券失败: %v", err)
	}
	fields, err := FieldsFromJSON(fresh.FieldsJSON)
	if err != nil {
		t.Fatalf("解析券字段: %v", err)
	}
	if n, ok := itemQtyValue(fresh.WireClass, fields); !ok || n != 1 {
		t.Fatalf("fail closed 后券数被改成 %d ok=%v，期望保持 1", n, ok)
	}
}
