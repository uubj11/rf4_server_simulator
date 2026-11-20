# 补全鱼种搏斗习性组参数

Status: done

## Problem Statement

`fight27StaticsFor` 原来只有官方逐鱼种表（少数鱼种）和 `marine` / `cyprinid`
两个习性组；其余两百多个鱼种全部落到 `fallbackFightStatics` 的小鲫鱼参数：

```text
f14 = 0.5
f15 = 4.0
```

结果是大鱼只有重量和 `f8` 拉力，没有外冲和摆头动作，表现为“像死鱼一样被直接拖回来”。
典型现象：`s.orfe`、`l_salmon`、`malma`、`golets_lev`、`neiva`、`kunja`
等 1～9kg 鱼在 4～20 秒内被收上来，几乎没有搏斗动作。

## Solution

保留官方逐鱼种表为最高优先级，把 `fishGroupStaticsFor` 从两个组扩展到全部
习性组：

```text
marine / apex / salmonid / ambush / cyprinid /
catfish / sturgeon / eel / small / default
```

每个组给出 `f9～f20`、`f5` 每克体力、`f6` 的组值：

- `salmonid`：持续发力 + 短促冲刺；
- `ambush`：受惊爆发；
- `catfish` / `sturgeon`：低速大扭矩；
- `eel`：持续扭动；
- `small`：动作碎但幅度小；
- `apex`：海鱼最高档外冲；
- `default`：未归组鱼种的中性偏强兜底。

组值是按常见体型/行为补的经验值，不是官服逐鱼种真值；拿到官方 14/14
样本后仍由 `officialSpeciesStatics` 逐种覆盖。

### 可选 A/B 层（默认关闭）

为定位“鱼像死鱼”和“中鱼瞬间被全树重建”的问题，代码保留以下默认关闭的
开关；生产默认仍走官方口径：

- `fight_legacy_model`：恢复 3166958 之前的 Burst/Duration/Rest 和 f21
  分阶段张力/时长；配合 `fight_official_strength` 时只恢复节奏，不恢复旧
  f8/f5/f6。
- `fight_official_strength`：legacy 模式下仍使用官方 f8/f5/f6。
- `fight_curve_tension_scale`：只缩放 f21 张力列，默认 `1.0`。
- `fish_f6_scale`：缩放 f6，默认 `1.0`，结果钳制 `[0.05,1.0]`。
- `fight_hook_full_sync`：默认 `1` 保持 14/11 后全树同步；设为 `0` 时仅发
  钩帧，用于 A/B 验证全树是否重建客户端搏鱼目标。
- `fight_phase_scales`：单独启用阶段倍率，不启用 legacy 模型。
- `fish_fight_group_fallback`：默认 `1`；设为 `0` 时未实测鱼种的
  f9~f20、f5 组均值和 f6 组值全部回到旧 fallback 口径，用于整组回滚。

这些开关是诊断/平衡实验入口，不改变默认官方口径；实机配置由
`tunables` 覆盖。

## Testing Decisions

- 逐组钉 `f14/f15`，确保不再出现 `f14=0.5/f15=4`；
- 钉 `riv_rb_trout`、`s.orfe`、`l_salmon`、`catfish`、`sturgeon`、`eel`、
  `gudgeon`、`cetorhinus_m`、`neiva`；
- 钉 `f5` 每克组值和 `f6` 组值；
- 回滚任一组条目，对应断言变红；
- `fish_fight_group_fallback=0` 时未实测鱼种的 f9~f20、f5、f6 全部回到旧口径。

## Out of Scope

- 不声称这些组值是官方逐鱼种真值；
- 不修改官方已落表的逐鱼种参数；
- 不把 A/B 开关设为生产默认值。
