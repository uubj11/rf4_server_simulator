# 切鱼（craft_bait_cut_fish）改造：消耗渔获 + 按出肉率连续计算出块

## 背景与现状

- 配方 `craft_bait_cut_fish`：`materials_json = []`（零材料），产物 `fish_pieces`（cat 27）。
- 服务端 `craft_ops.go` 对零材料配方强制 `times = 1`，并注释"尚未接入鱼获/熟饵等消耗"。
- 当前结果：切鱼不消耗任何鱼，每次固定产出 1 件（30 块），可无限重复。玩家反馈"大鱼切几块就没了"不成立，
  真正的问题是**根本没有鱼参与**。
- 现成能力：`keepnet-fish-craft-material` 已实现"鱼护渔获当材料"（`consumeKeepnetForDemandTx`、
  `consumeKeepnetEntryTx`、`KeepnetGet/KeepnetGetTx`、`KeepnetEntry.WeightG`）。
- 客户端风险：切鱼配方在客户端本地定义里材料为空，UI 不保证提供"放鱼槽"。因此**不能依赖客户端传鱼槽**。

## 方案（不改数据，只改服务端行为）

对 `craft_bait_cut_fish` 单点特判：

1. **取鱼**：先看材料槽里有没有指向本 `uid` 鱼护条目的 GUID（`craftSlot.GUID` 命中 `keepnet_items.echo_guid`）；
   命中就用那条；没有则自动按 `ts` 升序取鱼护里**最旧**的一条。鱼护为空 → `803(false)`，不产出、不扣鱼、不写流水。
2. **块数**：按该鱼 `WeightG` 用连续函数算出块数（见下），产出数量 = 块数，取代原来的固定 30 块。
3. **消耗**：与产物入库同一经济事务内执行，复用 `consumeKeepnetEntryTx`（删条目 + 镜像行 + 槽位 + 钓组关联）。
4. **一致性**：事务内按 GUID 复核该条目仍存在且重量未变；不存在（并发被消费）→ 整体回滚 `803(false)`。
5. **一次一条**：`times` 恒为 1，请求里的批量 qty 不放大产出。

### 块数公式（连续函数，不分档）

```text
pieces = round( weightG * permille / 1000 / 7 )      // 每块 7g
permille = 280 + 170 * min(weightG, 25000) / 25000   // 出肉率 280‰ → 450‰
pieces < 1 时钳到 1
```

含义：整鱼出肉率按 28%（小鱼、头骨内脏占比高）随体重线性升到 45%（≥25kg 封顶）；
20.9kg 的历史最大渔获约出 1260 块（≈8.8kg 鱼肉），块数随重量严格单调不减。
全程整数运算（`weightG*permille` 最大约 4.5×10⁷，无溢出），避免浮点误差。

| 代表重量 | 出肉率 | 块数 |
|---|---|---|
| 100 g | 28.0% | 4 |
| 300 g | 28.2% | 12 |
| 600 g（中位） | 28.4% | 24 |
| 1 kg | 28.7% | 41 |
| 1.578 kg | 29.1% | 65 |
| 5 kg | 31.4% | 224 |
| 10 kg | 34.8% | 497 |
| 15 kg | 38.2% | 819 |
| 20.9 kg（历史最大） | 42.2% | 1260 |
| 25 kg 以上 | 45.0% | ≥1607 |

## 实施计划表

| ID | Required behavior | Exact API / default / boundary | 涉及文件 | Observable assertion | Repository-native validation |
|---|---|---|---|---|---|
| A1 | 重量→块数纯函数 | `fishPieceCountForWeight(weightG int64) int64`；`permille = 280 + 170*min(weightG,25000)/25000`；`pieces = (weightG*permille+3500)/7000`；`<1` 钳到 1；非正数返回 1 | `craft_cutfish.go`（新） | 21 个精确点位 + 0~50kg 每 50g 单调性抽样 + 出肉率 27%~46% 区间断言 | `go test ./internal/gameserver -run TestFishPieceCountForWeight` |
| A2 | 切鱼消耗 1 条鱼并产出块数 | 槽内 GUID 优先；无槽取 `ts` 最小条目；产出 `qty == fishPieceCountForWeight(entry.WeightG)` | `craft_ops.go`、`craft_cutfish.go` | 制作后 `keepnet_items` 该条消失；`item` 表 `fish_pieces` 数量 = 块数 | `go test ./internal/gameserver -run TestCraftCutFishConsumesKeepnet` |
| A3 | 鱼护为空必须失败 | 返回 `803.f0=0`，无产物、无流水、鱼护不变 | `craft_ops.go` | 响应 f0=0；`txn` 无新增 | `go test ./internal/gameserver -run TestCraftCutFishWithoutFishFails` |
| A4 | 批量请求不放大 | `req.Qty=1000` 仍只消耗 1 条、产出该鱼块数 | `craft_ops.go` | 只少 1 条鱼；块数等于单条 | `go test ./internal/gameserver -run TestCraftCutFishIgnoresBatchQty` |
| A5 | 他人渔获不被采用 | 槽位 GUID 不属于本 uid → 跳过，回落取自己的最旧一条；仅本 uid 条目可被消耗 | `craft_cutfish.go` | 别人的鱼仍在；自己的鱼被切 | `go test ./internal/gameserver -run TestCraftCutFishIgnoresForeignKeepnetFish` |
| A6 | 全量回归与编译 | — | 全仓库 | 全绿 | `go test ./...` + `go build ./cmd/gameserver` |

