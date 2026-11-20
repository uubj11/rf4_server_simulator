# 修正资格任务的竿型、钓组与拟饵族判定

Status: done

## Problem Statement

玩家反馈资格任务「传统纺车竿」阶段 5-2（1402）与 6-1（1501）无法完成，5-2 面板还显示
「纺车竿 + 抽停钓组」这种互相矛盾的文案；用手工饵或表水饵钓到的鱼在部分任务里不计数。

现象来自三处：

1. 5007 的竿型（Idx5）按分支兜底下发：1xxx 全部限成纺车竿，与任务文案无关。客户端
   据此渲染「鱼竿」行并叠加服务端过滤，导致 1402（只能用抽停竿）永远无法完成，
   1501（水表路亚常用枪柄竿）不计数，分支 2/4/7/8 的「鱼竿」行也普遍多出文案里
   没有的竿型。
2. 1402 文案写的是「抽停(jerkbait)硬饵钓组」，但判定只按拟饵族，没有要求该钓组。
3. 拟饵族按 item_id 前缀判定：X-Walker 等表水饵的 item_id 是 `plug_w_*`，被误判成
   鱼形硬饵；自制饵（`hm_*`）则全部落到空族，导致「水表路亚」「使用某类拟饵」类
   任务大量漏算。

## Solution

资格任务的条件只来自客户端任务文案（`qlfctn_t_<id>`）：

- 文案写了竿型的任务才下发 Idx5、才做竿型过滤；没写的不下发、不拦。
- 1402 下发抽停竿（12），判定同时要求抽停钓组（rig 22）+ 抽停硬饵 + 奖杯狗鱼。
- 拟饵族按 `item_def.mbemakijdeh`（拟饵类型）判定，类型随咬钩快照持久化；
  item_id 前缀只作为旧存档/类型缺失时的兜底。
- 7002「10 种不同味道的颗粒」按饵本身（去掉尾部尺寸）去重。

## User Stories

1. 作为玩家，我希望 5-2 面板只显示抽停竿，并能用抽停竿 + 抽停钓组完成任务。
2. 作为玩家，我希望 6-1 用纺车竿或枪柄竿抛投水表饵都能完成，包括 X-Walker 这类
   item_id 前缀与真实类型不一致的表水饵。
3. 边界：升级前已挂钩、重启后才结算的鱼（旧快照没有拟饵类型）仍应正确判定；
   升级窗口内无法从 item_id 反查类型时回落到前缀。
4. 边界：文案没写竿型的任务不再因分支名被限成单一竿型。

## Implementation Decisions

- `internal/gameserver/qualification_rules.go`
  - `qualificationRodTypesFor`：改为「文案白名单」，不再按分支兜底。非空集合：
    - 1402 → `[12]`
    - 3302/3501/3601 → `[1]`
    - 5101/5103/5201/5202/5203/5401 → `[4]`；5301/5302/5303/5601/5602 → `[4,5]`；
      5402/5403/5501/5503 → `[5]`
    - 6001/6002/6003/6101/6103 → `[10]`；6202/6303/6402/6501 → `[9]`；
      6102/6201/6203/6301/6302/6401/6403/6503/6601/6602/6603 → `[9,10]`
    - 其余 168 条永久任务（含 5001/5002/5003/5102/5502/5603/6502 这些只写钓组的）
      均为 `nil`。
  - `qualificationRodPowerFor`：按文案收窄，「超轻」→ UL，「轻型/轻型或超轻」→ UL+L。
  - 1402 判定加 `qRigIn(22)`。
  - `qualificationBait`/`qualificationEvent` 增加 `LureType`；快照缺失时在
    `qualificationEventForCatch` 里按 `item_id` 反查 `item_def` 回填。
- `internal/gameserver/qualification_classify.go`
  - `lureTypeFamilies`：1/20 spinner、2 spoon、4 wobbler、5/10 topwater、7 jerkbait；
    软饵/铁板/飞蝇/铅头钩等类型不映射。
  - `lureFamilyOf`：类型优先，缺失时 `lureFamily(item_id)` 前缀兜底；前缀表补齐
    `hm_spinner/hm_spoon/hm_walker/hm_popper/hm_jerk/hm_pl/hm_crank/hm_shad/hm_minnow`。
  - `qCountBaitFlavor`：味道键 = item_id 去掉尾部尺寸数字。
