# 海钓搏鱼帧补钩/拟饵挂载树（f0=1 秒脱挂载缺口）

Status: in-progress

<!-- 状态取值：draft | ready | in-progress | done | cancelled
     完成时原地改这里，不要归档、不要搬家、不要另开第二份。 -->

## Problem Statement

2026-11-15 晚北海船钓（rig_type=50，5 拟饵 + 4 独立钩行）3 起 f0=1 秒脱
（gap 1.3/1.7/1.7s，全部落在客户端松口计时器 0.5~2.0s 签名区间）——
《路亚秒脱钩归因分析》（2026-11-12）实证的"保持力 ×0.05 惩罚"仍在命中。
路亚同类问题已由 PR #452（搏鱼期 4/22 拟饵树+运行位）修复，但海钓没吃到：
`Push4_22FightSync` 只按 17 号槽取**单个**拟饵树（多拟饵/槽内混装漏发），
且完全不发钩行树。官方海钓抓包（2026-11-11 marine_422_*）里搏鱼期周期帧
**恒含**钩/拟饵挂载树。客户端搏鱼 ctor 一次性读取挂载态锁定整场，缺树 →
挂载判定翻车 → 保持力 ×0.05 → 秒脱（机制全解见
`docs/搏鱼脱钩根因与修复整体方案-2026-11-13.md` §七.1）。

## Solution

搏鱼帧（14/11 中鱼确认与磨损结算的 4/22）里，海钓钓组能看到的钩/拟饵挂载树
与官方一致：全部拟饵容器树（ioombppcnlh+内置钩）+ 全部独立钩行裸钩树
（agedjipcnpe）都在帧内、运行位齐全。玩家表现为北海铁板/串钩中鱼后正常搏鱼，
f0=1 秒脱簇消失。

## User Stories

1. 作为海钓玩家，我中鱼后不再秒脱钩（<2.5s 真脱钩簇消失），可以正常搏鱼起鱼。
2. 边界：浮钓/底钓搏鱼帧**不加**钩树——官方周期帧本就无（float_422 619B 帧
   0 命中），底钓竿架类走全值分支不依赖挂载态（既有回归测试保持）。
3. 边界：路亚行为不变——17 号槽单拟饵树是"全部拟饵行"的子集，泛化后
   既有路亚测试原样通过。
4. 边界：协议面不变——4/22 args 结构（S("61")+树）不动，只增树元素与 flags 值。

## Implementation Decisions

- `internal/gameserver/profilebuild.go`：
  - 新增 `appendFightHookLureTrees`（返回 `([]*proto.Value, error)`）：
    - 组件读取走 **f5 真源**（`storedRigComponents`），仅 f5 为空时回退
      `rig_component` 投影（与 `rigSetTarget` 同口径）——投影是单向派生，
      "f5 完整、投影为空"的历史档案按投影读会漏发全部钩/拟饵树（评审 P1）；
    - 海钓判定优先 `rigTypeForWrite`（f5/竿型推导），rig 表 `GetRigType` 兜底；
    - 遍历按 slot/ordinal 确定性排序；`ErrNoRows`（组件悬空引用）跳过该组件，
      其余数据库错误向上返回（评审 P2）——`Push4_22FightSync` 收到错误即
      **整帧放弃**（客户端保留上一帧，下一拍重推），绝不发缺钩/拟饵树的帧；
    - catLure（17）行全部追加 `ItemToTree`（路亚/海钓通用，替换原"仅 17 号槽
      首件"特例）；catHook（12）行仅 `marineRigType` 钓组追加。
  - `Push4_22FightSync` 调用点替换原单拟饵块；组件个位数、逐个读物品行可接受
    （每 2s 热路径），品类走批量 IN 查询。
- 14/11 中鱼钩帧（`buildMountedHookTree`）不动：独立钩行裸钩形状与官方
  148B 帧同构（官方鱼2 裸钩帧同样正常搏鱼上岸），拟饵内置钩已有容器分支。
- 数据与迁移：无表结构改动；回滚 = 还原 `Push4_22FightSync` 单拟饵块。

## Testing Decisions

- 单元（`fight_mount_bits_test.go`）：
  - 新增 `TestPush422FightSyncMarineIncludesHookAndLureTrees`（boatr 竿 +
    rig 50 + 2 拟饵 + 2 钩行）：帧内 2 ioombppcnlh + 2 agedjipcnpe 顶层 +
    竿/线/轮，全部挂载位（容器本体与内嵌钩 1024，竿/线/轮 1024|2048）。
  - 新增 `TestPush422FightSyncMarineF5SourceWithoutProjection`（评审 P1 回归）：
    f5 完整 + `rig_component` 投影清空的历史档案，搏鱼帧仍发完整钩/拟饵树。
  - 新增 `TestAppendFightHookLureTreesQueryFailureReturnsError`（评审 P2 回归）：
    数据库不可用时 helper 返回错误（整帧放弃），不静默发缺树帧。
  - 既有 `TestPush422FightSyncLureIncludesLureTree`（路亚）与
    `TestPush422FightSyncBottomRigNoLureTree`（底钓无拟饵树）原样保持——
    泛化是子集扩展、门控是海钓限定，回滚即红。
- 期望值来自官方抓包解码（tmp_analysis 工具，锚点数字入 spec Further Notes），
  非同代码复算。
- 门禁：`go build/vet/test ./...`（cafe 基线失败不计）+ 实机海钓回归
  （f0=1 <2.5s 簇消失、搏鱼时长向官方量级靠）。

## Out of Scope

- 14 状态 op43（`eeajjmkbjbi` 读位）解码——A/B 补树后仍触发 ×0.05 时的
  下一步，见 §七.1 修法 C。
- 客户端构造期整数除法（挂载条目/钩位长度）输入形状的官服对照。
- f17 基线秒脱概率（官方 v<1 区间 1−f17）——官方设计，不做归零。

## Further Notes

- `[事实]` 官方海钓帧解码（2026-11-11 抓包，tmp_analysis 解码器）：
  - marine_422_57（237B）：ioombppcnlh 容器 def 10228=`pilkerlures_21176`
    flags 0x2004600，内嵌 agedjipcnpe def 15699=`triple_21057` flags 22016
    （父=容器、容器类型 4）。
  - marine_422_89（148B）：裸钩 def 9062=`mhook_20894` flags 22016（父=竿）。
  - marine_422_59/60/62（2133B，n=5）：竿树 157 + 线组 149 + 主线 140 +
    **容器 141+钩** + 竿坯树 137——周期帧含钩组树。
  - marine_422_90（2045B）：裸钩竿周期帧含单钩树（1 命中）。
  - float_422_50（619B）：浮钓周期帧 0 钩/拟饵命中（豁免证据）。
- `[事实]` 官方鱼2（148B 裸钩）搏 8 帧后 14/4 入护——裸钩形状本身合法，
  缺口在"钓组挂了拟饵而帧里没有拟饵树"的不一致。
- `[推断]` ×0.05 的客户端谓词（idmgpbkhbng 时间窗 / eeajjmkbjbi op43）由
  挂载态间接驱动；补树后命中条件消失（11-12 路亚修复已实证同路径）。
