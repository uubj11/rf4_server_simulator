# 海图归属驱动海水组兜底（词表缺口的 f0=2 脱钩修复）

Status: in-progress

<!-- 状态取值：draft | ready | in-progress | done | cancelled
     完成时原地改这里，不要归档、不要搬家、不要另开第二份。 -->

## Problem Statement

2026-11-15 晚北海船钓（loc12_244，rig_type=50）13 发 11 中 7 脱（63.6%），
其中 4 次 f0=2 搏鱼脱钩全部栽在搏鱼脱钩方案（fight-statics-official-tables，
2026-11-13）的**归组词表缺口**上：海水组兜底机制本身工作正常，但归组靠
`fishFightArchetypeFor` 手写关键词——

- `meagre`/`x_gladius` 不在任何词表 → default 桶；
- `m_zubatka` 命中 "zubatka" → 被归进 **small 淡水小型组**（比 default 还冤）；
- 三者全部落第三层 `fallbackFightStatics`（f14=0.5、f16=0.834），即修复前的
  老参数：f16 挣脱概率差海水组 4.4 倍、f14 搏斗强度差 45 倍（零外冲 →
  收线期钩保持目标≈0 → 进度衰减归零）。

对照组：marine 词表命中的鱼 3/4 上岸，落 fallback 兜底的鱼 1/7 上岸。
北海每换一批鱼种，词表就得补一轮——补漏式修复不可收敛。

另有 3 次 f0=1 秒脱钩走 §七 构造期校验路径，本 spec 不覆盖（见 Out of Scope）。

## Solution

海水组判定改数据驱动：静态库 `loc_fish` 的海图鱼种集合（唯一海图经
`loc_alias: level_012_north_sea → 挪威海` 解析；该图 57 种与 17 张淡水图
零交叉，海图鱼种即纯海水集合）优先于关键词词表。词表 marine 命中仍兜底
静态库未收录的种；静态库不可用时纯词表，行为与上线前一致。

## User Stories

1. 作为北海玩家，我希望 meagre/x_gladius/m_zubatka 等词表外的海鱼在搏鱼中
   有外冲、能拉弯竿（f14 22.5/f15 225/f16 0.19/f5 8.445/g），以便 f0=2
   脱钩率与词表内海鱼一致、不再无声脱钩。
2. 边界：词表 marine 命中的种行为不变；淡水种与未收录种维持第三层 fallback
   现状；静态库缺失/无海图数据时启动降级为词表判定（打 Warn 日志，不阻断）；
   27 对象形状与字段 op 不变，只改值，客户端 4.0.25034 基线无需变动。

## Implementation Decisions

- `internal/gameserver/server.go`：Server 增 `marineFishKeys` 只读集合缓存
  （启动一次加载，RWMutex 同步，仿 fishEcologyCache 风格）。
- `internal/gameserver/fishing.go`：
  - `reloadMarineFishKeys`（loc_fish ⋈ loc_alias 按 level_012_north_sea 取
    distinct fish_key，小写归一）；空结果视为错误回退词表；
  - `fishMarineGroupHit`：海图集合 ∪ 词表 marine，nil 集合纯词表；
  - `fishGroupStaticsFor`/`fight27StaticsFor`/`fishStaminaRateGroupFor` 增加
    marine 入参（纯函数保持无状态，调用方传 `s.fishMarineGroupHit`）；
  - `fishF6For` 海水组判定改走 `fishMarineGroupHit`。
- `internal/gameserver/fish_fight_profile.go` + `serverdb.go buildFightCurves`：
  曲线池/耐力层兜底 `fishCurveArchetypeFor`——词表落 default/small 两个通用桶
  的海鱼改归 marine（perch 池、耐力倍率 1.18）；apex/catfish/sturgeon 等
  专属词表携带家族特化，不覆盖。
- `internal/gameserver/serverdb.go attachDB`：启动加载并记 Info/Warn。
- 影响面（2026-11-15 按 252 鱼种 × 静态库枚举复核）：新增进海水组的共 36 种
  （词表 marine 27 种之外的海图鱼），其中曲线层同步变化 31 种（default/small
  桶），cetorhinus_m/conger/katran/lamna_n/somniosus_m 5 种仅静态/f5/f6 层
  变化（专属曲线词表不动）。淡水 195 种行为逐字节不变。
