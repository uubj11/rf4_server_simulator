package gameserver

// 切鱼（craft_bait_cut_fish）改造的验收测试：
// 分档表边界、消耗鱼护渔获、产出=块数、无鱼失败、批量不放大、别人的鱼不采用。

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"rf4server/internal/logging"
	"rf4server/internal/proto"
)

// TestFishPieceCountForWeight 逐档验证重量→块数，边界必须严格落在左闭右开区间。
func TestFishPieceCountForWeight(t *testing.T) {
	// 精确点位：出肉率 280‰→450‰（25kg 封顶线性升），每块 7g，四舍五入。
	cases := []struct {
		g    int64
		want int64
		note string
	}{
		{-500, 1, "负数钳到 1"},
		{0, 1, "零重量钳到 1"},
		{1, 1, "1g 不够一块"},
		{7, 1, "正好一块"},
		{50, 2, "50g"},
		{100, 4, "100g"},
		{200, 8, "200g"},
		{300, 12, "300g"},
		{500, 20, "500g"},
		{600, 24, "中位 600g"},
		{1000, 41, "1kg"},
		{1400, 58, "1.4kg"},
		{1578, 65, "本档现存渔获"},
		{2500, 106, "2.5kg"},
		{5000, 224, "5kg"},
		{10000, 497, "10kg"},
		{15000, 819, "15kg"},
		{20901, 1260, "本档最大渔获"},
		{25000, 1607, "25kg 出肉率封顶"},
		{30000, 1929, "30kg"},
		{50000, 3214, "50kg 仍按 45% 出肉"},
	}
	for _, c := range cases {
		if got := fishPieceCountForWeight(c.g); got != c.want {
			t.Fatalf("fishPieceCountForWeight(%d) = %d, want %d（%s）", c.g, got, c.want, c.note)
		}
	}

	// 单调性：重量增加，块数不得减少，且恒 ≥ 1。
	prev := int64(0)
	for g := int64(0); g <= 50000; g += 50 {
		got := fishPieceCountForWeight(g)
		if got < prev {
			t.Fatalf("块数随重量递减：%dg=%d < 上一点 %d", g, got, prev)
		}
		if got < 1 {
			t.Fatalf("%dg 块数应 ≥1，实得 %d", g, got)
		}
		prev = got
	}

	// 出肉率合理区间：1kg 以上整鱼切出的鱼肉（块数×7g）占体重 27%~46%。
	for _, g := range []int64{1000, 3000, 8000, 20000, 40000} {
		got := fishPieceCountForWeight(g)
		ratio := float64(got*7) / float64(g)
		if ratio < 0.27 || ratio > 0.46 {
			t.Fatalf("%dg 出肉率 %.3f 越界（块数 %d）", g, ratio, got)
		}
	}
}