- `internal/gameserver/fishing_persist.go`、`internal/store/repos_fishing_state.go`
  - `FishingFish.QualBaitLureType`（JSON `qual_bait_lure_type`）随 pending 快照往返，
    不改表结构；旧 JSON 缺字段为零值。
- 5007：Idx5 只下发文案点名的竿型；Idx4 钓组大类、Idx6 硬度档维持原有语义。

兼容性：不改表结构、不改 5007/5003 wire 形状；旧存档 JSON 直接可读。客户端基线
4.0.25034。回滚方式：回退本次提交即可，旧 Rows 无新字段。

## Testing Decisions

- 单元（纯逻辑）：
  - `TestQualificationRodTypesCoverage`：固化全部 168 条永久任务的竿型清单，
    清单外必须为 nil。
  - `TestQualificationRodTypesFollowTaskText`：1402 抽停竿+rig 22 计入、纺车+路亚钓组
    不计；5101/6001 等文案点名竿型的仍拦。
  - `TestQualificationRule5007Conditions`：1402 下发 `[12]`、1501 不下发 Idx5。
  - `TestLureFamilyUsesLureType` / `TestLureFamilyPrefixes`：类型优先（X-Walker 为
    topwater、`plug_w_2381` 为 wobbler）、`hm_foamr`（类型 3）为空、类型缺失回退前缀。
  - `TestQualificationRule7002PelletFlavors`：真实颗粒 item_id；同味道不同尺寸算 1 种。
- 存储（真实 SQLite）：
  - `TestQualificationMountedBaitReadsLureType`：真实 `item_def`/`item` 行 → 挂载快照
    读到 `mbemakijdeh`。
  - `TestQualificationOldSnapshotBackfillsLureType`：`LureType=0` 的旧快照按 item_id
    回填为 5。
  - 既有 `TestFishingStateRestorePendingNoReroll` 覆盖新字段的 JSON 往返。
- 期望值来源：客户端任务文案（`assets/qualification_texts_zh.json`）、客户端
  `GameAssembly.pgjliohpiie` 的竿型×钓组矩阵、静态库真实 item_id（98 条颗粒均以
  尺寸数字结尾）。
- 回滚后变红的测试：上述新增/调整的资格、拟饵族、快照恢复用例。
- 实机（客户端 4.0.25034）：
  - 5-2：抽停竿 + rig 22 + 抽停硬饵，奖杯狗鱼 13.12kg → 完成。
  - 6-1：纺车竿 + 路亚钓组 + 表水饵，奖杯细鳞鲑 5.714kg → 完成。

## Out of Scope

- 活动分支 9..20 的竿型/硬度表（本来就是逐任务按文案维护，未发现同类问题）。
- 7401/6502 等目标值（Idx3）的保守取值，不在本次范围。
- 拟饵类型以外的其它饵属性（味型数值、颗粒味道的官方枚举）。

## Further Notes

- `[事实]` 客户端 `GameAssembly.dll` 的 `pgjliohpiie(竿型, 钓组)` 跳转表：rig 22 只在
  竿型 12 返回 true；纺车/枪柄允许 5..11、19、20、35；Picker/Feeder 允许
  12..17、24..29、32、34；鲤竿另加 18；海竿 50..59。
- `[事实]` 客户端名称表里 `rig_JerkbaitRig` 的简中名就是「抽停(jerkbait)硬饵钓组」，
  与 1402 文案逐字一致。
- `[事实]` 静态库 98 条颗粒（category=27, fopbhlmkcpe=9）的 item_id 全部以尺寸数字
  结尾，去掉尾数字后得到 30 种味道。
- `[推断]` 文案没写竿型时客户端会按钓组大类推导并显示兼容竿型；服务端不过滤，
  这是产品接受的行为。
- 未验证：7002 实机钓满 10 种味道；旧存档快照只在本地测试库验证。