- 数据与迁移：不改表结构、不写库；回滚 = 还原 fishMarineGroupHit 为纯词表
  （三层结构与组表均不动）。
- 红线自查：发鱼参数口径变化已对齐 `specs/capabilities/bite-bait-and-spawn.md`
  （27 判鱼对象值域变化，形状不变）；本 spec 即声明。

## Testing Decisions

- 接缝：`internal/gameserver/fish_fight_statics_test.go`（单元纯函数 +
  `newStaticRigTestServer` ATTACH 真实 static.sqlite3 的存储层）。
- 期望值来源：海水组常数表字面量（f14=22.5/f16=0.19048/f5 8.445/g/f6 0.50，
  沿用 fight-statics-official-tables 落表值）；海图命中判定期望来自
  loc_fish 真实数据（m_zubatka/meagre/x_gladius 等当晚鱼种），非同代码复算。
- 新增用例：`TestFishMarineGroupHitFromChartRoster`（海图命中 + 词表兜底 +
  淡水不误判 + 无库回退）、`TestFight27StaticsChartRosterMarine`（修复前后
  分界锚定：marine=true 海水组 / marine=false 第三层现状；曲线层 default/
  small 改归 marine、apex 不覆盖）、`TestFishStaminaF6ChartRosterMarine`
  （f5 组均值不乘 StaminaScale、f6 0.50、无库回退 0）。
- 既有用例全量保持绿：官方锚点 12 样本、组兜底、f5/f6 锚点、词表判定均改为
  显式传 marineByKeyword，期望值不变。
- 回滚后变红：上述三个新用例（m_zubatka/meagre 不再命中海水组）。
- 门禁：`go test ./...` + `go vet ./...` + `go build ./...`（输出贴 PR；
  实机回归沿用搏鱼脱钩方案 §五 D.5 判据）。

## Out of Scope

- f0=1 构造期校验路径（拉力×钩件比值，2026-11-15 晚 3 起：cetorhinus_m/
  halibut_w/chlamys_isl）——维持搏鱼脱钩方案 §七 范围外结论，需客户端逆向
  续作或官服对照包，独立推进。
- 海水组参数按科细分（鳕科/鲽科/鲨形目）与 P3 锚点补抓——未测种仍用
  merlang+piksha 两点均值外推。
- `fishFightArchetypeFor` 其余关键词（apex/catfish/sturgeon/ambush 等）的
  归属校准——本次只把 marine 判定数据驱动化，不动其他家族词表。

## Further Notes

- `[事实]` 海图集合纯海水：loc_fish 18 图中「挪威海」为唯一海图，57 种与
  淡水图零交叉（2026-11-15 实查 server_data/static.sqlite3）。
- `[事实]` 当晚脱钩鱼种归组：m_zubatka→small、meagre/x_gladius→default，
  均不在 marine 词表（fish_fight_profile.go 关键词表实查 + 影响面脚本）。
- `[事实]` 27 静态参数差异由客户端本地判定消费（f0=2 钩保持归零），见
  `docs/搏鱼脱钩根因与修复整体方案-2026-11-13.md` §三/§四。
- `[事实]` 词表 marine 27 种中仅 21 种位于挪威海 loc_fish；其余 6 种
  `agar`/`warm`/`b_herring`/`c_herring`/`cha_herring`/`k_herring`
  只出现在淡水图（2026-11-15 实查 `server_data/static.sqlite3`）。因此
  “海图集合 ∪ 词表”会保留这 6 种既有的海水组误判；本 PR 为保持淡水项
  逐字节不变暂不改变其行为。后续若要让海图归属成为唯一真源，应在静态库
  可用时停用该词表并补回归。
  后续兑现（PR #516 复审，2026-11-17）：淡水-only 误伤条目 `agar`/`warm`
  已移出 marine 词表，由 f8 组表直查归 predator/sunfish（见
  fish-pull-k-table spec v2.1.2）；其余 4 种为里海流域洄游真海鱼，
  词表 marine 与 marine_pelagic 组语义同向，保留。
