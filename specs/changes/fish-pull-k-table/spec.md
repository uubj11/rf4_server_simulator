# 鱼拉力逐鱼种 K 表 + 搏鱼曲线纯 uniform 缩放

Status: in-progress

## Problem Statement

官服 14/14 抓包 21+8 条 A 级样本对拍证实模拟器搏鱼参数两处系统性错误
（完整样本表与反推见「Further Notes」，分析快照 2026-11-11）：

1. **27.f8 拉力全线偏低 14–49%**（均值 −28%，无一例外）。
   官方 `f8 = 体重kg × 逐鱼种 k`，k ∈ [12.6, 20.6] N/kg、种内随体重 +0.3~0.35·ln(w_kg)
   缓升、同种多样本稳定 ±3%。模拟器用常数 `verifiedForcePerGram = 0.00980665` N/g
   （fishing.go:595）× archetype `ForceScale`（fish_fight_profile.go 猜测系数 0.84–1.34），
   等效 k 仅 8.2–13.1。
2. **27.f21 曲线多了官方不存在的逐阶段乘数**（`fightPhaseTensionScale`：
   302 下潜×1.78、105 主搏×0.30、306 翻腾×0.42、101×1.66 等；
   `fightPhaseDurationScale` 同理）。决定性反证：官方每鱼曲线 = 鱼种池 × 单一
   uniform scale 逐行精确成立（perch221=池×0.19375 / crucian293=×1.47233 /
   crucian743=×3.8062 / c.roach680=×4.6066），张力列外的列不随体重缩放
   （duration 恒等于池值；amp 列另有独立因子 ~1.3，形式未定，本期不动）。
   自造层把主搏段压到官方 22%、下潜抬到 132%。

## Solution

### f8：逐鱼种 K 表（已测种精确 + 未测种习性组 v1）

`pull_N = w_kg × (K0(species) + slope·ln(w_kg))`，slope 默认 0.35（tunable
`fish_pull_ln_slope`）。K0 查表顺序：实测种 → 习性组 → fallback 17.5。

实测种 K0（=1kg 基值，由 29 条 A 级样本按 `k0 = k − 0.35·ln(w_kg)` 反推，
样本明细见「Further Notes」）：

| species | K0 | species | K0 |
|---|---|---|---|
| merlang | 21.1 | din_l_carp | 16.9 |
| b_gill | 20.4 | carp | 16.5 |
| taran | 18.8 | perch | 17.6 |
| g_carp | 18.5 | c.roach | 16.4 |
| piksha | 18.3 | crucian | 15.05 |
| b_carp | 18.0 | c.carp | 13.7 |
| n.pike | 17.3 | s.bream | 13.0 |

习性组 K0（未测种，纯推断 ±1.5）：鲑科 18.0 / 鲟科 17.0 / 鲶科 16.0 /
比目鱼 16.0；无组归属 fallback 17.5。鱼种→组映射覆盖鱼谱高频种；
`tunable fish_pull_fallback_k`（默认 17.5）兜底。

- `profile.ForceScale` 从 f8 路径移除（官方 k 无 archetype 形状）。
- `RolledFish.Force` 手动覆盖与 `fight_power_scale` tunable 语义保留。
- `verifiedForcePerGram` 常量保留但不再进入 f8 主路径（其 B 级来源与全部
  A 级样本矛盾，`newtonPerKgf = 9.80665` 恰为其 ×1000，数值不变）。

### f21：纯 uniform 缩放

`buildFightCurvesWithProfile` 只保留 `scale = pull / 池首行f8`：

- f8/f9（张力列）× scale；
- f2/f3（duration 列）不再乘任何系数（官方不随体重/阶段变）；
- 其余列维持池原值；f15–f18 int 列照旧；
- 删除 `fightPhaseTensionScale` / `fightPhaseDurationScale` 及
  profile 的 DurationScale/RestScale/Burst/ForceScale 字段（StaminaScale/SizeRatio
  保留，f5 仍用 archetype，其逐鱼种表属 P2）。

## User Stories

- 玩家溜同一条官方克重的鱼，杆弯量/泄力报警/线载显示与官服同量级（力 ±3%）。
- 主搏段（105）冲刺强度恢复官方量级（现被压到 22%），下潜（302）不再异常硬。
- 空竿时长不变（本 spec 不动咬钩/发鱼）。

## Implementation Decisions

