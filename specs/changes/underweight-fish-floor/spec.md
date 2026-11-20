# 未达标档下界取鱼种自然最小体重

Status: done

## Problem Statement

档位比例采样开启时，「未达标」档的下界写死 1 g，且档内是对数均匀采样。
达标线 500 g 的鱼种会有约 37% 的未达标个体落在 10 g 以下，产生不合常理的
极小个体。

## Solution

- 有鱼种自然最小体重（`species_weight.min_g`）且低于达标线时，未达标档
  下界直接采用 `min_g`。
- `min_g` 缺失（静态库 252 行中 207 行为 NULL）或 `min_g` 越界（≥ 达标线）
  时，下界退回达标线的 15%，钳到 [1, 50] g，与 `sampleWeightDB` 的
  `belowGradeFloorFromVal` 兜底共用同一口径。
- 下界在任何情况下都不超过达标线，未达标档不会消失。

## User Stories

- 钓到未达标个体时，重量不低于该鱼种的自然最小体重或 15% 兜底下界，
  不再出现个位数克数的成年鱼。

## Implementation Decisions

- `gradeRatioModel`（`internal/gameserver/fishing.go`）计算未达标档下界时
  优先取 `min_g`，否则用 `belowGradeFloorFromVal(达标线)`。
- 兜底口径集中在 `belowGradeFloorFromVal`（15%、钳 [1,50] g），两个调用点
  共用，避免口径漂移。

## Testing Decisions

- `TestGradeRatioBelowBandHonorsSpeciesMinWeight`：有 `min_g` 时未达标个体
  全部 ≥ `min_g`（变异验证：删采纳行后 FAIL）。
- `TestGradeRatioBelowBandFallsBackToProportionalFloor` 与
  `TestGradeRatioBelowBandGuardsMinAboveLine`：`min_g` 缺失/越界时下界 =
  15% 兜底钳 50 g，未达标档存在。
- `TestGradeRatioBelowBandUsesMinGBelowFloor`：`min_g` 低于兜底时仍取
  `min_g`。
- `TestSampleWeightDBLoadsSpeciesMinG`：从 `species_weight` 插入真实
  `min_g`，验证 `sampleWeightDB` 经缓存/SQL 读取后，未达标个体 ≥ `min_g`。
- `TestSampleWeightDBFloorsBelowBandWhenMinGMissing`：端到端抽样验证
  `min_g` 缺失鱼种的未达标个体 ≥ 兜底下界（期望值为独立常量，不复用被测
  函数）。
- `TestBelowGradeFloorFromVal`：兜底函数边界值表。

## Out of Scope

- 不调整档位占比参数（合格/奖杯/蓝冠比例沿用现有 tunables）。
- 不修改 `species_weight` 静态数据本身。
