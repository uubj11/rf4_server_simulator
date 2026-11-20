# 昼夜活跃度曲线（diel_curve）

Status: done

## Problem Statement

鱼的昼夜活跃度原先只有 `fish_ecology.diel_mask` 一个四位位图：命中时段权重 1.0、未命中 0.35。
位图无法表达窗口期——峰值时段最容易出、过了峰值逐渐变少但仍会出，而现在只有「开」和「惩罚 0.35」
两档，同一时段的所有鱼要么满权重、要么统一降到 0.35，无法体现鱼种之间的差异。

## Solution

1. 新增 `fish_ecology.diel_curve`（四段权重字符串，`[夜, 晨, 昼, 昏]`），`fishDielWeight` 优先按段取系数；
   曲线为空时回退旧位图，保持兼容。
2. 曲线数据来源分三级，逐级回退：
   - `rf4info`：社区公开抓取库 `ghoomt03-sketch/rf4info` 的 `public_data/core.sqlite3`，
     表 `spot_activity_events`。仅取 `TRIM(game_time) <> ''` 的记录，按四段计数；
     单鱼样本数至少 5 时直接使用自身分布。各段取 `(count + 2) / (peakCount + 2)`，
     再钳到 `[0.08, 1.0]`，因此峰值固定为 1.0。
   - `gamekee`：GameKee 鱼种页的「日间活跃度 / 夜间活跃度」表情图标（笑/平/愁 = 高/中/低）。
     按日档与夜档的 8 个可能组合归档；当前数据实际覆盖 7 组。无 rf4info 自身样本的鱼，
     取同组合内 rf4info 实测鱼的平均四段形状。
   - `global`：两处都没有的鱼使用全体实测形状的全局均值 `[1.0, 0.67, 0.88, 0.68]`。
3. `diel_mask` 同步由曲线派生（权重 ≥0.5 的段置位），保证只读位图的旧路径不倒退。
4. `profile_version` 升到 5，让线上旧档案按 `WHERE fish_ecology.profile_version < excluded.profile_version` 刷新。

当前 253 个鱼种全部写入曲线：`rf4info` 120 条、GameKee 同组均值 111 条、全局均值 22 条。
资产中的 `diel_source` 只用于标记来源，运行逻辑忽略该字段。

## User Stories

1. 作为玩家，我在鳗鱼、江鳕这类夜行鱼的白天几乎钓不到，夜里稳定出货。
2. 作为玩家，我在欧鳊、梅花鲈这类鱼的夜间最容易爆护，白天偶尔也能碰到。
3. 作为玩家，我在葛氏鲈塘鳢这类日行鱼的白天最容易出货，夜里明显变少。

## Implementation Decisions

- 四段划分沿用 `fishActivityForHour`：夜 20–05、晨 05–09、昼 09–16、昏 16–20。
- 曲线是**乘性权重**，参与 `fishBiteEnvironmentWeight` 的 `EcologyInterest` 分组与窝料 `groundbaitGates`，
  不改变门限结构。
- 最低权重取 0.08 而非 0：低活跃段仍允许偶发记录，不会让该鱼在整段内彻底消失。
- `diel_curve` 为空时按 `diel_mask` 返回 1.0 / 0.35，供旧档案、手工清空或回滚使用。
- `diel_curve` 与 `diel_mask` 同时下发，读位图的旧代码路径行为不变。

## Testing Decisions

- `TestFishDielCurveWeightsParsing`：空串、非四段、非法 JSON 返回 nil，合法值按 [0,1] 钳制。
- `TestFishDielWeightUsesCurveAsWindow`：曲线存在时按段取值。
- `TestFishDielWeightSegmentBoundaries`：逐一覆盖夜/晨/昼/昏边界、跨日归一与负小时。
- `TestFishDielWeightFallsBackToMask`：无曲线时仍按位图 1.0 / 0.35。
- `TestFishDielCurveAssetMatchesDerivedMaskAndSources`：253 条资产都有曲线和来源；曲线在
  `[0.08,1.0]`、峰值为 1、`diel_mask` 与 ≥0.5 派生规则一致，并固定三类来源数量。
- `TestFishEcologySeedsSpeciesAndMatchesWaterLayers`：旧 profile_version 行升级时同时恢复 `diel_curve`。

## Out of Scope

- 不修改 `fish_distribution` 的硬编码族名单（独立路径，受 `fish_distribution_enabled` 控制）。
- 不调整 `fish_query_regions` 的分布系数口径。
- 不引入温度/气压与昼夜的交互项。

## Further Notes

复核时直接从 `public_data/core.sqlite3` 统计：

```text
SELECT fish_id, game_time
FROM spot_activity_events
WHERE TRIM(game_time) <> '' AND fish_id IS NOT NULL AND fish_id <> '';
```

当前库得到 9,957 条活动记录，其中 7,125 条带游戏时刻、覆盖 167 个鱼种；
样本数 ≥5 的鱼种为 120 个，与资产中的 `diel_source=rf4info` 数量一致。
GameKee 图标只决定鱼种归组，具体四段形状仍由 rf4info 实测样本提供。