- 模块：`internal/gameserver`（fishing.go 拉力口径、serverdb.go 曲线池装配、
  fish_fight_profile.go 删除自造缩放层、reel_fight_wear.go / statsbuild.go 结算随动）；
  `internal/admin/web/index.html` 登记新 tunable（「玩法手感」分组可直接改）。
- 接口/字段：`27.f8`（牛）与 `27.f21`（曲线行 19 列）的响应形状与字段 op 不变，
  只改取值口径；无新命令。
- 数据与迁移：无表结构变更。应急覆盖走既有 tunables：
  `fish_pull_ln_slope`（默认 0.35）、`fish_pull_fallback_k`（默认 17.5）、
  `fight_power_scale`（整体倍率，保留）。
- 兼容：客户端基线 `4.0.25034`；f8/f21 数值回归官方抓包口径（依据即本 spec
  「Further Notes」样本表），回归场景见 Tests。回滚 = 还原本提交，无状态残留。

## Non-Goals

- f5/f6 逐鱼种表（P2）；曲线池直配（P2）；amp 列独立缩放因子（待样本）；
  f16/f20（P3）；玩家耐力（独立能力项）。

## Tests

- `TestFishPullMatchesOfficialAnchors`（新增）：29 条 A 级样本钉死，全部落
  ±2~3%（样本表见「Further Notes」）。
- `TestFishPullScalesWithSpeciesAndWeight`：k 表单调性（同种体重大→拉力大、
  掠食者高于静水小鲤、海水鳕科最高）；未测种走组表/fallback 不为 0。
- `TestFishPullGroupFallback` / `TestFishPullLnSlopeTunable`（新增）：习性组与
  fallback 的 K0 取值；`fish_pull_ln_slope` / `fish_pull_fallback_k` 应急覆盖生效。
- `TestFish27UsesVerifiedFightFields` 重写：删 B 级 53g/63g 严格断言，改 k 表口径。
- `TestFightCurvesKeepSprintTensionStages`：uniform 后张力包络改为 crucian 池口径
  0.45×pull ≤ min、max ≤ 1.35×pull 且跨过 pull（官方 crucian 实测 min≈0.49×pull）；
  阶段存在性断言保留。
- `TestFightCurvesUniformEnvelope`（新增）：不同 pull 下逐行张力/pull 恒等，
  锁定纯 uniform 不变式。
- 既有 `reel_fight_wear` / `reel_wear_*` / `fishing_persist` 等引用 pull 的测试随
  新口径修正期望值。
- 回滚本提交后：以上新增/重写用例与全部锚点断言变红。

## Risks

- 力整体 +28%、105 段 ×4.5：实机可感知，属官方口径修复。需实机 A/B
  （同杆钓鲫鱼/草鱼：泄力报警时机、断件频率）。
- 结算侧 `fishFightPullKgfFor`（= pull / 9.80665）随 f8 上移，泄力比较基准同步
  变严——回归断线判定相关测试。
- f5 未动：鱼更强但体力池没调，搏鱼观感更费力，属预期（P2 修 f5）。

## Further Notes

- [事实] 29 条 A 级锚点样本（species / weight_g / 官方 f8），与
  `fish_pull_k_table_test.go` 的数值锚点一一对应；按
  `k0 = f8/w_kg − 0.35·ln(w_kg)` 反推，同种各样本 K0 落 ±3% 内，
  按种取均值即 Solution 中的 K0 表：

| species | weight_g | f8 (N) | 反推 K0 |
|---|---|---|---|
| crucian | 611 | 9.0919 | 15.05 |
| crucian | 293 | 4.2863 | 15.06 |
| crucian | 743 | 11.1182 | 15.07 |
| crucian | 193 | 2.7937 | 15.05 |
| c.roach | 680 | 11.0735 | 16.42 |
| c.roach | 150 | 2.3899 | 16.60 |
| c.roach | 713 | 11.6109 | 16.40 |
| perch | 221 | 3.7476 | 17.49 |
| perch | 93 | 1.5575 | 17.58 |
| perch | 1070 | 18.9743 | 17.71 |
| perch | 119 | 1.9980 | 17.54 |
| perch | 342 | 5.8674 | 17.53 |
| perch | 642 | 11.1688 | 17.55 |
| g_carp | 21742 | 423.7980 | 18.41 |
| g_carp | 4880 | 93.4397 | 18.59 |
| g_carp | 5484 | 105.0730 | 18.56 |
| g_carp | 5716 | 109.5450 | 18.55 |
| g_carp | 10307 | 198.4960 | 18.44 |
| b_carp | 7828 | 146.5190 | 18.00 |
| carp | 1063 | 17.5527 | 16.49 |
| taran | 611 | 11.3884 | 18.81 |
| piksha | 2975 | 55.4510 | 18.26 |
| merlang | 252 | 5.1914 | 21.08 |
| b_gill | 113 | 2.2171 | 20.38 |
| n.pike | 4305 | 76.6636 | 17.30 |
| din_l_carp | 1715 | 29.3737 | 16.94 |
| c.carp | 655 | 8.9112 | 13.75 |
| c.carp | 140 | 1.8186 | 13.68 |
| s.bream | 359 | 4.5193 | 12.95 |

