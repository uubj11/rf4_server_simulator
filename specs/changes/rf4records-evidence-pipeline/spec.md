# rf4records 官方周纪录证据管道

Status: ready

## Problem Statement

`companion_bait_fish` 是随初始发布预置的 3.1 万行食性证据，仓库内没有抓取时间、口径、
分服等 provenance（详见 PR #478 的 fish-water-layer-v4 spec Evidence 段，跨 PR 文件不在本树）。
需要一条可溯源、可重复执行的外部证据管道，为水层/食性基线提供第二数据源。

## Solution

- 新表 `rf4records_catch`（迁移 `00061_rf4records_catch.sql`）：一行 = 官方周纪录榜上的
  一条捕获纪录（鱼/饵原文、映射后的 fish_key/bait_id、重量、水域、钓法、日期）。
  主键 `(fish_name_en, bait_name, weight_g, catch_date, waterbody)` 做增量幂等；
  `weight_g` 非空，避免 NULL 在主键里被 SQLite 视为互不相等；`(fish_key,bait_id)` 建索引
  供食性占比聚合。
- `tools/fetch_rf4records.py`：从 rf4records.com 公开 API 抓取，UA 自报、请求间隔 2s、
  支持 `--fish` 补抓与 `--reparse` 只刷新映射；`ensure_table` 按
  `*rf4records_catch.sql` 查找迁移（不硬编码版本号）。
- `tools/rf4records_fish_map.py` / `tools/rf4records_bait_map.py`：鱼名/饵名到本地
  `fish_key` / `item_def.item_id` 的映射；未命中保留空串，不阻塞统计。
- `repos_integrity.RuntimeTables` 登记 `rf4records_catch`：运营数据，非静态资料也非玩家存档。
- 不提交抓取数据；抓取脚本只手动/每周运行，不进入服务端运行时。

## Testing Decisions

- `go build ./...`、`go vet ./...`、`go run ./tools/doccheck` 通过；迁移只新增表，不改变
  现有运行时行为。
- `python -m py_compile` 三个脚本通过；`ensure_table` 用临时 sqlite 连接跑通（迁移重编号后
  仍能找到文件）；同一批输入重复抓取按主键零新增。
- 映射覆盖：源站 252 鱼名 → 本地 `fish_key` 251 个非空（250 对有效一一对应，已知边界
  在 `rf4records_fish_map.py` 头部注释）。
- 不把外网抓取放进 CI；抓取需要外网与时间，不属于单测。

## Out of Scope

- 服务端运行时读取本表：由 `bait-evidence-threshold` 的 `rf4BaitShare` 另行消费。
- 数据再分发/许可：仓库只提交抓取与映射代码，不提交 rf4records 数据。
- 每周抓取调度、重试/退避策略：当前为手动运营命令；后续可另加 wrapper。
