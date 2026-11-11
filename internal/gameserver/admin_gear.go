// admin_gear.go —— 管理指令：装备槽位读写与商店代购。
//
// 外部程序（钓鱼宝典）只往 admin_queue 写一条 JSON，服务端每 2 秒由
// StartAdminSubscriber 取出执行。装配与购买均复用玩家在游戏里点按钮时的
// 同一套函数（rigSetComponent / rigRemoveComponentInstance / storePrice /
// prepareStoreGoods / applyWalletTx），因此槽位兼容性、断竿、余额不足等校验
// 与游戏内完全一致，不绕过任何规则，也不直接改 rig_component 表。
//
// 指令形状：
//
//	{"op":"set_component","uid":N,"rod":"竿guid","slot":槽位品类,"guid":"物品guid","extra":0}
//	{"op":"set_component","uid":N,"rod":"竿guid","slot":槽位品类}          // 不给 guid = 卸下该槽
//	{"op":"buy_mount","uid":N,"rod":"竿guid","slot":槽位品类,"item_id":"商品","store":"store_id"}
//	{"op":"buy_item","uid":N,"item_id":"商品","store":"store_id"}          // 只买不装
//
// 成功与否都写日志，宝典侧按 4/22 全量推送或后续读库核对结果。
package gameserver

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"rf4server/internal/store"
)

// adminGearTimeout 是单条装备指令的执行上限，与 execAdminCmd 的 10 秒一致。
const adminGearTimeout = 10 * time.Second

// adminGearRefresh 让在线客户端立刻看到装配结果。
// 4/22 是搏鱼全量同步（157 竿 + 140 线），2/1 刷新档案与背包计数。
func (s *Server) adminGearRefresh(uid int64) {
	for _, sess := range s.sessionsForUID(uid) {
		sess.Push4_22Full()
		sess.Push2_1()
	}
}

// adminSetComponent 处理 set_component：把某件背包物品装到竿的指定槽，
// 不给 guid 表示卸下该槽第一件。
func (s *Server) adminSetComponent(cmd *AdminCommand) {
	ctx, cancel := context.WithTimeout(context.Background(), adminGearTimeout)
	defer cancel()

	if cmd.RodGUID == "" {
		s.log.Warn("管理指令：装配缺少竿 guid", "uid", cmd.UID)
		return
	}
	if s.DB == nil {
		s.log.Warn("管理指令：数据库未就绪，装配跳过", "uid", cmd.UID)
		return
	}
	if s.ownedRodRow(ctx, cmd.UID, cmd.RodGUID) == nil {
		s.log.Warn("管理指令：竿不属于该玩家", "uid", cmd.UID, "rod", shortGUID(cmd.RodGUID))
		return
	}

	if cmd.ItemGUID == "" {
		res := s.rigRemoveComponentInstance(ctx, cmd.UID, cmd.RodGUID, cmd.Slot, "", cmd.Extra)
		if res == nil || res.CompGUID == "" {
			s.log.Warn("管理指令：卸下组件未命中",
				"uid", cmd.UID, "rod", shortGUID(cmd.RodGUID), "slot", cmd.Slot)
			return
		}
		s.adminGearRefresh(cmd.UID)
		s.log.Info("管理指令：卸下组件",
			"uid", cmd.UID, "rod", shortGUID(cmd.RodGUID), "slot", cmd.Slot,
			"removed", shortGUID(res.CompGUID))
		return
	}

	res := s.rigSetComponent(ctx, cmd.UID, cmd.RodGUID, cmd.Slot, cmd.ItemGUID, cmd.Extra,
		rigSetOpts{ParentType: familyRod, EnforceCategory: true, SplitStack: true})
	if res.Rejected || res.MountedGUID == "" {
		s.log.Warn("管理指令：装配被服务端拒绝",
			"uid", cmd.UID, "rod", shortGUID(cmd.RodGUID), "slot", cmd.Slot,
			"item", shortGUID(cmd.ItemGUID))
		return
	}
	s.adminGearRefresh(cmd.UID)
	s.log.Info("管理指令：装配成功",
		"uid", cmd.UID, "rod", shortGUID(cmd.RodGUID), "slot", cmd.Slot,
		"mounted", shortGUID(res.MountedGUID))
}

// adminResolveStore 决定在哪家店买：显式给了 store 就用它，否则取该商品
// 银币价最低的在架店。本地服实测同商品跨店同价，这一步只是把"各图商店不同价"
// 的口径固定成"按实时货架价成交"，改价后仍然成立。
func (s *Server) adminResolveStore(ctx context.Context, itemID, storeID string) (string, uint64, error) {
	if storeID != "" {
		price, err := s.storePrice(ctx, storeID, itemID, 0, false)
		return storeID, price, err
	}
	if s.DB == nil || s.DB.SQL == nil {
		return "", 0, errors.New("数据库未初始化")
	}
	var sid string
	var silver int64
	err := s.DB.SQL.QueryRowContext(ctx, `
		SELECT store_id, silver FROM shop_item
		WHERE item_id = ? AND silver > 0
		ORDER BY silver ASC, store_id ASC LIMIT 1`, itemID).Scan(&sid, &silver)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", 0, fmt.Errorf("该商品不在任何在架商店: %s", itemID)
		}
		return "", 0, err
	}
	return sid, uint64(silver), nil
}