- [事实] uniform 缩放：官方每鱼曲线 = 鱼种池 × 单一 scale 逐行精确成立
  （perch221=×0.19375 / crucian293=×1.47233 / crucian743=×3.8062 /
  c.roach680=×4.6066，张力列外的列不随体重缩放）。
- [推断] 未测种习性组 K0：按已测梯度（同科体型单调、海水/洄游高于静水）外推，
  纯估计组 ±1.5。
- [假设] slope=0.35 种内通用：perch/crucian/g_carp 多重量级样本钉死，其余种
  样本量不足以独立验证斜率。
- amp 列存在 ~1.3 独立因子，形式未定，未验证，本期不动。

## v2 增补：恒 k/变 k 两类模型 + 17 新实测种（2026-11-17）

数据源：2026-11-16/17 断电抢救包（`tools/wireshark/断电抢救-20261116.pcapng`，
53 条 14/14 A 级样本，全部经服务端通道切换恢复解密——见
logs/captures/official_trace/HANDOFF.md v2 章节）。鱼种代码↔中文名对照以
static.sqlite3 `loc_fish` 表为准。

### 发现：k 与体重关系分两类

- **恒 k 类（Slope=0，pull = w×k）**：海鱼与小型鱼。跨体重档 k 精确不变：
  coalfish 绿青鳕 702~3027g ×4 恒 21.58、atl_cod 大西洋鳕 ×2 恒 21.58、
  m_scorpius 短角大杜父鱼 205~1142g ×5 恒 17.66、shemaya_cha 黑海欧白鱼
  117~237g ×4 恒 18.64、tiulka 棱鲱 ×6 恒 ≈18.6。k 取值落在离散集合
  {17.66, 18.64, 19.62, 21.58, 22.56, 23.54, 25.53}，疑似服务端曲线池直配幅值。
  旧单样本 taran（18.64）/piksha（18.64）/b_gill（19.62）与离散值精确重合，
  v2 一并改判恒 k。
- **变 k 类（k = K0 + slope·ln(w_kg)，slope 鱼种各异）**：鲤科淡水鱼。
  c.roach 0.23 / g_carp 0.23 / crucian 0.35 / perch 0.38 / carp 0.49 /
  c.carp 0.45 / rudd 0.53 / m_carp 0.59。v1 的全局 slope=0.35 是折中值。

### 实现变更

- `fishPullK0` 值类型 `float64` → `fishPullParam{K0, Slope}`；Slope<0 表示
  单样本未标定，回落全局 `fishPullLnSlope`（=0.35，降级为组/fallback 默认斜率）。
- 恒 k 类不吃 `fish_pull_ln_slope` tunable（锚点测试断言）。
- `fishPullK0For` 保留返回 K0（日志/显示层用），主路径改 `fishPullParamFor`。
- f8 官方路径 0.5N 下限降为 0.05N：官方 10g 棱鲱 f8=0.186N、24g 欧鲌 0.424N，
  旧下限与 A 级样本矛盾（legacy 分支保持 0.5N 不动）。
- 17 个新实测种入表（恒 k 13 + 变 k/先验 4）：coalfish 21.58、atl_cod 21.58、
  m_scorpius 17.66、shemaya_cha 18.64、tiulka 18.64、pumps_sunfish 19.62、
  menek 18.64、pollachius 22.56、atl_herring 22.57、makrel 23.54、
  cha_herring 25.53、rudd {19.24, 0.53}、carp_ghost 16.73、n_carp 16.53、
  kw_tench 18.68、c_bleak 18.81、m_carp {16.27, 0.59}（均为 K0，恒 k 类
  Slope=0、鲤科单样本 -1）。**m_carp 为低置信口径**：4 样本仅产出拟合参数，
  无单样本 f8 锚点、无 f5 实测值（样本原始值按 CONTRIBUTING 禁令不入库、
  断电后未留档），测试只钉表参数；补抓样本后再升级数值锚点。
