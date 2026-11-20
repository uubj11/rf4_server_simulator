# 工坊换件响应与鱼竿部件快照一致性

Status: done

## Problem Statement

玩家更换配件后再次提交同型号零件，工坊可能报错，无法完成交互。
鱼竿换件后重新打开工坊时，已安装部件与出厂快照不一致，也会导致零件查找异常。

## Solution

已无需修复的同型号订单返回当前装备状态，不扣费、不修改装备、不产生交易流水。
鱼竿快照保留已安装配件的型号、GUID 和耐久，并与部件索引保持一致。

## User Stories

1. 玩家重复提交已完好的零件时，仍能收到可解析的装备状态。
2. 未定价的替代零件和金币零价格订单继续拒绝，不允许免费购买。

## Implementation Decisions

- 快照使用持久化鱼竿部件型号与 GUID，重建状态位时保留耐久；f5 内同品类部件索引与 f0~f2 保持一致，换件事务同时保存索引。旧档读取时重建一致的 wire 索引，不覆盖已购部件。
- 修改 `internal/gameserver/econ_market.go` 的 `hWorkshopOrder` 零费用同型号修理分支。
- 使用现有 `ItemToTree` 和编码器返回原装备；保留正常付费路径的事务和回执机制。
- 响应结构依据 `docs/official-protocol-workshop.md` 的装备对象约定。
- 无表结构、迁移或静态库改动；兼容客户端基线 4.0.25034。
- 回滚为还原该分支与测试，存档无需变换。

## Testing Decisions

- 鱼竿替换手柄后核验回包、重新读取快照、持久化 f5 的型号和 GUID 一致；回滚后会恢复出厂型号，回归测试变红。
- 真实 SQLite + handler + 协议编解码：21/3、21/4 无磨损同型号订单返回原装备类，余额、字段、耐久、流水、推送均不变。
- 期望值为初始余额 1000、流水 0 和初始装备快照；还原实现后返回失败，测试变红。
- 运行工坊回归、全量测试、vet、build、doccheck，并验证客户端交互。

## Out of Scope

不调整不同 call_id 的业务去重或其他非法请求的错误协议。按部件修理与报价见 [按部件修理](../workshop-selected-part-repair/spec.md)。

## Further Notes

[事实] 客户端基线 4.0.25034：零费用请求需要装备对象响应；鱼竿工坊诊断与装备快照需要引用同一已安装部件。结构依据见 `docs/official-protocol-workshop.md` 和 `docs/debugging-workflow.md`。

## Validation

- 工坊定向回归通过；新增用例在修复前按预期失败，修复后通过。
- `go vet ./...`、`go build ./...`、`go run ./tools/doccheck` 通过。
- `go test ./... -parallel 4` 全量通过。
- 客户端实机确认轮子换件及鱼竿换件后重新打开工坊不再报错。
- `TestWorkshopWornReelDirectReplaceSequence` 在临时 SQLite 中覆盖 21/3、21/3、21/4 连续请求，验证对象回包与只扣费一次。
