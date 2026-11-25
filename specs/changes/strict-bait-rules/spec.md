# 严格鱼饵清单（bait_exact_only）

Status: done

## Problem Statement

默认鱼饵规则仍是宽泛的互斥/偏好模型，允许证据、同食性杂口和多饵组合把不在攻略白名单中的饵放行。开启严格模式后，玩家需要明确的“每种鱼只吃规则允许的饵”语义，且不能因为 companion 证据或组合饵中的一项漏配而改变白名单边界。

## Solution

新增 tunable `bait_exact_only`，默认 `0`。开关开启时只查询随包发布的 254 条精确规则清单：

- 规则按鱼名归一化查找，兼容“鱼”字后缀；
- 一款饵必须命中 allow 清单；组合饵的每一项都必须在 allow 清单中；
- deny 与“底钓只吃”逐项生效；
- 不再使用 companion tier、rf4records 占比或同食性杂口兜底；
- 没有精确规则的鱼在严格模式下不参与判鱼。

开关关闭时仍走原有 `fishing_bait_rules.txt`、轮换、证据和权重逻辑，不改变默认行为。

## User Stories

1. 玩家开启严格模式后，只有攻略明确列出的饵能让对应鱼种咬钩。
2. 多钩组合中只要存在一处不在白名单的饵，就不把该鱼错误地判给整组。
3. 关闭严格模式后，沿用原有鱼饵规则和后台覆盖，不被精确清单隐式影响。

## Implementation Decisions

- `internal/gameserver/fishing_bait_rules_strict.json`：随包发布的精确规则快照，共 254 条，字段为 fish/allow/deny/bottom/condition。
- `internal/gameserver/fishing_bait_strict.go`：解析规则、鱼名归一化、严格匹配与组合饵逐项校验；规则缺失或条件不满足时直接剔除该鱼。
- `internal/gameserver/fishing_bait_rules.go`：`loadFishNames` 增加严格规则鱼名覆盖；`filterFishBaitSet` 仅在 `bait_exact_only > 0` 时走严格分支。
- 不新增协议字段、数据库表或迁移；规则数据随二进制嵌入。
- 严格模式仍保留客户端已上报的动作、海图区域、船和钓组条件；无法获得证据时按既有中性策略处理。

## Testing Decisions

- 规则覆盖：`TestStrictBaitRulesCoverage` 校验 254 条规则、allow 非空和规则查找正常。
- 行为层：`TestStrictExactBaitModeIgnoresEvidenceAndCombos` 使用真实 SQLite/判鱼链，验证 companion 证据不能越级、未列饵和未知饵不能出鱼、组合饵任一不匹配即整组拒绝，并保留合法白名单饵。
- 默认关闭：完整 Go 测试覆盖原有 bait rules / evidence / rotation 行为，确保开关为 0 时仍走旧路径。
- 期望值来源：严格清单来自后台 `fish_bait_override` 导出的精确 allow 清单；测试使用真实静态定义，不用被测函数回算 allow 集合。
- 回滚后变红：关闭严格分支会让未列饵/组合饵越级出鱼；恢复默认路径会改变完整测试中的原有规则结果。

## Out of Scope

- 不把严格清单转成运行时数据库覆盖，也不改后台管理 UI。
- 不调整普通模式证据阈值、rf4records 占比、轮换策略或鱼群权重。
- 不保证 254 条规则逐条实机验证；外部数据新增鱼种仍需更新嵌入快照。
