package gameserver

import (
	"context"
	"encoding/hex"
	"strconv"
	"testing"
	"time"

	"rf4server/internal/proto"
	"rf4server/internal/store"
	"rf4server/internal/wiregen"
)

func TestEventStageGiftNonStackableQuantity(t *testing.T) {
	srv, db := newStaticRigTestServer(t)
	defer db.Close()
	uid := newProbeSess(srv).Userid
	setEventStageGifts(t, srv, db, `{"13":{"7":[{"item":"bcr_5008","qty":3},{"item":"fcb_coffee_can2","qty":2}]}}`)
	completeQualificationTasks(t, db, uid, eventStageRuleIDs(13, 7))
	gifts := srv.ensureEventStageGifts(t.Context(), uid)
	if len(gifts) != 4 {
		t.Fatalf("3件装备和1堆咖啡应返回4行，实得%d", len(gifts))
	}
	seen := map[string]bool{}
	for _, row := range gifts {
		if row.ItemID != "bcr_5008" {
			continue
		}
		if row.Qty != 1 || seen[row.GUID] {
			t.Fatalf("装备应逐件创建独立实例: %+v", row)
		}
		seen[row.GUID] = true
	}
	if len(seen) != 3 {
		t.Fatalf("独立装备实例=%d", len(seen))
	}
	if rows, qty := countContainerItem(t, db, uid, "bcr_5008", "backpack"); rows != 3 || qty != 3 {
		t.Fatalf("装备数量=%d/%d", rows, qty)
	}
	if rows, qty := countContainerItem(t, db, uid, "fcb_coffee_can2", "backpack"); rows != 1 || qty != 2 {
		t.Fatalf("咖啡数量=%d/%d", rows, qty)
	}
	if got := srv.ensureEventStageGifts(t.Context(), uid); len(got) != 0 {
		t.Fatal("重复巡检又发放了奖励")
	}
}

