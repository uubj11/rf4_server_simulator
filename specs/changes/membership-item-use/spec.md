# 会员时长物品使用

Status: done

## Problem Statement

category 75 的会员时长物品在 4/8 使用时，客户端收到成功快照但服务端没有续期会员，导致物品提示使用成功却不增加会员时间，并可能被重复使用。

## Solution

在通用 4/8 物品使用入口识别 category 75 会员物品。按定义属性 `mgbjgaebnne` 的天数续期 `profile.membership`，在同一经济事务中记录流水并消耗物品；活跃会员从原到期时间顺延，过期或未开通会员从当前时间起算，永久会员拒绝使用并保留物品。成功响应使用 800 移除/更新快照，响应后推送 2/1 和物品变更。其他物品类别交回原处理链。

## User Stories

1. 非永久会员使用 3 天或 7 天会员物品后，会员立即增加对应天数，物品按数量被消耗。
2. 仍有有效会员时，新时长从原到期时间顺延，不覆盖剩余时间。
3. 永久会员使用会员物品会失败，会员状态和物品都保持不变。
4. 续期、流水或扣物任一步失败时整体回滚。
5. 非会员品类继续由酒精、技能书、礼盒或通用物品处理分支处理。
6. 同一 4/8 请求因网络重试时，通过既有请求回执避免重复续期或重复扣物。

## Implementation Decisions

- 识别条件：物品定义 category 75；天数读取 `attrs.mgbjgaebnne`，有效范围为 1 到 3660 天。
- 只接受玩家本人背包中的物品，且 storage 参数为 0、物品未挂到钓组。
- 使用经济事务同时完成会员到期时间更新、`membership_item` 流水写入和物品数量消耗。
- 现有有效会员使用当前到期时间作为基准；过期会员使用当前时间；永久会员 (`membership=2`) 返回失败且不扣物。
- 800 快照通过 `buildRigSnapshot800` 返回更新行或移除 GUID；响应后统一推送 2/1 与物品变更。
- 不新增表或字段；沿用 `profile.membership`、`profile.membership_exp` 与既有请求回执键 `(token_hash, main, sub, call_id)`。
- 客户端基线 4.0.25034，不改变 4/8 args 或响应 args code。

## Testing Decisions

- 真实临时 SQLite：新会员、活跃会员顺延、永久会员拒用、事务失败回滚和非会员类别放行。
- 成功路径断言会员状态、到期时间、流水、物品消耗和 800 快照。
- 永久会员断言失败响应、会员不变和物品保留。
- 通过触发器让流水写入失败，断言会员到期时间与物品同时回滚。
- 现有 dispatch 回执测试覆盖同一 call_id 重放不重复执行副作用。

## Out of Scope

- 不新增会员物品的 wire class 映射或新的发放路径；本改动只修复已存在 category 75 物品的使用语义。
- 不修改会员购买、价格、权益内容或显示文案。
- 不新增表结构或迁移。

## Verification

- `go test ./internal/gameserver -run 'TestMembershipItem' -count=10`：通过。
- 全量测试排除既有 Cafe 推送 flake 后通过。
- `go vet ./...`、`go build ./...`、`go run ./tools/doccheck`、`git diff --check`：通过。