- 旧种复核修正：carp {16.50, 0.49}（7 样本）、c.carp {13.84, 0.45}（4）、
  c.roach {16.36, 0.23}、perch {17.60, 0.38}（9）、g_carp {18.79, 0.23}（5，
  底钓2 样本重拟合）；crucian {15.05, 0.35} 6 样本精确复核不变。
- `fishPullSpeciesGroup` 移除已实测 5 种（coalfish/pollachius/menek/n_carp/
  kw_tench）；`marine_gadid` 19.7 组值保留给未实测鳕形目，注释标注实测离散值。
- f5 `fishStaminaRatePerGram` 新增 16 种（均值口径，样本极差 ±13%~30%）；
  perch 2.75→2.69、c.carp 2.52→2.78（样本扩充重算）。m_carp 不在 f5 表
  （无实测均值），运行时走 cyprinid 组值 2.50/g 回退。
- 搏斗组词表补 tiulka → small 组（原落 default）。

### 锚点与自校验

- 锚点测试扩到 30 种 68 条（新种配跨体重档对钉 slope/恒 k；m_carp 无单样本
  值不入锚点，仅钉表参数）；
  crucian K0=15.05 与 v1 spec 精确一致、f5 每 g 2.297 vs 2.29 双指标自校验通过。
- 全量门禁：gameserver 包仅剩既有基线失败 TestCafeNativeFlowPaysAndPushesRemoval
  （单跑过、时序敏感，口径同 AGENTS.md）；其余包 2 失败均为环境/工作区既有问题
  （launcher GOTMPDIR 路径分隔符、docs 索引缺 2026-11-16 解码文档登记），与本分支无关。

### v2.1 增补：未实测种全覆盖归组 + 皮肤变种别名（2026-11-17）

- static 库 249 种全覆盖，不再有裸 fallback：31 实测 + 204 习性组 + 16 别名
  = 251 键，其中 `vobla`/`zander` 为两个非 loc_fish 兼容键（客户端旧键，
  行为兼容保留），loc_fish 实际 249 种零遗漏。
- **别名表 `fishPullAlias`**：16 个皮肤/变种鱼种继承宿主实测参数（避免组表
  斜率偏差）——carp 系 10（common_carp/三花/鳞鲤白化/匈红鲤/绿鲤/带形框形
  镜鲤系）、m_carp 系 3、n_carp 系 2、g_carp_albino。
- **组值按组内实测成员标定**（`fishPullGroupK0` 值类型同步升级
  `fishPullParam`）：marine_gadid 19.7→{21.58,0}、sunfish 20.4→{19.62,0}、
  migratory_roach 18.8→{18.64,0}、herb_carp 18.2→{18.79,0.23}、
  roach_run 16.4→{16.36,0.23}、still_carp 15.0→{15.05,0.35}、
  pond_carp 13.7→{13.84,0.45}、carp_mid 16.7→{16.50,-1}、
  predator 17.5→{17.30,-1}、weak_silver/salmonid/sturgeon/catfish/flatfish
  保持 K0（Slope=-1 全局先验）。海鱼类组 Slope=0 恒 k、鲤科组继承成员
  自拟合斜率、纯估计组 -1 吃全局 tunable。
- **新增 5 组**：marine_pelagic {22.57,0}（鲱鲭中上层）、marine_rock {17.66,0}
  （鮋/杜父/绵鳚/鳗鳃类）、small_forage {18.64,0}（小型饵料鱼：刺鱼/鮈/鱥/
  泥鳅/胡瓜鱼/shemaya 自 roach_run 移入）、apex {25.53,0}（鲨/剑鱼/金枪鱼，
  纯推断取离散上界）。
- 官方 k 域注释更新：v1 的 [12.6, 20.6] 被恒 k 海鱼实证推翻，实测域
  [12.59, 25.53]（s.bream~cha_herring）。
- 归组依据：key/中文名词表分类（鳞鲱鳟鲈鲷鲆鲽鲨鳗鲶鲟……）+ 离散 k 离散值
  近亲继承；蟹/贝/蛙等无拉力数据的渔获按体型归 weak_silver/marine_rock
  （低置信，抓到后即校准）。fallback 17.5 仅剩非 loc_fish 未知键兜底。

### v2.1.1 审查修正：跨层归组自洽 + 低置信口径落档（2026-11-17，PR #516）

