# 中鱼确认瞬间扣饵打断搏鱼 ctor —— 扣饵挪到搏鱼终局之后

Status: done

<!-- 状态取值：draft | ready | in-progress | done | cancelled
     完成时原地改这里，不要归档、不要搬家、不要另开第二份。 -->

## Problem Statement

钩上挂天然饵（沙蚕 fb_nereis 等）后，**每次中鱼必然秒脱**（f0=2，中鱼后
50~150ms）。2026-11-16 实机抓包（logs/gameserver.log 00:27~01:12 三个会话，
完整字节）统计：带饵钩 **9/9 秒脱**（铁板 rig_type=50 ×7、海钓沉底 ×2）；
无饵钩 5 成 3 脱。换拟饵、重抛、重启均无法摆脱——扣饵写 rebait:0 记忆键，
每竿自动重挂，形成"挂饵→秒脱→耗饵→再挂"死循环，玩家被锁死在带饵状态。

机制链【实证，完整抓包解码】：

1. 客户端 14/11 确认中鱼 → 进入搏鱼态，**ctor 一次性读取全部挂载态并锁定
   整场**（既有已知结论，handlers_game.go 中鱼帧注释）。
2. 服务器在 14/11 处理中调用 `consumeHookedBait` 扣饵：`f5` 删除饵组件
   （removeConsumedRigReferenceTx）→ `pushBaitChange` 推 **4/21（饵消失）+
   4/22（竿树重建帧）**。该 4/22 是普通物品树推送，flags = 定义基值
   0x4A04（缺 0x400 挂载运行位）——正好落进 ctor 的挂载态读取窗口。
3. 客户端 ctor 读到"钓组刚被改（饵没了+竿挂载位丢失）"→ 钩组/传动链构造
   失败 → 脱钩时刻 14/8 快照实证 **f2(鱼拉力均值)=0、f4(线完好系数)=0**、
   f19 体力几乎未耗（钩先掉）。
4. 钩保持从起点 1−27.f20≈0.00023 直接衰减 → f0=2 秒脱。
5. 扣饵事务写 `rod_setting rebait:0`（bait_consumption.go:691）→ 下一竿
   `reloadBaitForCast` 自动重挂 → 循环。

对照（客户端反编译 + 官服抓包既有结论）：
- 官方中鱼确认到进搏鱼态之间**零推送**：`14/11 → 147B 钩帧 → 14/8×3 →
  大帧同步`，扣饵不在其中。
- `pushBaitedFish` 的注释早已记录同类事实（bait_consumption.go:756）：
  14/14 时刻扣饵推送会让客户端"立即看到空钩，随后让鱼以 14/12 f0=1 离场"
  ——于是扣饵被挪到 14/11；但 14/11 仍落在 ctor 窗口内，只是换了个打断点。
- 拟饵（无天然饵）中鱼同样有 mid-ctor 推送（扣"拟饵磨损"，148B 容器帧
  flags 0x2004200 同样缺 0x400），sid=1-old 两次无饵秒脱与之同窗。

## Solution

扣饵（consumeHookedBait）从 **14/11 中鱼确认**挪到**搏鱼终局**之后执行：
14/12 跑鱼（f0=1/2/8 及未知帧分支）、14/5 入护、14/6 放生、14/4 收竿结算。
四个出口共享 `txn(uid,'bait_bite',FishGUID)` 幂等键，先到先扣、重复调用
无害；玩家断线中途退出时饵不扣、留待下一尾（可接受）。

玩家表现：挂沙蚕中鱼后正常搏鱼（f4≈1.0、f6 有脉冲、体力赛跑决胜），
脱钩/入护后左上角饵条消失、下一竿自动补同种饵（rebait 循环保留）。

## User Stories

1. 作为挂饵海钓玩家，我中鱼后不再秒脱，可以正常搏鱼起鱼。
2. 边界：鱼跑掉/入护/放生/收竿四条终局路径饵都恰好扣一次（不吃双份、
   不漏扣）；断了线（17/9）或断线重连没发终局命令时饵保留在钩上。
3. 边界：嘬饵未中钩（finishNibbleHook）的轻耗与推送不变——该路径无搏鱼
   ctor，现有行为合法。
4. 边界：rebait:0 记忆键的写入随扣饵挪到终局，循环体感不变。

## Implementation Decisions

