# 计划：每图钓鱼熟练度等级制（稀有鱼 / 偏重个体加权）

## 目标（验收 ID）

| ID | 期望行为 | 边界/口径 | 涉及文件 | 可观察断言 | 验证命令 |
|---|---|---|---|---|---|
| M1 | 每张地图单独记熟练度，钓得越多等级越高 | `map_proficiency(uid,loc,catches,last_ts)`，每次钓获 +1；等级 = 累计渔获数落在 `map_prof_steps` 的第几档（默认 `0,10,30,70,150,300,600,1200,2400,4800` → Lv0–Lv9）；loc 取 `level_xxx` 地图键 | `internal/store/migrations/00066_map_proficiency.sql`、`repos_integrity.go`、`map_proficiency.go` | 0 尾→Lv0；10 尾→Lv1；9 尾→Lv0；4800 尾→Lv9 | `go test ./internal/gameserver -run MapProf -v` |
| M2 | 等级越高，稀有 / 超稀有鱼种权重越高 | 权重 ×`1 + rare_step×level×rarity`（rare_step 默认 0.05）；rarity 0（普通）恒 1.0；level 0 或 `map_prof_enabled=0` 恒 1.0 | `map_proficiency.go`、`fishing.go`（`rollFishFromDBWithRules`） | Lv0 时权重表与旧版逐项相等；Lv9 时 rarity1 ×1.45、rarity2 ×1.90；普通鱼不变 | `go test ./internal/gameserver -run MapProf -v` |
| M3 | 等级越高，偏重个体概率越高 | 每口咬钩先抽一次体重，再按 `min(level×heavy_step, 0.9)` 概率重抽一次取较大值（heavy_step 默认 0.02）；Lv9 → 18% | `map_proficiency.go`、`fishing.go` | heavy 概率 0 时不重抽（采样路径与旧版一致）；概率 1 时结果 = max(两次抽样) | `go test ./internal/gameserver -run MapProf -v` |
| M4 | 跨级有播报 | 跨级时给在线会话推系统聊天：`「克马羚诺也湖」钓鱼熟练度 Lv.3（累计 41 尾）`；地图名走 `loc_alias` | `catch_settlement.go`、`map_proficiency.go` | 9→10 尾推一次、10→11 尾不推 | `go test ./internal/gameserver -run MapProf -v` |
| M5 | 关掉即回到原版，且不影响攻略查询 | `map_prof_enabled=0`：不写库、不加权、不播报；uid=0（攻略/查询链路）不参与 | `map_proficiency.go`、`fishing.go` | enabled=0 时 `mapProfWeightMult` 恒 1、无表写入 | `go test ./internal/gameserver ./internal/store ./internal/seed` |

## 顺序（TDD）

1. 读现状：`rollFishFromDBWithRules` 权重链、`settleCatchOnceWithQualification` 事务、`PlayerTables`。
2. 写 `map_proficiency_test.go` → 跑 RED（编译通过、断言失败）。
3. 最小实现：迁移 + 表登记 + 纯函数（等级/权重/重抽概率）+ 接线（发鱼权重、结算计数、跨级播报）。
4. 跑 GREEN，再全量 `go test ./internal/store ./internal/gameserver ./internal/seed`。
5. 部署（停游戏才部署）+ 实机核对。
6. 经验追加 `docs/RF4本地服_经验总结.md` 的 §3.34.48。

## 不做

- 不改客户端、不加协议字段（等级不写进 801/310 卡面，只用聊天提示）。
- 不动 `fish_hunt_state` 双轨保底那条抽样曲线（hunt 开关默认关）。
- 不改 `map_fish_roster` 静态权重（玩家进度只走运行时倍率）。