// adminBuyOne 按店内实时价扣款买入一件，返回入库后的物品行与成交价。
// 事务内扣款、记流水、入库，任一步失败整笔回滚，不会出现"扣了钱没发货"。
func (s *Server) adminBuyOne(ctx context.Context, uid int64, itemID, storeID string) (*store.Item, uint64, string, error) {
	storeID, price, err := s.adminResolveStore(ctx, itemID, storeID)
	if err != nil {
		return nil, 0, storeID, err
	}
	prepared, _, err := s.prepareStoreGoods(ctx, uid, itemID)
	if err != nil {
		return nil, 0, storeID, fmt.Errorf("商品不可构造: %w", err)
	}
	tx, err := beginEconomyTx(ctx, s.DB)
	if err != nil {
		return nil, 0, storeID, fmt.Errorf("开启事务失败: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	note := fmt.Sprintf("store=%s via=codex", storeID)
	if err := applyWalletTx(ctx, tx, uid, -int64(price), 0, "buy", itemID, "", note); err != nil {
		if errors.Is(err, errInsufficientFunds) {
			return nil, 0, storeID, errors.New("余额不足")
		}
		return nil, 0, storeID, fmt.Errorf("扣款失败: %w", err)
	}
	granted := make([]*store.Item, 0, len(prepared))
	for _, it := range prepared {
		row, _, gerr := grantPreparedItemTx(ctx, tx, it, false)
		if gerr != nil {
			return nil, 0, storeID, fmt.Errorf("入库失败: %w", gerr)
		}
		granted = append(granted, row)
	}
	if err := s.saveStorePurchaseTx(ctx, tx, uid, storeID, itemID, price, false, granted); err != nil {
		return nil, 0, storeID, fmt.Errorf("购买凭据失败: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, 0, storeID, fmt.Errorf("提交失败: %w", err)
	}
	if len(granted) == 0 {
		return nil, 0, storeID, errors.New("商品未产生物品")
	}
	return granted[0], price, storeID, nil
}

// adminBuyItem 处理 buy_item：只买不装。
func (s *Server) adminBuyItem(cmd *AdminCommand) {
	ctx, cancel := context.WithTimeout(context.Background(), adminGearTimeout)
	defer cancel()
	if cmd.ItemID == "" {
		s.log.Warn("管理指令：代购缺少商品 id", "uid", cmd.UID)
		return
	}
	row, price, storeID, err := s.adminBuyOne(ctx, cmd.UID, cmd.ItemID, cmd.StoreID)
	if err != nil {
		s.log.Warn("管理指令：代购失败", "uid", cmd.UID, "item", cmd.ItemID, "store", cmd.StoreID, "err", err)
		return
	}
	s.adminGearRefresh(cmd.UID)
	s.log.Info("管理指令：代购成功",
		"uid", cmd.UID, "item", cmd.ItemID, "store", storeID, "price", price, "guid", shortGUID(row.GUID))
}

// adminBuyAndMount 处理 buy_mount：先按店内价买，再立刻装到指定槽。
// 装配失败不回滚购买（物品已合法进入背包），宝典可随后再发 set_component。
func (s *Server) adminBuyAndMount(cmd *AdminCommand) {
	ctx, cancel := context.WithTimeout(context.Background(), adminGearTimeout)
	defer cancel()
	if cmd.ItemID == "" || cmd.RodGUID == "" {
		s.log.Warn("管理指令：代购装配缺少参数", "uid", cmd.UID, "item", cmd.ItemID)
		return
	}
	if s.ownedRodRow(ctx, cmd.UID, cmd.RodGUID) == nil {
		s.log.Warn("管理指令：竿不属于该玩家，代购中止", "uid", cmd.UID, "rod", shortGUID(cmd.RodGUID))
		return
	}
	row, price, storeID, err := s.adminBuyOne(ctx, cmd.UID, cmd.ItemID, cmd.StoreID)
	if err != nil {
		s.log.Warn("管理指令：代购装配的购买失败",
			"uid", cmd.UID, "item", cmd.ItemID, "store", cmd.StoreID, "err", err)
		return
	}
	res := s.rigSetComponent(ctx, cmd.UID, cmd.RodGUID, cmd.Slot, row.GUID, cmd.Extra,
		rigSetOpts{ParentType: familyRod, EnforceCategory: true, SplitStack: true})
	mounted := res != nil && !res.Rejected && res.MountedGUID != ""
	s.adminGearRefresh(cmd.UID)
	if !mounted {
		s.log.Warn("管理指令：代购成功但装配被拒，物品留在背包",
			"uid", cmd.UID, "item", cmd.ItemID, "store", storeID, "price", price,
			"guid", shortGUID(row.GUID), "slot", cmd.Slot)
		return
	}
	s.log.Info("管理指令：代购并装配成功",
		"uid", cmd.UID, "item", cmd.ItemID, "store", storeID, "price", price,
		"rod", shortGUID(cmd.RodGUID), "slot", cmd.Slot, "mounted", shortGUID(res.MountedGUID))
}