func TestEventStageGiftHonorsEventSwitches(t *testing.T) {
	for _, tc := range []struct{ name, newyear, anniversary, branches string }{
		{"both-disabled", "0", "0", "13"},
		{"branch-hidden", "1", "0", "14"},
		{"no-branches", "0", "1", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, db := newStaticRigTestServer(t)
			defer db.Close()
			uid := newProbeSess(srv).Userid
			setEventStageGifts(t, srv, db, `{"13":{"7":[{"item":"bcr_5008","qty":1}]}}`)
			completeQualificationTasks(t, db, uid, eventStageRuleIDs(13, 7))
			setSwitches := func(newyear, anniversary, branches string) {
				t.Helper()
				for key, value := range map[string]string{eventNewYearKey: newyear, eventAnniversaryKey: anniversary, eventBranchesKey: branches} {
					if _, err := db.SQL.Exec(`INSERT INTO tunables(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value); err != nil {
						t.Fatal(err)
					}
				}
				srv.tunMu.Lock()
				srv.tunCache = nil
				srv.tunMu.Unlock()
			}
			setSwitches(tc.newyear, tc.anniversary, tc.branches)
			if gifts := srv.ensureEventStageGifts(t.Context(), uid); len(gifts) != 0 {
				t.Fatal("关闭的活动或隐藏分支不能发奖")
			}
			if _, ok, _ := db.KVGet(t.Context(), eventStageGiftKey(uid, 13, 7)); ok {
				t.Fatal("关闭活动时不应写发放标记")
			}
			setSwitches("0", "1", "13")
			if gifts := srv.ensureEventStageGifts(t.Context(), uid); len(gifts) != 1 {
				t.Fatal("重新开启可见分支后应补发一次")
			}
			setSwitches("1", "0", "13")
			if gifts := srv.ensureEventStageGifts(t.Context(), uid); len(gifts) != 0 {
				t.Fatal("切换活动面板不能重复发放")
			}
		})
	}
}

func TestEventStageGiftSettlementResponseAndReplay(t *testing.T) {
	srv, db := newStaticRigTestServer(t)
	defer db.Close()
	sess := newProbeSess(srv)
	uid := sess.Userid
	if _, err := db.EnsureProfile(t.Context(), uid); err != nil {
		t.Fatal(err)
	}
	setEventStageGifts(t, srv, db, `{"13":{"7":[{"item":"bcr_5008","qty":1}]}}`)
	if err := db.TunableSet(t.Context(), "card_drop_normal", "1", "test"); err != nil {
		t.Fatal(err)
	}
	fish := &RolledFish{FishGUID: newFishGUID(), Species: "g_carp", WeightG: 7000, Grade: 1, LocLevel: evLocMedvezhye}
	extra := &RolledFish{FishGUID: newFishGUID(), Species: "perch", WeightG: 300, Grade: 1, LocLevel: evLocMedvezhye}
	st := srv.stateFor(uid)
	st.mu.Lock()
	st.Extra = []*RolledFish{extra}
	st.mu.Unlock()
	obj := wlSettle(t, srv, sess, fish, 1)
	if reason := checkResponseRule(ResponseContractRule{Kind: proto.KindObj, TypeIDs: []uint32{801}}, obj); reason != "" {
		t.Fatalf("801合同: %s", reason)
	}
	var guid string
	if err := db.SQL.QueryRow(`SELECT guid FROM item WHERE uid=? AND item_id='bcr_5008' AND container='backpack'`, uid).Scan(&guid); err != nil {
		t.Fatal(err)
	}
	item, _ := db.GetItemByGUID(t.Context(), guid)
	expected, err := srv.ItemToTree(t.Context(), item)
	if err != nil {
		t.Fatal(err)
	}
	expectedBytes, err := proto.EncodeValue(expected, sess.Table())
	if err != nil {
		t.Fatal(err)
	}
	countGift := func(items *proto.Value) int {
		t.Helper()
		if items == nil || items.Kind != proto.KindArgs || items.ArgCodeStr() != "61" {
			t.Fatalf("奖励应为args61: %v", items)
		}
		n := 0
		for _, it := range items.Items {
			b, e := proto.EncodeValue(it, sess.Table())
			if e != nil {
				t.Fatal(e)
			}
			if string(b) == string(expectedBytes) {
				n++
			}
		}
		return n
	}
	if n := countGift(obj.Fields[8].Val); n != 1 {
		t.Fatalf("801.f8阶段物品出现%d次", n)
	}
	// 主线的多钩卡片必须与阶段礼物同时保留在 801.f8。
	_, cardGUID := catchCardDraw(uid, extra.FishGUID)
	card, err := db.GetItemByGUID(t.Context(), cardGUID)
	if err != nil || card == nil {
		t.Fatalf("额外鱼卡片未入库: %v", err)
	}
	cardTree, err := srv.ItemToTree(t.Context(), card)
	if err != nil {
		t.Fatal(err)
	}
	cardBytes, err := proto.EncodeValue(cardTree, sess.Table())
	if err != nil {
		t.Fatal(err)
	}
	cardCount := 0
	for _, tree := range obj.Fields[8].Val.Items {
		encoded, err := proto.EncodeValue(tree, sess.Table())
		if err != nil {
			t.Fatal(err)
		}
		if string(encoded) == string(cardBytes) {
			cardCount++
		}
	}
	if cardCount != 1 {
		t.Fatalf("801.f8额外鱼卡片出现%d次", cardCount)
	}
	rows := obj.Fields[4].Val
	if rows == nil || rows.Kind != proto.KindArgs || rows.ArgCodeStr() != "173" {
		t.Fatalf("801.f4应为args173: %v", rows)
	}
	eventRows := 0
	for _, row := range rows.Items {
		if row.Kind != proto.KindObj || row.TypeID != 811 {
			t.Fatalf("801.f4包含嵌套容器: %v", row)
		}
		if u64Of(row.Fields[0].Num) == uint64(giftSourceGameEvent) {
			eventRows++
			if n := countGift(row.Fields[2].Val); n != 1 {
				t.Fatalf("811礼物行物品出现%d次", n)
			}
		}
	}
	if eventRows != 1 {
		t.Fatalf("活动礼物行=%d", eventRows)
	}
	replay := wlSettle(t, srv, sess, fish, 2)
	if n := countGift(replay.Fields[8].Val); n != 0 {
		t.Fatal("重放又携带阶段物品")
	}
	if rows := replay.Fields[4].Val; rows != nil && rows.Kind != proto.KindNull {
		for _, row := range rows.Items {
			if row.TypeID == 811 && u64Of(row.Fields[0].Num) == uint64(giftSourceGameEvent) {
				t.Fatal("重放又携带活动礼物行")
			}
		}
	}
	if rows, qty := countContainerItem(t, db, uid, "bcr_5008", "backpack"); rows != 1 || qty != 1 {
		t.Fatalf("重放后奖励数量=%d/%d", rows, qty)
	}
}

// eventStageRuleIDs 列出某活动分支某阶段已下发（非缺口）的规则编号。
func eventStageRuleIDs(branch, stage int64) []int64 {
	var ids []int64
	for _, r := range qualificationRules {
		if r.ID/1000 == branch && !eventGapRuleIDs[r.ID] && (r.ID%1000)/100 == stage-1 {
			ids = append(ids, r.ID)
		}
	}
	return ids
}

func setEventStageGifts(t *testing.T, srv *Server, db *store.DB, value string) {
	t.Helper()
	if _, err := db.SQL.Exec(`INSERT INTO tunables(key,value) VALUES(?,?)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value`, eventStageGiftsKey, value); err != nil {
		t.Fatalf("写 %s: %v", eventStageGiftsKey, err)
	}
	srv.tunMu.Lock()
	srv.tunCache = nil
	srv.tunMu.Unlock()
}

func TestAppend811RowsFlattensStageGiftContainer(t *testing.T) {
	line1 := &proto.Value{Kind: proto.KindObj, TypeID: 811}
	line2 := &proto.Value{Kind: proto.KindObj, TypeID: 811}
	base := proto.Args(proto.S("173"), line1)
	stage := proto.Args(proto.S("173"), line2)

	merged := append811Rows(base, stage)
	if merged.Kind != proto.KindArgs || merged.ArgCodeStr() != "173" || merged.ArgCount() != 2 {
		t.Fatalf("合并后的 801.f4 应为扁平 args(173)[2]，实得 kind=%s code=%q count=%d",
			merged.Kind, merged.ArgCodeStr(), merged.ArgCount())
	}
	for i, line := range merged.Items {
		if line == nil || line.Kind != proto.KindObj || line.TypeID != 811 {
			t.Fatalf("合并后的第 %d 项应为 obj(811)，实得 %#v", i, line)
		}
	}

	fresh := append811Rows(proto.Null(), stage)
	if fresh != stage || fresh.ArgCount() != 1 {
		t.Fatalf("空容器合并应直接复用 args(173) 行，实得 %#v", fresh)
	}
}

func countContainerItem(t *testing.T, db *store.DB, uid int64, itemID, container string) (rows int64, qty int64) {
	t.Helper()
	rowsPtr := &rows
	if err := db.SQL.QueryRow(
		`SELECT COUNT(*) FROM item WHERE uid=? AND item_id=? AND container=?`,
		uid, itemID, container).Scan(rowsPtr); err != nil {
		t.Fatal(err)
	}
	if err := db.SQL.QueryRow(
		`SELECT COALESCE(SUM(qty),0) FROM item WHERE uid=? AND item_id=? AND container=?`,
		uid, itemID, container).Scan(&qty); err != nil {
		t.Fatal(err)
	}
	return rows, qty
}

// 配置解析：坏 JSON、非活动分支、坏阶段号、qty 钳制、预览键去重。
func TestEventStageGiftPlansParse(t *testing.T) {
	srv, db := newStaticRigTestServer(t)
	defer db.Close()

	setEventStageGifts(t, srv, db, "not-json")
	if plans := srv.eventStageGiftPlans(); len(plans) != 0 {
		t.Fatalf("坏 JSON 应得空计划，实得 %v", plans)
	}
	// 分支 1 不是活动分支、阶段 8 越界、qty=0 钳成 1、重复键去重。
	cfg := `{"1":{"1":[{"item":"fcb_coffee_can2","qty":1}]},` +
		`"13":{"8":[{"item":"fcb_coffee_can2","qty":1}],"1":[` +
		`{"item":"fcb_coffee_can2","qty":0},{"item":"fcb_coffee_can2","qty":2},{"item":"ab_tea_can2","qty":3}]}}`
	setEventStageGifts(t, srv, db, cfg)
	plans := srv.eventStageGiftPlans()
	if len(plans) != 1 {
		t.Fatalf("应只剩分支 13 阶段 1 的计划，实得 %v", plans)
	}
	p := &plans[0]
	if p.Branch != 13 || p.Stage != 1 {
		t.Fatalf("计划应为 13/1，实得 %d/%d", p.Branch, p.Stage)
	}
	if len(p.Gifts) != 3 || p.Gifts[0].Qty != 1 || p.Gifts[1].Qty != 2 || p.Gifts[2].Qty != 3 {
		t.Fatalf("数量钳制错误，实得 %v", p.Gifts)
	}
	if len(p.Preview) != 2 || p.Preview[0] != "fcb_coffee_can2" || p.Preview[1] != "ab_tea_can2" {
		t.Fatalf("预览键应去重为 [咖啡,茶]，实得 %v", p.Preview)
	}
}

// 预览：有配置发 0x04 字符串数组（UTF8 字节），无配置发空数组。
func TestEventStageGiftPreviewEncoding(t *testing.T) {
	srv, db := newStaticRigTestServer(t)
	defer db.Close()
	setEventStageGifts(t, srv, db, `{"13":{"1":[{"item":"fcb_coffee_can2","qty":1},{"item":"ab_tea_can2","qty":1}]}}`)

	nodes := srv.eventNodes()
	if len(nodes) == 0 {
		t.Fatal("新年活动默认开启，应下发活动节点")
	}
	var branch13 *proto.Value
	for _, n := range nodes {
		if *n.Fields[0].Num == 13 {
			branch13 = n
		}
	}
	if branch13 == nil {
		t.Fatal("节点里找不到分支 13")
	}
	stages := branch13.Fields[3].Val
	stage1 := stages.Items[0]
	f2 := stage1.Fields[2].Val
	if f2 == nil || f2.Kind != proto.KindArray || len(f2.Items) != 2 {
		t.Fatalf("阶段 1 f2 应为 2 元素 0x04 数组，实得 %v", f2)
	}
	want := []string{"fcb_coffee_can2", "ab_tea_can2"}
	for i, it := range f2.Items {
		if it.Kind != proto.KindBytes {
			t.Fatalf("f2[%d] 应为字节元素，实得 %v", i, it.Kind)
		}
		if it.Hex != hex.EncodeToString([]byte(want[i])) {
			t.Fatalf("f2[%d] 应为 %s 的 UTF8，实得 %s", i, want[i], it.Hex)
		}
	}
	// 未配置的阶段仍有字符串数组类型，编码为空 0x04 数组。
	stage2 := stages.Items[1]
	if v := stage2.Fields[2].Val; v == nil || v.Kind != proto.KindArray || len(v.Items) != 0 {
		t.Fatalf("未配置阶段 f2 应为空数组，实得 %v", v)
	}
	encoded, err := proto.EncodeValue(stage2.Fields[2].Val, wiregen.Table())
	if err != nil || hex.EncodeToString(encoded) != "040000" {
		t.Fatalf("空字符串数组编码=%x err=%v", encoded, err)
	}
	// 整棵树可编码（协议回环）。
	if _, err := proto.EncodeValue(branch13, wiregen.Table()); err != nil {
		t.Fatalf("分支节点编码失败: %v", err)
	}
}

// 行为：阶段集齐恰好发一次、数量正确；重复巡检不重发；未集齐不发。
func TestEventStageGiftGrantOnce(t *testing.T) {
	srv, db := newStaticRigTestServer(t)
	defer db.Close()
	sess := newProbeSess(srv)
	uid := sess.Userid
	c := context.Background()
	setEventStageGifts(t, srv, db,
		`{"13":{"1":[{"item":"fcb_coffee_can2","qty":2},{"item":"ab_tea_can2","qty":1}]}}`)

	ids := eventStageRuleIDs(13, 1)
	if len(ids) == 0 {
		t.Fatal("分支 13 阶段 1 应有已下发任务")
	}
	// 差一条：不发。
	completeQualificationTasks(t, db, uid, ids[:len(ids)-1])
	if gifts := srv.ensureEventStageGifts(c, uid); len(gifts) != 0 {
		t.Fatalf("阶段未集齐不应发放，实得 %v", gifts)
	}
	// 补上最后一条：发一次，数量正确。
	completeQualificationTasks(t, db, uid, ids[len(ids)-1:])
	gifts := srv.ensureEventStageGifts(c, uid)
	if len(gifts) != 2 {
		t.Fatalf("应发 2 件物品，实得 %v", gifts)
	}
	if rows, qty := countContainerItem(t, db, uid, "fcb_coffee_can2", "backpack"); rows != 1 || qty != 2 {
		t.Fatalf("咖啡应 1 行 2 件，实得 %d 行 %d 件", rows, qty)
	}
	if rows, qty := countContainerItem(t, db, uid, "ab_tea_can2", "backpack"); rows != 1 || qty != 1 {
		t.Fatalf("茶应 1 行 1 件，实得 %d 行 %d 件", rows, qty)
	}
	if rows, qty := countContainerItem(t, db, uid, "fcb_coffee_can2", "house"); rows != 0 || qty != 0 {
		t.Fatalf("活动奖励不应进入 house，实得 %d 行 %d 件", rows, qty)
	}
	// 幂等：再巡检不重发。
	if gifts := srv.ensureEventStageGifts(c, uid); len(gifts) != 0 {
		t.Fatalf("重复巡检不应重发，实得 %v", gifts)
	}
}

// 缺口任务不阻塞阶段达标：缺口任务不进达标集合，可见任务全完成即发放。
func TestEventStageGiftIgnoresGapRules(t *testing.T) {
	var branch, stage int64
	var visible, gaps []int64
	found := false
	for b := range eventTaskCounts {
		for st := 1; st <= len(eventTaskCounts[b]); st++ {
			visible, gaps = nil, nil
			for _, r := range qualificationRules {
				if r.ID/1000 != int64(b) || (r.ID%1000)/100 != int64(st)-1 {
					continue
				}
				if eventGapRuleIDs[r.ID] {
					gaps = append(gaps, r.ID)
				} else {
					visible = append(visible, r.ID)
				}
			}
			if len(gaps) > 0 && len(visible) > 0 {
				branch, stage = int64(b), int64(st)
				found = true
				break
			}
		}
		if found {
			break
		}
	}
	if !found {
		t.Skip("没有同时含缺口与可见任务的阶段，本用例不适用")
	}

	srv, db := newStaticRigTestServer(t)
	defer db.Close()
	sess := newProbeSess(srv)
	uid := sess.Userid
	c := context.Background()
	setEventStageGifts(t, srv, db, `{"`+strconv.FormatInt(branch, 10)+`":{"`+strconv.FormatInt(stage, 10)+`":[{"item":"fcb_coffee_can2","qty":1}]}}`)

	completeQualificationTasks(t, db, uid, visible)
	if gifts := srv.ensureEventStageGifts(c, uid); len(gifts) == 0 {
		t.Fatalf("分支 %d 阶段 %d 可见任务全达标（缺口不算）应发放阶段礼物", branch, stage)
	}
}

// 活动阶段奖励中的完整装备必须按普通背包物品树下发；奖赏分组 f4=14
// 是资格奖杯专用投影，会导致装备模型在活动奖励卡片/背包中不可见。
func TestEventStageGiftUsesNormalBackpackTree(t *testing.T) {
	srv, db := newStaticRigTestServer(t)
	defer db.Close()
	uid := newProbeSess(srv).Userid
	ctx := context.Background()
	setEventStageGifts(t, srv, db, `{"13":{"7":[{"item":"bcr_5008","qty":1}]}}`)
	completeQualificationTasks(t, db, uid, eventStageRuleIDs(13, 7))

	gifts := srv.ensureEventStageGifts(ctx, uid)
	if len(gifts) != 1 {
		t.Fatalf("13/7 应发 1 件奖励，实得 %d", len(gifts))
	}
	row := gifts[0]
	if row.Container != "backpack" {
		t.Fatalf("活动装备奖励 container=%q，应 backpack", row.Container)
	}
	tree, err := srv.ItemToTree(ctx, row)
	if err != nil {
		t.Fatal(err)
	}
	idx := fieldIdxByCS(row.WireClass, csSlotEnum)
	if idx < 0 || idx >= len(tree.Fields) {
		t.Fatalf("活动装备奖励缺少容器字段 idx=%d", idx)
	}
	if got := u64Of(tree.Fields[idx].Num); got != rootItemStorageKind(row.WireClass) {
		t.Fatalf("活动装备奖励 f4=%d，应为普通背包枚举 %d", got, rootItemStorageKind(row.WireClass))
	}
}

func TestEventStageGiftSnapshotPushUsesBackpackTree(t *testing.T) {
	srv, db := newStaticRigTestServer(t)
	defer db.Close()
	uid := newProbeSess(srv).Userid
	setEventStageGifts(t, srv, db, `{"13":{"7":[{"item":"bcr_5008","qty":1}]}}`)
	completeQualificationTasks(t, db, uid, eventStageRuleIDs(13, 7))
	h := newMailTestSession(t, srv, 1, uid)
	if _, ok := hFog(&Ctx{Sess: h.sess}); !ok {
		t.Fatal("28/2 快照构造失败")
	}
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	for {
		select {
		case env, ok := <-h.envelopes:
			if !ok {
				t.Fatal("奖励推送前隧道关闭")
			}
			if env.Main != 4 || env.Sub != 22 {
				continue // 系统聊天播报不影响物品更新。
			}
			items, n, err := proto.DecodeValue(env.Body, 0, h.sess.Table())
			if err != nil || n != len(env.Body) {
				t.Fatalf("4/22 解码失败: %v", err)
			}
			if reason := checkPushArgsStrict(4, 22, items); reason != "" {
				t.Fatalf("4/22 合同不符: %s", reason)
			}
			if len(items.Items) != 1 {
				t.Fatalf("应推送一件阶段奖励，实得 %d", len(items.Items))
			}
			var guid string
			if err := db.SQL.QueryRow(`SELECT guid FROM item WHERE uid=? AND item_id=? AND container='backpack'`, uid, "bcr_5008").Scan(&guid); err != nil {
				t.Fatal(err)
			}
			row, err := db.GetItemByGUID(context.Background(), guid)
			if err != nil || row == nil {
				t.Fatalf("阶段奖励入账异常: %v", err)
			}
			tree := items.Items[0]
			idx := fieldIdxByCS(row.WireClass, csSlotEnum)
			if idx < 0 || idx >= len(tree.Fields) {
				t.Fatal("阶段奖励推送缺少容器字段")
			}
			if got := u64Of(tree.Fields[idx].Num); got != rootItemStorageKind(row.WireClass) {
				t.Fatalf("补发推送容器=%d，应为普通背包枚举 %d", got, rootItemStorageKind(row.WireClass))
			}
			if _, ok := hFog(&Ctx{Sess: h.sess}); !ok {
				t.Fatal("重复 28/2 快照构造失败")
			}
			assertNoMailTestPush(t, h)
			return
		case <-timer.C:
			t.Fatal("未收到活动阶段奖励 4/22 推送")
		}
	}
}
