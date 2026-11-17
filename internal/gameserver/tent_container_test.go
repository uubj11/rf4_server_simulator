package gameserver

import (
	"context"
	"testing"

	"rf4server/internal/config"
	"rf4server/internal/logging"
	"rf4server/internal/proto"
)

// 帐篷存储（容器枚举 1/6）的库存枚举链路：4/1 计数必须带非零容量，4/4 必须
// 能列出帐篷物品，物品树的容器枚举必须投影为 1。
//
// 现状缺口：nativeInventoryContainers 没有 tent 分支、hItemCount 对容器 1
// 走 default 回 used=0/cap=0，客户端据此在 UI 层直接禁用「移放到帐篷里」。
// 官方容量：帐篷 30 件，会员 1.5 倍 = 45。
func TestTentContainerInventory(t *testing.T) {
	t.Parallel()
	db := openStaticTestDB(t)
	ctx := context.Background()
	srv := NewServer(config.Game{Tunnel: config.Tunnel{PSK: "tent"}}, logging.Discard(), nil)
	srv.SetDB(db)
	if err := srv.EnsurePlayer(ctx, 1000002); err != nil {
		t.Fatal(err)
	}
	sess := &Session{ID: 2, Userid: 1000002, Srv: srv, log: logging.Discard()}

	// 枚举映射：1/6 都指向 tent 存储。
	for _, kind := range []uint64{1, 6} {
		got := nativeInventoryContainers(kind)
		if len(got) != 1 || got[0] != "tent" {
			t.Fatalf("nativeInventoryContainers(%d) = %v，期望 [tent]", kind, got)
		}
	}

	// 播种：背包 1 件 + 帐篷 2 件。
	if _, err := srv.CreateItem(ctx, 1000002, "worm_01", "backpack", 1); err != nil {
		t.Fatalf("创建背包 worm_01 失败: %v", err)
	}
	tentCount := 2
	for i := 0; i < tentCount; i++ {
		if _, err := srv.CreateItem(ctx, 1000002, "worm_01", "tent", 1); err != nil {
			t.Fatalf("创建帐篷 worm_01 失败: %v", err)
		}
	}

	// 4/1 容器 1：used=2、容量 30、会员容量 45。
	b, ok := hItemCount(&Ctx{Sess: sess, Main: 4, Sub: 1, Tree: proto.Args524(proto.U32(1)), DecodeOK: true})
	if !ok {
		t.Fatal("4/1 未处理")
	}
	dec, _, err := proto.DecodeValue(b, 0, srv.Table())
	if err != nil {
		t.Fatalf("4/1 响应解码失败: %v", err)
	}
	if dec.Kind != proto.KindObj || len(dec.Fields) < 3 {
		t.Fatalf("4/1 响应形状异常: kind=%v fields=%d", dec.Kind, len(dec.Fields))
	}
	used, cap, capMember := u64Of(dec.Fields[0].Num), u64Of(dec.Fields[1].Num), u64Of(dec.Fields[2].Num)
	if used != 2 {
		t.Errorf("4/1 容器1 used=%d，期望 2", used)
	}
	if cap != tentContainerCapacity {
		t.Errorf("4/1 容器1 容量=%d，期望 %d", cap, tentContainerCapacity)
	}
	if capMember != tentContainerCapacityMember {
		t.Errorf("4/1 容器1 会员容量=%d，期望 %d", capMember, tentContainerCapacityMember)
	}

	// 4/1 容器 6：同一存储。
	b6, _ := hItemCount(&Ctx{Sess: sess, Main: 4, Sub: 1, Tree: proto.Args524(proto.U32(6)), DecodeOK: true})
	dec6, _, err := proto.DecodeValue(b6, 0, srv.Table())
	if err != nil {
		t.Fatalf("4/1 容器6 解码失败: %v", err)
	}
	if got := u64Of(dec6.Fields[0].Num); got != 2 {
		t.Errorf("4/1 容器6 used=%d，期望 2", got)
	}
	if got := u64Of(dec6.Fields[1].Num); got != tentContainerCapacity {
		t.Errorf("4/1 容器6 容量=%d，期望 %d", got, tentContainerCapacity)
	}

	// 4/4 容器 1：回帐篷内 2 个 GUID。
	b44, ok := hItemGUIDs(&Ctx{Sess: sess, Main: 4, Sub: 4, Tree: proto.Args524(proto.U32(1)), DecodeOK: true})
	if !ok {
		t.Fatal("4/4 未处理")
	}
	dec44, _, err := proto.DecodeValue(b44, 0, srv.Table())
	if err != nil {
		t.Fatalf("4/4 响应解码失败: %v", err)
	}
	if dec44.Kind != proto.KindGuidArr || len(dec44.Items) != tentCount {
		t.Fatalf("4/4 容器1 返回 %d 个 GUID（kind=%v），期望 %d", len(dec44.Items), dec44.Kind, tentCount)
	}
	want, err := srv.DB.ItemGUIDsByContainers(ctx, 1000002, []string{"tent"})
	if err != nil {
		t.Fatal(err)
	}
	if len(want) != tentCount {
		t.Fatalf("DB 帐篷物品数 %d，期望 %d", len(want), tentCount)
	}
	for i, it := range dec44.Items {
		got := ""
		if it.Hex != "" {
			got = it.Hex
		}
		if got != want[i] {
			t.Errorf("4/4[%d] = %s，期望 %s", i, got, want[i])
		}
	}
}

