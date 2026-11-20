# 商店提交前校验物品可下发性

Status: done

## Problem Statement

本次起于吸引材料、响珠等部分商品购买显示异常的反馈，不代表全部商品均有问题。购买商品时，新建产出可投影不代表最终合并到的旧堆可下发。旧堆 fields_json 损坏若被忽略，可能扣款、增加库存后返回缺失或错误的物品数据。

## Solution

拒绝解析失败的旧堆；对实际发放逻辑预演出的交付行进行投影和编码，在正式事务中核对交付行，并在提交前编码完整购买响应。任何上述步骤失败均回滚经济变更并返回可重放的原生失败响应，不跳过坏物品后返回购买成功。

## User Stories

1. 玩家购买正常商品后，购买响应包含实际交付的 GUID 与合并数量。
2. 商品或旧堆不能投影、编码时，购买失败且余额、库存和购买流水不改变；相同请求重放失败结果。

## Implementation Decisions

- grantPreparedItemTx 不再忽略旧堆 FieldsFromJSON 错误，所有使用该发放函数的调用方均收到错误。
- hStoreBuy 先在短事务中使用相同发放函数预演，再回滚，释放单连接后调用 ItemToTree 和 EncodeValue。预演不扣款，不保留库存变更。
- 正式事务扣款、发放后，逐行比较与预演结果；仅生成时刻不同的版本戳允许变化，并同步到已投影树。其他差异拒购回滚，不自动重试。
- 可退商品的最终购买凭据同步到投影树，最终编码覆盖凭据字段；退货测试比较响应凭据与落库凭据。
- 完整 804 响应编码及已注册响应合同检查在正式事务提交之前完成；不再提交后重新查询投影，也不再 continue 丢弃失败产出。
- 保持无钩拟饵空 args(60)、购买 804/args(61) 等现有协议形状；客户端基线 4.0.25034。无表结构、迁移或静态定义修改。

## Testing Decisions

- 真实临时 SQLite、真实静态商品 box_food_egg_10 → food_egg 和已有计件堆。
- 经 dispatchRequest → dispatch.executeRequest、完整响应合同及回执重放路径，验证损坏 JSON、旧堆不可编码、完整响应编码失败、正常合并。
- 正常路径解码最终响应，断言数量 11、原堆 GUID；失败和成功均重放，核对余额、库存、流水及回执字节。
- 保留银币/金币新产出投影和编码失败测试，运行购买、退货、回执聚焦回归。

## Out of Scope

- 不保证所有历史异常存档自动修复，不自动退款或补发。
- 不更改业务提交与回执完成之间现有崩溃窗口、网络投递保证或幂等架构。
- 投影沿用 ItemToTree 的既有定义查询和容错语义；不宣称任意商品语义均正确或所有客户端展示场景均已验证。
- 两次事务之间并发库存变化可能导致保守拒购；预演增加一次短事务，不在写事务中调用普通 DB 查询。
- 不变更部署配置、运行服务或玩家运行数据。

## Verification

- 修复前运行 TestStoreExistingEggStackWireReceipt 实际失败：损坏旧堆仍返回 completed 成功回执，余额 199000、库存 11、购买流水 1（初始余额 200000、库存 1）。
- 修复后：`go test ./internal/gameserver ./internal/store -run 'TestStore|TestShop|Test.*Receipt|Test.*Return' -count=1` 通过。
- Windows/amd64、CGO_ENABLED=0 下 `go build ./...` 与 `go vet ./...` 通过；CI 同款 `CGO_ENABLED=0 go test -p 1 ./internal/... ./cmd/gameserver/... ./cmd/gateway/... ./cmd/seed/...` 在 macOS 执行通过，无跳过用例。
- macOS 原生 `go test ./...`、`go vet ./...`、`go build ./...` 受 cmd/launcher 的 Windows 专用依赖限制；不将交叉构建等同于 Windows 实机验证。
- gofmt、doccheck 及差异检查通过；不引用旧无阻塞审查结论。
- 审核修复版本 c5906af 已部署实机验证：吸引材料、虾、彩珠、响珠等商品购买显示，已有计件堆合并，连续购买，可退商品退货，退出重登持久性，以及余额不足拒购均通过；具体商品型号和币种未逐项记录。损坏旧堆、编码失败及回执重放由自动化回归覆盖。