- `internal/gameserver/handlers_game.go`：
  - 14/11 处理器删除 `consumeHookedBait` 调用（保留日志说明扣饵已挪后）；
  - 14/12 的 f0=1/2/8 分支与未知帧分支：在跑鱼播报之后、
    `settleFightReelWearState` 之前，`hooked && pending != nil` 时调用
    `consumeHookedBait`；
  - 14/9 海钓 `needsConfirmation` 与多钩额外鱼 14/11 同样只确认、不扣饵；
  - 多钩额外鱼若以已确认状态从 14/12 离场或被拒收，按其 `HookConfirmed`
    在移除队列前扣掉对应钩位的饵；
- `internal/gameserver/econ.go`：`fishingKeep`（14/5）与 `fishingRelease`
  （14/6）在清 `st.Pending` 前扣饵（`st.Hooked` 为真时）；
- `internal/gameserver/multi_hook.go`：`settleExtraOnReelIn` 在额外鱼终局
  先扣饵，再尝试结算，避免结算失败时把挂载饵留成可循环使用；
- `internal/gameserver/progress_ops.go`：`fishingEndSettle` 的 `hasFish`
  分支开头扣饵；
- 资格/统计不受影响：QualBait 咬钩快照在掷鱼时即固化（fishing.go:2592），
  不依赖饵实例存活；BaitGUIDs 快照随 pending 存活到终局。

## Testing Decisions

- 新增 `bait_consume_timing_test.go`：
  - `TestHookConfirmDoesNotConsumeOrPushBait`：带饵钩 14/11 确认后，钩上
    饵实例仍在、探针未收到 4/21/4/22（回归：mid-ctor 推送消失）；
  - `TestEscapeConsumesBaitOnceAfterFight`：14/12 跑鱼后饵被扣且 4/21/4/22
    在播报之后到达；同 FishGUID 重复 14/12 不再扣；
  - `TestKeepConsumesBaitOnce`：14/5 入护路径扣饵一次，随后的 14/4 不重复扣；
  - `TestMarineStrikeDoesNotConsumeMountedBait`：14/9 确认瞬间不扣饵，14/12 终局扣一次；
  - `TestExtraFishHookConfirmDoesNotConsumeBait`：额外鱼 14/11 不扣饵，14/4 结算扣一次；
  - `TestExtraFishEscapeConsumesMountedBait`：额外鱼 14/12 终局扣自己的饵，不误扣主鱼饵。
- 既有测试回归：`selected_hook_test.go`（直接调 consumeHookedBait 的用例
  不受调用点迁移影响）、`qualification_settlement_test.go`（快照语义）、
  嘬饵/补饵族测试原样通过。
- 门禁：`go test -count=1 ./...`、`go vet ./...`、`go build ./...`、`doccheck`；实机回归：
  挂沙蚕中鱼 → 正常搏鱼入护；左上角饵条在终局后消失、下一竿自动补饵。

## Out of Scope

- 搏鱼期 4/22 周期帧缺钩/拟饵树、主线/竿坯树缺 0x400/0x800 运行位
  （官方周期帧 2133B 恒含全树）——独立推进；
- 全树把背包报废拟饵 3b71c01b（durability=0.0、rig_guid=None）当挂件推送
  （f5/rig_component 挂的是 443b95b8）——树构建按 def 反查的实例选择缺陷，
  独立推进；
- rebait 循环的客户端取消出路（卸饵不写 rebait 键 / 显式清除命令）；
- 14 状态 op43（eeajjmkbjbi 读位）解码。

## Further Notes

- `[事实]` 2026-11-16 完整抓包：带饵钩中鱼帧序列 `4/21(26B) → 4/22(竿树
  773/871B, flags 0x4A04) → 4/22(裸钩 147B, 0x5600) → 4/22(全树 2243/2343B)
  → 4/22(721B 钓组部件链)`；无饵钩为 `4/22(148B 容器, 0x2004200) → 147B →
  2429B`。两条带饵秒脱（piksha 57ms、lampris_g 78ms）的 14/8 快照 f2=0、
  f4=0、f19=满值；对照入护鱼开局 f2=鱼重、f4=1.0。
- `[事实]` 官方中鱼序列无任何 4/21/竿树重建推送
  （logs/captures/official_trace/README.md 关键事实 5）。
- `[推断]` 0x400=挂载运行位缺失与 ctor 判定翻车的因果，与 11-12/11-13 两
  份脱钩方案的 ×0.05 归因同源；本修复消除的是"扣饵推送"这一确定性触发源，
  无饵秒脱簇（sid=1-old）若仍复现则指向周期帧运行位缺陷（Out of Scope）。