## 不做的事

- 不改 `recipe.materials_json`（客户端本地配方仍为空，避免 UI/校验不一致）。
- 不动其他鱼类加工配方（蟹肉、鱼片、活饵）的既有语义。
- 不动 `fish_pieces` 物品定义与堆叠上限。

## 交付

- 编译产物替换 `bin\gameserver.exe`，重启 launcher，`/api/status` 校验。
- 经验总结追加"切鱼机制：从零材料占位到按重量出块"一节。

## 实施结果（2026-11-13 09:40）

| ID | 状态 | 证据 |
|---|---|---|
| A1 | passed | `TestFishPieceCountForWeight`：22 个边界点（负数/0/99/100/299/300/…/20901/50000）逐点通过 |
| A2 | passed | `TestCraftCutFishConsumesKeepnet`：1578g → 28 块，条目删除，背包 `fish_pieces`=28 |
| A3 | passed | `TestCraftCutFishWithoutFishFails`：803.f0=0，背包 0，`txn` 0 条 |
| A4 | passed | `TestCraftCutFishIgnoresBatchQty`：qty=1000 → 301g 出 8 块，鱼护剩 0 条 |
| A5 | passed | `TestCraftCutFishIgnoresForeignKeepnetFish`：他人的鱼仍在，自己的 1578g 鱼被切 |
| A6 | passed | `go test ./... -count=1` exit 0；四件套重编 0.9.28，gameserver/gateway/launcher/seed 全部替换 |

实现中踩到的两个坑：

1. **产出数量必须走 `prepareNewItemWithCount`**。`prepareNewItem(units)` 里的 units 是「件」，
   而 `fish_pieces` 的物品定义自带每件 30 块，首版按它实现直接得到 840 块（28×30），
   由 `TestCraftCutFishConsumesKeepnet` 抓出后改为精确计数。
2. **既有用例需要同步语义**。`TestCraftBatchCapsAndSlotScaling/零材料配方限制1份` 原先把
   `craft_bait_cut_fish` 列为"纯零材料配方应成功"，改造后它必须消耗渔获，已从该列表移除，
   改由 `craft_cutfish_test.go` 覆盖；`craft_crushed_boils` 仍保留原断言。

未覆盖的缝隙（如实记录）：事务内并发复核依赖 `consumeKeepnetEntryTx` 返回 `ok=false` → 整体回滚，
单线程测试造不出并发，未写并发用例；客户端切鱼 UI 是否给"放鱼槽"未实机验证，
无槽时走"最旧一条"，玩家无法自选切哪条。

## 第二版：分档 → 连续函数（2026-11-13 09:51）

触发：第一版 9 档在 1.5kg 以上出现「档位跨度大、大鱼边际产出低」（≥20kg 封顶 220 块，
10~20kg 也有 150 块），玩家要求"不往原版靠、往实际靠，不能出现一条大鱼才切一点，划分再细一点"。

改动：删掉 `switch` 分档，改成出肉率连续函数（见上「块数公式」）；测试从"档位边界点"改为
"精确点位 + 全量单调性 + 出肉率区间"三重断言。

| ID | 状态 | 证据 |
|---|---|---|
| A1 | passed | `TestFishPieceCountForWeight`：21 个精确点位（-500/0/1/7/50/100/200/300/500/600/1000/1400/1578/2500/5000/10000/15000/20901/25000/30000/50000）+ 0~50kg 每 50g 单调性 + 出肉率 27%~46% 区间 全通过 |
| A2 | passed | `TestCraftCutFishConsumesKeepnet`：1578g → **65 块**，条目删除，背包 `fish_pieces`=65 |
| A3 | passed | `TestCraftCutFishWithoutFishFails`：803.f0=0，背包 0，`txn` 0 条 |
| A4 | passed | `TestCraftCutFishIgnoresBatchQty`：qty=1000 → 301g 出 **12 块**，鱼护剩 0 条 |
| A5 | passed | `TestCraftCutFishIgnoresForeignKeepnetFish`：他人的鱼仍在，自己的 1578g 鱼被切出 65 块 |
| A6 | passed | `go test ./... -count=1` exit 0（gameserver 79.3s）；四件套重编 **0.9.29**；`/api/status` current_version=0.9.29、hosts=local、last_error 空、两进程 running |

第二版踩到的坑：`-ldflags "-X Version=..."` 必须写全 importpath
（`rf4server/internal/buildinfo.Version=...`），否则 linker 报
`-X flag requires argument of the form importpath.name=value`，四个产物一个都编不出来。

回滚：第一版程序在 `备份\exe\*_before_curve_20261113_0951.exe`（0.9.28）。
