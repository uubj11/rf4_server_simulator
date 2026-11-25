# 真实稀有度与拟饵覆盖

Status: done

## Problem Statement

判鱼权重、窝料和鱼猎使用本地生成的 `fish_ecology.rarity_tier`，它与静态 `species_rarity` 的真实稀有度在多数鱼种上不一致。同时规则表几乎没有拟饵词条，使用拟饵时可选鱼种过少。

## Solution

鱼生态加载时为每个 `fish_key` 关联静态 `species_rarity.rarity`，判鱼加权、窝料稀有度门和鱼猎 pit 特性统一使用真实稀有度。拟饵规则按公开渔获记录中某鱼实际咬过的拟饵占比补充；占比达到 1% 的鱼种允许对应拟饵，分布过于分散时使用「拟饵」通配。

## User Stories

1. 稀有鱼技能对判鱼权重的加成按真实稀有度 0/1/2 计算，不再按本地合成档位。
2. 窝料和鱼猎行为对稀有条目使用同一真实稀有度。
3. 玩家使用有公开渔获证据的拟饵时，相关鱼种可以进入候选，不再只剩极少数拟饵鱼。
4. 明确不吃拟饵/天然饵的规则仍然生效。
5. 缺失 `species_rarity` 行时按普通鱼处理，不影响旧库启动。

## Implementation Decisions

- `fishEcologyProfile` 新增只读 `Rarity`，查询通过 `LEFT JOIN species_rarity ... COALESCE(r.rarity,0)` 加载，不写回玩家库。
- `fishBiteEnvironmentWeight`、`fishHunt` marine pit 和 `groundbaitGates` 改用 `p.Rarity`；`RarityTier` 只保留既有数据字段，不再参与这三处判鱼。
- 拟饵覆盖依据公开 rf4records 数据集 570,969 条、2026-10-11 至 2026-11-16；按鱼种内拟饵记录占比 ≥1% 纳入，42 个鱼种列具体系列，33 个鱼种因分布分散使用「拟饵」通配。
- 不删除明确禁饵规则；仅移除与实际拟饵记录冲突的「不吃拟饵」限制。
- `legalAlternativeShare=0.05` 仅把既有 0.05 提为常量，不改变数值。
- 不改协议、表结构或迁移，客户端基线 4.0.25034。

## Testing Decisions

- 真实/测试静态库确认 `species_rarity` 的 0/1/2 值可加载，真实稀有度覆盖判鱼、窝料和鱼猎消费路径。
- 规则解析测试确认新增拟饵系列、通配词条和禁饵规则均按预期匹配。
- 更新原有 rare/拟饵替代测试，确保技能只提升已有正兴趣、不绕过禁饵。
- 门禁：`go test ./...`、`go vet ./...`、`go build ./...`、`go run ./tools/doccheck`。

## Out of Scope

- 不提交 570,969 条原始外部记录，不把外部站点评级术语写入服务端配置。
- 不保证每条鱼的全部拟饵组合；只覆盖达到 1% 证据阈值的组合。
- 不改变鱼群聚散、12 小时周期或昼夜禁口规则。

## Verification

- 稀有度相关与拟饵规则测试通过。
- 聚焦测试、vet、build、doccheck 和差异检查通过。
- PR CI 通过。
- 2026-10-11 至 2026-11-16 的 rf4records 数据为本次规则证据；原始数据仍由外部来源提供，不进入仓库。