// 帐篷物品的树内容器枚举必须投影为 1：客户端物品管理器按该枚举把物品挂进
// 对应容器字典，恒 0 时帐篷里的物品会被算成背包物品。
func TestTentItemTreeStorageKind(t *testing.T) {
	t.Parallel()
	db := openStaticTestDB(t)
	ctx := context.Background()
	srv := NewServer(config.Game{Tunnel: config.Tunnel{PSK: "tent"}}, logging.Discard(), nil)
	srv.SetDB(db)
	if err := srv.EnsurePlayer(ctx, 1000003); err != nil {
		t.Fatal(err)
	}
	it, err := srv.CreateItem(ctx, 1000003, "worm_01", "tent", 1)
	if err != nil {
		t.Fatalf("创建帐篷 worm_01 失败: %v", err)
	}
	tree, err := srv.ItemToTree(ctx, it)
	if err != nil {
		t.Fatal(err)
	}
	idx := fieldIdxByCS(tree.Class, csSlotEnum)
	if idx < 0 {
		t.Fatalf("类 %s 缺容器枚举字段", tree.Class)
	}
	if got := u64Of(tree.Fields[idx].Num); got != containerTent {
		t.Errorf("帐篷物品树容器枚举=%d，期望 %d", got, containerTent)
	}
}

// 4/6 端到端（帐篷）：背包→帐篷、帐篷→背包时，DB 归属和 4/21/4/22
// 增量树的容器枚举都必须同步成目标容器，否则客户端把物品挂错字典。
func TestTentItemMoveEndToEnd(t *testing.T) {
	t.Parallel()
	srv, _ := newStaticRigTestServer(t)
	ctx := t.Context()
	sess, peer := newTunnelProbe(t, srv)
	pushes := handitemPushReader(t, peer)
	it, err := srv.CreateItem(ctx, sess.Userid, "worm_01", "backpack", 1)
	if err != nil {
		t.Fatalf("创建 worm_01 失败: %v", err)
	}

	runMove := func(dst uint64, wantContainer string, wantKind uint64) {
		t.Helper()
		tree := proto.Args524(proto.U32(0), proto.U32(uint32(dst)), proto.GuidArr([]string{it.GUID}))
		body, ok := hItemMove(&Ctx{Sess: sess, Main: 4, Sub: 6, Tree: tree, DecodeOK: true,
			ExpectResponse: true, Log: logging.Discard()})
		if !ok || len(body) == 0 {
			t.Fatalf("4/6 移动到 %d 未处理", dst)
		}
		first := nextPush(t, pushes)
		if first.Main != 4 || first.Sub != 21 || !pushHasGUID(pushGuids(t, srv, first), it.GUID) {
			t.Fatalf("移动到 %d 首条推送 = %d/%d，期望 4/21 删除", dst, first.Main, first.Sub)
		}
		second := nextPush(t, pushes)
		if second.Main != 4 || second.Sub != 22 || !pushHasGUID(pushGuids(t, srv, second), it.GUID) {
			t.Fatalf("移动到 %d 次条推送 = %d/%d，期望 4/22 加入", dst, second.Main, second.Sub)
		}
		dec, _, err := proto.DecodeValue(second.Body, 0, srv.Table())
		if err != nil {
			t.Fatalf("4/22 解码失败: %v", err)
		}
		if dec.ArgCount() < 1 {
			t.Fatalf("4/22 没有物品树: %+v", dec)
		}
		itemTree := dec.ArgItem(dec.ArgCount() - 1)
		idx := fieldIdxByCS(itemTree.Class, csSlotEnum)
		if idx < 0 {
			t.Fatalf("4/22 物品树类 %s 缺容器枚举", itemTree.Class)
		}
		if got := u64Of(itemTree.Fields[idx].Num); got != wantKind {
			t.Fatalf("4/22 容器枚举=%d，期望 %d", got, wantKind)
		}
		row, err := srv.DB.GetItemByGUID(ctx, it.GUID)
		if err != nil || row == nil {
			t.Fatalf("读取移动后物品失败: %+v %v", row, err)
		}
		if row.Container != wantContainer {
			t.Fatalf("移动后 container=%s，期望 %s", row.Container, wantContainer)
		}
	}

	runMove(1, "tent", containerTent)
	runMove(6, "tent", containerTent)
	runMove(0, "backpack", 0)
}
