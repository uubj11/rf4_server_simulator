# 计划：体力消耗调整 + 熟练度加权 + 鱼搏斗自耗

## 目标（验收 ID）

| ID | 期望行为 | 边界/口径 | 涉及文件 | 可观察断言 | 验证命令 |
|---|---|---|---|---|---|
| P1 | 熟练度增量随鱼重、稀有度成正比 | 重量档 0.6/1.0/1.5/2.2/3.0（≤0.2kg / 0.2–1 / 1–3 / 3–10 / >10）；稀有档 1.0/1.2/1.4/1.7（grade1–4）；`prof_weight_scale`、`prof_rarity_scale`=0 时回到旧行为；单次增量封顶 `prof_gain_cap`=1.0 格；结果仍落在 0.1 格 | `skills_ops.go`、`catch_settlement.go` | 42g grade1 → 0.1；22kg grade3 → 0.7；scale=0 → 0.1/0.2 与旧版一致 | `go test ./internal/gameserver -run 'ProfWeight|ProfGain' -v` |
| P2 | 鱼的搏斗自耗可调，默认比原版快 | `fish_fatigue_scale`（0.2–4，默认 1.25）作用在 f6：`f6' = clamp(1-(1-f6)*scale, 0, 0.95)`；scale=1 与官方抓包完全一致 | `fishing.go` | 0.5628 × 1.0 → 0.5628；× 1.25 → 0.4535；× 4 → 0；f6=1（测试鱼）永远 1 | `go test ./internal/gameserver -run 'FishFatigue' -v` |
| P3 | 体力消耗/恢复 5 项按标定值落库 | fight 0.006 / cast 0.006 / regen_cap 0.92 / food_cost 0.3 / regen_per_hour 70 | `data/rf4server.db` (tunables) | 改库后 30s 内生效；`/api/status` 正常 | sqlite 查询 + 实机日志 |
| P4 | 新二进制度量与部署 | 版本号 0.9.57；停服→换 exe→计划任务拉起；四端口 LISTEN | `bin\gameserver.exe` | `go test ./internal/gameserver` 全绿；`/api/status` 与日志版本号一致 | `go test ./internal/gameserver`、`/api/status` |

## 顺序（TDD）

1. 备份：DB（sqlite backup API）+ 待改源码文件（`.bak_20261118`）。
2. 写测试 → 跑 RED（编译通过但断言失败）。
3. 最小实现 → 跑 GREEN。
4. 全量 `go test ./internal/gameserver`。
5. 构建 exe（版本号 0.9.57）+ 停服部署 + 实机核对。
6. 经验总结追加 §3.34.44。

## 不做

- 不动 `awardSkillProficiency`（制作/挖饵的熟练度路径）与 `awardCraftProficiency`。
- 不动 f5 体力池公式、不动 f20 挣脱概率。
- 不改客户端、不新增协议字段。