// TestCraftCutFishConsumesKeepnet 一条 1578g 的鱼：消耗该条、按出肉率产出 65 块。
func TestCraftCutFishConsumesKeepnet(t *testing.T) {
	db := openRecipeTestDB(t)
	srv := newSkillsCraftServer(t, db)
	srv.randFloat = fixedRolls(0.9)
	ctx := context.Background()
	const uid int64 = 2000101
	if _, err := db.EnsureProfile(ctx, uid); err != nil {
		t.Fatal(err)
	}

	// seedImportedKeepnetFish 造 300+id 克；1278 → 1578g（出肉率约 29% → 65 块）
	entry, _ := seedImportedKeepnetFish(t, srv, uid, 1278, false)

	box := &handlerBox{srv: srv, sess: &Session{ID: uint64(uid), Userid: uid, Srv: srv, log: logging.Discard()}, main: 9, sub: 1}
	box.tree = proto.Args524(craftObj8("craft_bait_cut_fish",
		[]craftSlot{{ItemID: "bait_fishForCut", GUID: entry.EchoGUID, Species: "asp"}}, 1))
	body, ok := box.invoke(t)
	if !ok {
		t.Fatal("9/1 切鱼未处理")
	}
	v := decodeTreeValue(t, srv, body)
	if !v.IsKind(proto.KindObj) || v.TypeID != 803 {
		t.Fatalf("响应应为 803，实得 %v/%d", v.Kind, v.TypeID)
	}
	if u64Of(v.Fields[0].Num) != 1 {
		t.Fatal("切鱼应成功（803.f0=1）")
	}
	if got := craftResultQty(t, v); got != 65 {
		t.Fatalf("803.f1 产出应为 65 块，实得 %d", got)
	}
	if got := craftInventoryQty(t, v); got != 65 {
		t.Fatalf("803.f6 背包增量应为 65 块，实得 %d", got)
	}

	// 鱼护条目必须被消费
	if _, err := db.KeepnetGet(ctx, uid, entry.EchoGUID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("鱼护条目应已删除，实得 err=%v", err)
	}
	var qty int64
	if err := db.SQL.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(qty),0) FROM item WHERE uid=? AND item_id='fish_pieces'`, uid).Scan(&qty); err != nil {
		t.Fatal(err)
	}
	if qty != 65 {
		t.Fatalf("背包鱼块应为 65，实得 %d", qty)
	}
}

// TestCraftCutFishWithoutFishFails 鱼护为空时切鱼必须失败，且不产出、不写流水。
func TestCraftCutFishWithoutFishFails(t *testing.T) {
	db := openRecipeTestDB(t)
	srv := newSkillsCraftServer(t, db)
	srv.randFloat = fixedRolls(0.9)
	ctx := context.Background()
	const uid int64 = 2000102
	if _, err := db.EnsureProfile(ctx, uid); err != nil {
		t.Fatal(err)
	}

	box := &handlerBox{srv: srv, sess: &Session{ID: uint64(uid), Userid: uid, Srv: srv, log: logging.Discard()}, main: 9, sub: 1}
	box.tree = proto.Args524(craftObj8("craft_bait_cut_fish", nil, 1))
	body, ok := box.invoke(t)
	if !ok {
		t.Fatal("9/1 切鱼未处理")
	}
	v := decodeTreeValue(t, srv, body)
	if !v.IsKind(proto.KindObj) || v.TypeID != 803 {
		t.Fatalf("响应应为 803，实得 %v/%d", v.Kind, v.TypeID)
	}
	if u64Of(v.Fields[0].Num) != 0 {
		t.Fatal("没有鱼时切鱼应失败（803.f0=0）")
	}

	var qty int64
	if err := db.SQL.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(qty),0) FROM item WHERE uid=? AND item_id='fish_pieces'`, uid).Scan(&qty); err != nil {
		t.Fatal(err)
	}
	if qty != 0 {
		t.Fatalf("失败时不应产出鱼块，实得 %d", qty)
	}
	var txns int64
	if err := db.SQL.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM txn WHERE uid=? AND kind='craft'`, uid).Scan(&txns); err != nil {
		t.Fatal(err)
	}
	if txns != 0 {
		t.Fatalf("失败时不应写制作流水，实得 %d 条", txns)
	}
}

// TestCraftCutFishIgnoresBatchQty 请求 qty=1000 也只切一条鱼、产出该鱼块数。
func TestCraftCutFishIgnoresBatchQty(t *testing.T) {
	db := openRecipeTestDB(t)
	srv := newSkillsCraftServer(t, db)
	srv.randFloat = fixedRolls(0.9)
	ctx := context.Background()
	const uid int64 = 2000103
	if _, err := db.EnsureProfile(ctx, uid); err != nil {
		t.Fatal(err)
	}
	// id=1 → 301g → 出肉率约 28% → 12 块
	entry, _ := seedImportedKeepnetFish(t, srv, uid, 1, false)

	box := &handlerBox{srv: srv, sess: &Session{ID: uint64(uid), Userid: uid, Srv: srv, log: logging.Discard()}, main: 9, sub: 1}
	box.tree = proto.Args524(craftObj8("craft_bait_cut_fish",
		[]craftSlot{{ItemID: "bait_fishForCut", GUID: entry.EchoGUID}}, 1000))
	body, ok := box.invoke(t)
	if !ok {
		t.Fatal("9/1 切鱼未处理")
	}
	v := decodeTreeValue(t, srv, body)
	if u64Of(v.Fields[0].Num) != 1 {
		t.Fatal("批量请求也应成功，但只切一条")
	}
	if got := craftResultQty(t, v); got != 12 {
		t.Fatalf("301g 应切 12 块（出肉率约 0.28），实得 %d", got)
	}
	var remaining int64
	if err := db.SQL.QueryRowContext(ctx, `SELECT COUNT(*) FROM keepnet_items WHERE uid=?`, uid).Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if remaining != 0 {
		t.Fatalf("只应消耗 1 条，实得剩余 %d 条", remaining)
	}
}

// TestCraftCutFishIgnoresForeignKeepnetFish 槽位指向别人的鱼时不能被采用，
// 必须回落到自己的鱼护条目。
func TestCraftCutFishIgnoresForeignKeepnetFish(t *testing.T) {
	db := openRecipeTestDB(t)
	srv := newSkillsCraftServer(t, db)
	srv.randFloat = fixedRolls(0.9)
	ctx := context.Background()
	const uid int64 = 2000104
	const other int64 = 2000105
	for _, u := range []int64{uid, other} {
		if _, err := db.EnsureProfile(ctx, u); err != nil {
			t.Fatal(err)
		}
	}
	mine, _ := seedImportedKeepnetFish(t, srv, uid, 1278, false)  // 1578g → 65
	theirs, _ := seedImportedKeepnetFish(t, srv, other, 1, false) // 301g → 12

	box := &handlerBox{srv: srv, sess: &Session{ID: uint64(uid), Userid: uid, Srv: srv, log: logging.Discard()}, main: 9, sub: 1}
	box.tree = proto.Args524(craftObj8("craft_bait_cut_fish",
		[]craftSlot{{ItemID: "bait_fishForCut", GUID: theirs.EchoGUID}}, 1))
	body, ok := box.invoke(t)
	if !ok {
		t.Fatal("9/1 切鱼未处理")
	}
	v := decodeTreeValue(t, srv, body)
	if u64Of(v.Fields[0].Num) != 1 {
		t.Fatal("自己的鱼护有鱼，切鱼应成功")
	}
	if got := craftResultQty(t, v); got != 65 {
		t.Fatalf("应采用自己的 1578g 鱼（65 块），实得 %d", got)
	}
	// 别人的鱼必须还在
	if _, err := db.KeepnetGet(ctx, other, theirs.EchoGUID); err != nil {
		t.Fatalf("别人的鱼不应被消耗，实得 err=%v", err)
	}
	// 自己的鱼应被消耗
	if _, err := db.KeepnetGet(ctx, uid, mine.EchoGUID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("自己的鱼应已被切掉，实得 err=%v", err)
	}
}
