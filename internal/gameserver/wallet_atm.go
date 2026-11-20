package gameserver

import (
	"errors"
	"fmt"
	"strings"

	"rf4server/internal/proto"
	"rf4server/internal/wiregen"
)

// ATM 钱包：购买即到账，不在背包留下 wallet_* 实例。
// 客户端在 atm_01/atm_02 花金币买 wallet_50/100/300/1000/5000，
// 官方行为是金币扣掉后银币直接进 profile.silver，背包不出现"银币"物品。
// 此前把钱包当普通商品发到背包、还需要再次使用，与官方行为不一致。

const (
	atmStorePrefix    = "atm_"
	atmWalletCategory = int64(43)     // 钱包品类
	atmWalletAmountCS = "pkdlooielib" // 钱包面额（银币分）
)

// tryBuyWalletAtm 在 hStoreBuy 开头拦截 ATM 钱包购买。
// 只接管 store_id 以 atm_ 开头、金币支付、且物品是带面额的钱包；其余交回原流程。
func (s *Server) tryBuyWalletAtm(ctx *Ctx, storeID, itemID string, goldCol bool) ([]byte, bool) {
	if !strings.HasPrefix(storeID, atmStorePrefix) || !goldCol {
		return nil, false
	}
	def, err := s.DB.GetItemDefByItemID(ctx.Sess.pushCtx(), itemID)
	if err != nil || def == nil || def.Category != atmWalletCategory {
		return nil, false
	}
	amount, ok := defAttrU64(def, atmWalletAmountCS)
	if !ok || amount == 0 {
		return nil, false
	}
	fail := func(message string) ([]byte, bool) {
		ctx.ReplyFailure("System.InvalidOperationException", message)
		return nil, true
	}
	cctx, uid := ctx.Sess.pushCtx(), ctx.Sess.Userid
	ints := treeInts(ctx.Tree)
	var offer uint64
	if len(ints) > 0 {
		offer = ints[0]
	}
	// 价格只取店内生效货架，客户端报价仅用于诊断。
	price, err := s.storePrice(cctx, storeID, itemID, offer, true)
	if err != nil {
		ctx.Log.Warn("ATM 钱包无金币价目", "store", storeID, "item", itemID, "err", err)
		return fail("购买未完成，请刷新商店后重试")
	}
	tx, err := beginEconomyTx(cctx, s.DB)
	if err != nil {
		return fail("购买未完成，请刷新商店后重试")
	}
	defer func() { _ = tx.Rollback() }()
	// 扣金币与加银币同事务，条件更新由 applyWalletTx 保证余额不为负。
	if err := applyWalletTx(cctx, tx, uid, int64(amount), -int64(price),
		"buy", itemID, "", fmt.Sprintf("store=%s currency=gold wallet_direct", storeID)); err != nil {
		if errors.Is(err, errInsufficientFunds) {
			return fail("余额不足，未购买商品")
		}
		return fail("购买未完成，请刷新商店后重试")
	}
	// 804 回执不携带物品树，客户端只刷新余额。
	obj804 := wiregen.MustMakeObj("jfafgmcfjkc", map[int]*proto.Value{
		0: proto.StrVal(itemID),
		1: proto.Null(),
		2: proto.Null(),
		3: proto.Null(),
	})
	body, err := proto.EncodeValue(obj804, ctx.Sess.Table())
	if err != nil {
		return fail("购买未完成，请刷新商店后重试")
	}
	if reason := s.dispatch.checkRegisteredResponse(uint8(ctx.Main), uint8(ctx.Sub), body, ctx.Sess.Table()); reason != "" {
		return fail("购买未完成，请刷新商店后重试")
	}
	if err := tx.Commit(); err != nil {
		return fail("购买未完成，请刷新商店后重试")
	}
	ctx.AfterResponse(func() { ctx.Sess.Push2_1() })
	ctx.Log.Info("ATM 钱包直接到账", "store", storeID, "item", itemID, "gold", -int64(price), "silver", amount)
	return body, true
}
