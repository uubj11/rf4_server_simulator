# ATM 钱包购买直接到账

Status: done

## Problem Statement

ATM 商店出售的钱包此前按普通商品处理：玩家支付金币后获得一个背包内的钱包实例，还需要再次使用才能增加银币，与官服购买即到账的行为不一致，也给客户端留下无意义的钱包物品。

## Solution

在 22/3 金币购买入口优先识别 ATM 钱包。使用店内生效货架的金币价格扣款，并在同一经济事务内按钱包定义面额增加银币；成功响应沿用 804，不携带物品树，响应发送后推送 2/1 刷新余额。其他商店、银币购买和非钱包商品继续走原流程。

## User Stories

1. 玩家在 ATM 商店花金币购买钱包后，金币立即减少、银币立即增加，背包不出现钱包实例。
2. 金币不足时购买失败，金币和银币都不变化。
3. 非 ATM 商店或非钱包商品仍按现有商店购买流程处理，退货与物品树行为不受影响。
4. 同一请求因网络抖动重放时，通过既有请求回执键避免重复扣款和重复到账。

## Implementation Decisions

- 识别条件：`store_id` 以 `atm_` 开头、使用 22/3 金币列、物品定义 category 43，并从 `attrs.pkdlooielib` 读取银币面额。
- 成交价只信任 `storePrice` 返回的当前店内货架价；客户端报价不参与结算。
- 使用 `applyWalletTx` 在同一事务内完成金币扣减、银币增加和交易流水记录，余额不足整体回滚。
- 不创建钱包物品实例，也不写入购买退货凭据。
- 804 使用既有 `jfafgmcfjkc` 形状：f0 为商品 ID，f1/f2/f3 为空；不改变 args code 或客户端协议基线。
- 成功后通过 `AfterResponse` 推送一次 2/1；请求回执继续使用 `(token_hash, main, sub, call_id)` 幂等键。

## Testing Decisions

- 使用真实临时 SQLite 和经济事务测试服务器验证金币扣减、银币到账、背包不创建钱包。
- 验证余额不足时钱包与流水均不变化。
- 解码 804 固定形状，确认 f0 为商品 ID、其余字段为空，并确认只登记一次响应后余额刷新。
- 保留现有商店购买、退货和回执测试，覆盖非 ATM 商品继续走原分支。

## Out of Scope

- 不新增 ATM 钱包库存、补货或限购机制。
- 不改变其他钱包、其他货币列或其他商店的价格规则。
- 不新增表结构或迁移。

## Verification

- `go test ./internal/gameserver -run 'TestWalletAtm' -count=10`：通过。
- `go test ./...`：通过。
- `go vet ./...`、`go build ./...`、`go run ./tools/doccheck`、`git diff --check`：通过。