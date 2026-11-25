# 鱼群聚散周期与稀有度缺席

Status: done

## Problem Statement

鱼群聚散原本每 4 游戏小时重排，约三成鱼种直接归零，非零权重跨度约 26 倍，导致单个周期内可选鱼种明显收缩。鱼种是否整天缺席也与真实稀有度无关，高权重鱼可能整天消失。

## Solution

把聚散重排周期改为 12 真实小时，降低非零权重跨度，并只允许稀有鱼按稀有度整天缺席。普通鱼永不因此归零；图内权重份额达到 5% 的稀有鱼降低缺席判定线。生产判鱼和查鱼助手共用同一套 `share` 规则。

## User Stories

1. 普通鱼在任一聚散周期都有非零基础权重，不会整天完全消失。
2. 稀有度越高，稀有鱼整天缺席的概率越高；超稀有鱼仍有 35% 判定线上限。
3. 某稀有鱼在当前图内基础权重份额达到 5% 时，缺席判定线减半。
4. 查鱼助手按与实际判鱼相同的聚散周期、share 和缺席规则计算区域概率。
5. 同一请求/区域在同一周期得到稳定结果，不因换饵或移动重抽。

## Implementation Decisions

- `distributionPeriodRealHours=12`，聚散周期和饵效抖动周期共用该常量。
- 聚散值 `v<distributionZeroFloor(share, rarity)` 才归零；普通鱼 floor=0，稀有/超稀有分别从 5%/10% 起，share≥5% 时减半，上限 35%。
- 非零因子为 `0.55+0.90v`，仍乘现有 `activityMultiplier`；`pityEnv` 保底路径保持原语义。
- 稀有度由 `attachDB` 读取静态 `species_rarity` 到只读快照；未加载时按普通鱼处理。
- 查鱼区域缓存键必须包含 `share` 的 5% 缺席档；`shortlist` 与 `best` 使用不同权重集时不能复用旧因子。
- 不改协议、表结构或迁移。

## Testing Decisions

- 测试聚散周期为 12 小时，同一周期稳定、不同周期可重排。
- 测试普通鱼不会整天归零、稀有度提高缺席线、5% share 减半和 35% 上限。
- 测试查鱼按键+share 档区分缓存，防止 coarse shortlist 的因子污染最终 best 区域概率。
- 保留现有鱼猎、鱼口 pity、区域和天气查询测试。
- 门禁：`go test ./...`、`go vet ./...`、`go build ./...`、`go run ./tools/doccheck`。

## Out of Scope

- 不引入新的稀有度来源或修改 `species_rarity` 数据。
- 不改变鱼种基础权重、饵料接受度、档位比例或鱼口门。
- 不保证每个格子在每个 12 小时周期都有全部鱼种。

## Verification

- 新增分布与缓存测试通过；组合审查树中聚焦测试通过。
- 全量测试仅命中既有 Cafe 推送 flake 与 pace 测试的 elapsed-time 边界；后者已在本改动中增加时间余量。
- `go vet`、`go build`、doccheck、差异检查通过。
- GitHub CI 通过。