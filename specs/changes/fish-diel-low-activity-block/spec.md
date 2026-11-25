# 昼夜低活跃禁口

Status: done

## Problem Statement

昼夜曲线来自社区渔获时刻统计，样本少的鱼会被相邻时段平均抬高；图鉴标注为某个半天低活跃的鱼，服务端仍可能在该时段正常咬钩。

## Solution

新增内嵌 `fish_diel_block.json`，记录图鉴评为白天或夜间低活跃的鱼种。对应半天的权重上限设为 0.05；少数没有表项但白天基本不咬的夜行鱼再使用独立硬上限。保留原有四段昼夜曲线的其余行为。

## User Stories

1. 图鉴标为白天低活跃的鱼，白天权重不超过 0.05；夜间恢复原曲线。
2. 图鉴标为夜间低活跃的鱼，夜间权重不超过 0.05；白天恢复原曲线。
3. 未列入禁口表但属于严格夜行名单的鱼，白天仍接近不咬。
4. 禁口表的鱼种键必须存在于生态定义中，非法 day/night 值不得静默生效。
5. 不改变天气、区域、鱼群聚散或饵料接受度。

## Implementation Decisions

- `dielHalf` 把夜段 bit0 映射为 night，晨/昼/昏 bit1..3 映射为 day。
- `fishDielBlockedFor` 读取内嵌表；命中半天时 `fishDielWeight` 取曲线值与 0.05 的较小值。
- 严格夜行名单只覆盖缺少图鉴条目但白天基本不咬的鱼；不替代通用禁口表。
- 数据来源为 2026-11-19 抓取的鱼种图鉴日/夜活跃度评级，原始站点术语不写入运行配置。
- 新增 JSON 使用 LF，避免 Windows CRLF 触发仓库 diff 检查误报。

## Testing Decisions

- 解析完整禁口表，校验所有 fish_key 存在于 `fish_ecology_defs.json`，值仅为 day/night。
- 验证白天低活跃鱼白天权重≤0.05、夜间正常；验证夜间低活跃鱼反向路径。
- 保留昼夜曲线来源/四段曲线测试。
- 门禁：`go test ./...`、`go vet ./...`、`go build ./...`、`go run ./tools/doccheck`、`git diff --check`。

## Out of Scope

- 不把图鉴评级当作官方逐小时概率，不替代后续更可靠的时刻样本。
- 不新增协议、表结构或迁移。
- 不改变未列入禁口表且不在严格夜行名单中的鱼。

## Verification

- 新增禁口表测试通过；聚焦昼/夜曲线测试通过。
- `go vet`、`go build`、doccheck、差异检查通过。
- PR CI 通过。