- **crawfish（河螯虾）归组修正**：salmonid → weak_silver。虾无拉力数据，
  按「蟹/贝/蛙按体型归组」口径归贝/蛙组（K0 13.00），与 f5/f6 的
  default 弱鱼口径（2.60/g、f6 0.55）方向一致；原 salmonid 18.00 与
  虾的体型/习性明显不符。
- **pinagor（圆鳍鱼）归组修正**：salmonid → marine_rock {17.66, 0}。
  loc_fish 里圆鳍鱼仅出现在挪威海，运行时 marine 判定为真，f5/f6/f21
  走 marine 口径（8.445/g、merlang 曲线池），f8 却走鲑科淡水模型
  18.00 属跨层不自洽；圆鳍鱼为海底栖鱼，归 marine_rock 与 m_scorpius
  恒 k 锚同族。
- **m_carp 低置信口径落档**：4 样本仅产出拟合参数 {16.27, 0.59}，无单样本
  f8 锚点、无 f5 实测值；f5 运行时走 cyprinid 组值 2.50/g 回退（表内
  30 实测种、锚点 30 种 68 条计数以此为准）。测试钉表参数防误改，
  补抓样本后再升级数值锚点。
- **测试斜率可观测性修正**：斜率 tunable/别名继承断言原用 1kg
  （ln(1)=0，斜率项恒消失，tunable 是否接线不可观测），改 2kg
  （ln2≈0.693）并按 tunable 真实语义断言：`fish_pull_ln_slope` 只替换
  Slope<0 未标定种的回退斜率——置 0 时 2kg fallback 恰为 2×17.5、
  置 1 时为 2×(17.5+ln2)；自拟合斜率（perch 0.38）与恒 k（coalfish）
  不受 tunable 影响；carp_albino 与 carp 2kg f8 逐位相等（整参数继承）。

### v2.1.2 复审修正：组表驱动全搏斗层 + 别名全链路继承（2026-11-17，PR #516）

- **f8 组表直查驱动 f5/f6/f21**（复审 P2-1）：新增 `fishPullGroupFightKey`
  （f8 组 → 组键）与 `fightGroupKeyFromPullGroup`，`fishFightGroupKeyFor` 与
  `fishCurveArchetypeFor` 淡水分支先查组表、词表只兜底未入组鱼种。修复
  「54 个 f8 salmonid 中 26 个 f5/f6 落 default」等漂移：sig 系列等俄语译名
  鲑科 →salmonid（3.00/g、f6 0.60、e.chub 池）、small_forage 组泥鳅/胡瓜鱼
  →small（a_smelt 原被词表 smelt 误归 salmonid）、sunfish 组太阳鱼→small、
  predator 组→ambush、weak_silver（crawfish）→small（2.50/g、f6 0.50、
  c.roach 池）。海组（marine_*/flatfish）映射交回海图判定：淡水图的
  marine_rock 成员（鳗/七鳃鳗/杜父）维持词表口径，淡水鳗鱼不吃 8.445/g
  海水耐力。完备性测试保证组表/组值表每个键都有映射。
- **agar/warm 移出 marine 词表**（复审点名 warm 跨层偏差）：两键只在淡水图
  （chart-roster-marine-group spec §Further Notes 预留的「行为兼容保留」
  口径），词表 marine 曾使 warm 的 f5 吃 8.445/g 海水耐力；移除后由组表
  直查归 predator/sunfish。b_herring/c_herring/cha_herring/k_herring 为
  里海流域洄游真海鱼，词表 marine 与 marine_pelagic 组语义同向，保留。
- **皮肤变种全链路继承宿主**（复审 P2-2）：`fishPullAliasHost` 解析别名后
  统一进 f5/f6 显式表（`fishStaminaRatePerGram`/`fishF6Table`/`fishF6Step`
  用宿主键查询）与词表判定（`fishFightArchetypeFor` 入口解析）。midori_g
  现在 f8/f5/f6/f21 = carp 的 16.50+0.49·ln(w)/2.10/g/0.65/c.carp 池，
  非 1kg 断言（2kg f8 逐位相等）。
- **fight_power_scale 分支下限 0.5N→0.05N**（复审 P2-3）：与主路径统一为
  浮点保护下限；tiulka 10g 官方 f8=0.1864N，scale=0.5 现得 0.0932N
  （旧下限抬回 0.5N），加回归测试。
- **词表既有口径测试同步**：s.orfe（roach_run→small）、neiva（salmonid）、
  m_zubatka（predator→ambush）等断言按组表直查新口径更新；受影响种在
  生产的行为变化方向与 f8 组语义一致。
