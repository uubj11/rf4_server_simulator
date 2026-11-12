# 大鱼体力池封顶（优化拉大鱼体验）

## 问题

`fishStaminaForProfile` 里体力池 = 体重 × 2.388 × StaminaScale，**随体重线性膨胀**。
而拉力也随体重线性膨胀，渔轮泄力却有硬上限（商店最强 Furia 30000 = 38.55 kgf）。

于是 > 38 kg 的鱼，鱼拉力恒大于泄力 → 张力被压在泄力上限 → 时效 ∝ 体力池 ∝ 体重：

| 鱼重 | 体力池（StaminaScale 忽略） | 相对 1 kg |
|---|---|---|
| 1 kg | 2,388 | 1× |
| 38 kg | 90,744 | 38× |
| 246.7 kg（`w_catfish` 实测） | 589,111 | 247× |
| 3,125 kg（`cetorhinus_m` 抽样上限） | 7,462,500 | 3,125× |

这是原版被诟病的点，也是"一条大鱼拉半小时到几小时"的直接来源。

## 方案（体力池封顶，不动拉力）

只改体力池，**不动 f8 拉力**：大鱼依然拉力巨大、依然要放线、依然有搏斗感，
只是"血条"不再无限长。

```
stamina = 2.388 × min(体重, capG) × fish_stamina_scale × StaminaScale
capG = tunable "big_fish_stamina_cap_g"，默认 38000（38 kg，= 商店最强泄力对应的鱼重）
      capG <= 0 → 关闭封顶，回到原版行为
```

效果（以 `b_carp`、StaminaScale≈1.71 计）：

| 鱼重 | 原体力池 | 封顶后 | 比例 |
|---|---|---|---|
| 1 kg | 4,083 | 4,083 | 100% |
| 11 kg | 44,915 | 44,915 | 100% |
| 38 kg | 155,172 | 155,172 | 100% |
| 51.9 kg | 211,935 | 155,172 | 73% |
| 246.7 kg | 1,007,381 | 155,172 | 15% |
| 3,125 kg | 12,761,000 | 155,172 | 1.2% |

小鱼（≤ 38 kg）**完全不变**，原版手感保留；超过 38 kg 的鱼，搏斗时长最多等于"38 kg 的鱼"。

## 文件

| 文件 | 改动 |
|---|---|
| `internal/gameserver/fishing.go` | `fishStaminaForProfile` 加封顶；新增常量 `defaultBigFishStaminaCapG` |
| `internal/gameserver/fish_stamina_cap_test.go` | 新增测试：封顶生效 / 小鱼不受影响 / cap<=0 回退原版 |
| `docs/RF4本地服_经验总结.md` | §3.13 补第九节：优化方案与实测口径 |

## 验收

| ID | 断言 |
|---|---|
| A1 | cap=38000 时，246.7 kg 鱼的 stamina 等于 38 kg 鱼的 stamina（同鱼种） |
| A2 | cap=38000 时，11 kg 鱼的 stamina 与未设 cap 完全相同 |
| A3 | cap=0 时，246.7 kg 鱼的 stamina 回到 体重×2.388×StaminaScale 的原值 |
| A4 | `go test ./internal/gameserver -run TestBigFishStaminaCap` 全绿 |
| A5 | `go build ./...` exit 0，部署后日志里大鱼 `体力池` 明显下降 |

## 回退

把 `big_fish_stamina_cap_g` 设为 0（或在 `tunables` 表里写 0），即恢复原版线性体力池，无需回滚代码。
