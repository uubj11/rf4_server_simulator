# 鱼种水层基线 v4 —— 分辨力收窄与表层鱼补齐

Status: in-progress

<!-- 状态取值：draft | ready | in-progress | done | cancelled
     PR-1 数据层合入后改 ready；PR-2/PR-3 合入并实测后改 done。 -->

## Problem Statement

两个玩家实测现象，同一根因链：

1. **水面系饵上底部的鱼**：水面拟饵（波扒/老鼠/犬步）与中层/跳底拟饵打出同一批鱼，没有水层分割。
2. **浮钓上"路亚的鱼"**：浮钓天然饵能钓到食性上是拟饵向的鱼种（鲑科/狗鱼/黑鲈）。

根因（三层叠加，互相锁死）：

- **总开关默认关**：`fishBiteEnvironmentWeight` 在 `fish_ecology_enabled != 1` 时短路返回 1（fish_ecology.go），
  252 种鱼的水层/温度/动作权重全部不参与判鱼，任何饵×钓法共享同一份"地点名册×捕获数"权重。
- **v3 生态表本身无分辨力**：252 条鱼没有一条表层鱼（layer_pref≤0.25 为 0；0.46 以下有 42 条，最小 0.33），区间普遍宽 0.66~0.9
  （狗鱼 0.07~0.97 全水柱），即使打开开关，水面饵对全柱鱼因子同为 ~0.84~1.0，分不开层。
- **惩罚不对称**：`fishLayerWeightProfile` 只罚"底钓打表层鱼 ×0.05"和"路亚比鱼深 ×0.2"，
  水面饵打底层鱼只走高斯尾巴。
- **证据旁路放行**（现象 B 主因）：strict 食性下 companion/本地证据命中即满分放行，不设数量阈值、
  不分钓法——1 次孤例（玉米上狗鱼）就永久满分，85 种拟饵向鱼里 81 种带天然饵证据行。
- **生成能力丢失**：docs/gameplay.md 提到的 `tools/genfishdata` 不在仓库（git 历史也无），v3 表不可再生成。

## Solution（三步，按依赖排序）

### PR-1 数据层（本 spec 随附）

- 补回生成器 `tools/regen_fish_layers.py`：官服侧拟饵证据加权分位数（主，~64%）+
  食性组先验（兜底，~36%）+ 规则文本 bottomOnly（硬下界）+ **动作锚定**（v3 action_json 的
  bottom/surface 比值 >1.15 的底栖掠食——江鳕/鲶/鲟/鳕——pref 直接采用 v3 全值，
  证据分位数只收窄区间。pref 不能打折：它还喂 `marineTraits`（海图鱼群 ambush/pit）与
  `fishSpotBiteWeight`，曾试 0.75/0.9 折扣，鳕鱼 pref 0.90→0.61~0.68 三处连击，
  env 1.16→0.67，`TestMarineHuntUsesSonarZoneWithoutGroundbait` 回归（海图铁板
  lureLayer≈0.90 偏离 pref 过远）；pref 恢复 v3 后区间仍收窄，水面饵压制不受影响）。
- 拟饵水层带复用 `loadLureAttributes` 的 TypeID→DiveDepth 表；override 两处（stickbait 0.56→0.30、
  Flying worm 0.56→0.18）；popper/mouse 等名词特判表层。
- `fish_ecology_defs.json` v3→v4：只改水层三元组与 profile_version，其余字段零漂移（已验证）；
  区间最大宽度 0.90→0.45（v4 数；v4.1 下探后实际 0.65，8 条 >0.45）；表层鱼 0→6 条，中上层 116 条。
- Go 侧 `fishEcologyProfileVersion` 3→4，`ensureFishEcology` 的 `WHERE profile_version < excluded`
  在下次启动自动覆盖落库。

**v4.1（rf4records 二源修订，2026-11-14 追加）**：`tools/fetch_rf4records.py` 管道入库
官方周纪录捕获 47.9 万条（rf4records_catch 表）后，生成器接入第二独立证据源
（`load_rf4records_layer`）：companion（长期累计）与 rf4records（窗口纪录）的拟饵水层
中心逐鱼一致（江鳕 0.40/0.38、六须鲇 0.57/0.57、绵鳚 0.60/0.61），证明 v4 动作锚定在
底栖掠食上过头——12 条鱼的玩家主流中层拟饵打法被锁在区间外。修订：锚定组区间下沿
下探到 rf4records 证据中心（下限 0.35，浅水底栖保留对表层的压制；锚定组宽度上限放宽
到 0.65），23 条鱼受影响。复测：水面饵打底栖鱼 max 0.267→0.312（鳊/拟鲤类浅水杂食，
水面饵可达性本就略高，接受），底栖掠食 max 0.105 不变，中层拟饵对底鱼的因子从
0.28~0.42 回到区间档。

分辨力（区间内 0.75 地板、区间外指数衰减口径）：

| 组 | 指标 | v3 | v4 |
|---|---|---|---|
| bottom（58 条） | 表层饵 max | 0.68 | 0.27（中位 0.06） |
| predator（161 条） | 跳底饵中位 | 0.75 | 0.33 |
| pelagic（10 条） | 跳底饵中位 | 0.75 | 0.12 |
| 上层掠食（34 条：狗鱼/黑鲈/鲑科） | 表层饵 min | 0.61 | 0.34 |
| 底栖掠食（72 条：鲟/鲶/鳕/江鳕） | 表层饵 max | — | 0.105 |

### PR-2 评分函数对称化

`fishLayerWeightProfile` 删除两条不对称特判（"底钓打表层鱼 ×0.05"、"路亚比鱼深 ×0.2"），改为：
区间（±0.08 容差）内保留 pref 高斯峰（精确命中 ≈1.0，`marineDepthMatch`/海图狩猎依赖此峰，
不能一律压平到地板——曾试纯地板导致鳕鱼 env 再降）+ 0.75 地板；区间外对称指数衰减
`0.75·exp(-2·出界半宽)`，floor 0.02 保留杂口。水面饵打底栖鱼 0.267→~0.18，方向无关。
纯函数改动 + 表格驱动单测（对称性断言：同量级出界因子一致）。

### PR-3 水层证据链对齐

1. `rigWaterLayer` 路亚无运动证据 fallback 0.5 → 按 `runtime.lure.DiveDepth` 兜底
   （水面系饵刚入水未上报动作时不再按中层判鱼）；
2. `lureLayerFromMotion(mask, previous, dive)` 加第三参：dive>0 时结果钳进 `dive+0.15`——
   运动位是操作意图，但饵的物理泳层有上限；水面系饵（dive≈0.10）匀速收线旧版按
   `+.03` 无界漂深，深潜铁板跳底位也越物理深度；水面动作位不受钳（深潜饵快收上
   水面是真实操作）；dive=0 保持旧语义；
3. 抛竿（14/2）补饵后取当前装配拟饵的名义泳层：`FishState.LureDive`（仅进程内，
   不进持久化快照）缓存 + `st.LureLayer` 种子为该值——落水瞬间饵在设计泳层，
   14/7 高频路径用缓存值钳制，不再回查 DB。**新一杆无条件清零**（评审 P1/P2-1）：
   `LureDive=0`、`LureLayer=0`、`LureAction=""`、`LureMotionMask=0`（同竿换钓组
   路亚→非路亚也不得残留），非路亚竿同样清；多拟饵钓组取最深拟饵（评审 P2-3）；
4. （回退）浮钓实际水深归一不纳入本 PR：`rigWaterLayer` 与 `fishWaterConditionsFor`
   统一保持 5m 满刻度。按 `spotEnv.DepthM` 实际水深会让区域批量捕获与逐点生产权重
   对不上（`TestFishQueryRegionCaptureMatchesPointCalculation`）；评审 P2-2 以回退解决。

涉及客户端状态链（FishState + 14/2/14/7），需实机回归：水面系抛竿→收线→鱼口
分布、深潜铁板跳底。

### 依赖与可复现（评审 P2-2）

- 二源证据来自独立 PR #481 所引
  `tools/fetch_rf4records.py` + `rf4records_catch` 表（迁移 00061）——本 PR 的
  `regen_fish_layers.py` 依赖该表，合入顺序：#481 先、本 PR 后（或同批合）；
- 可复现命令：`python -I tools/fetch_rf4records.py`（抓取入库）→
  `python -I tools/regen_fish_layers.py --static server_data/static.sqlite3 --db data/rf4server.db
  --in internal/gameserver/assets/fish_ecology_defs.json --out /tmp/v41.json --report /tmp/report.md`；
- companion 数据 provenance 已在 Evidence 段声明为"高置信推断"，抓取口径随
  #481 管道每周滚动补齐。

### 合并后验收（评审 P2-1）

- `fish_ecology_enabled` 置 1 前必须完成：① 用 `verify2` 脚本重跑"rf4records 拟饵证据
  中心 vs v4.1 区间"确认区间外清单（当前 12 条已由 v4.1 下探收敛，需重测确认）；② 对
  区间外剩余鱼种做实机鱼口核对；③ 换饵/换竿场景实机（`TestCastPrepareResetsLureDiveAndLayer`
  已覆盖单测层，实机需复验 14/2→14/7 的 LureDive/LureLayer 链路）。

### 生效与回滚（评审 P1：明确接受默认行为变化）

- 总开关 `fish_ecology_enabled`（当前库值 0）**只**控制 `fishBiteEnvironmentWeight`：
  置 1 启用生态环境权重；置 0 时环境权重早退，但**不是完整回滚**。
- 本 PR 的 v4 水层数据与 PR-2/PR-3 的函数仍会被以下默认消费者直接使用，
  因此 `fish_ecology_enabled=0` 时也会改变行为（已明确接受，不再声称“行为完全一致”）：
  - 鱼猎：`fishLureWeight` / `fishWaterConditionsFor`（`fish_hunt_enabled` 缺省 1）；
  - 成就：`f.DepthCM` 的 `qDepthAtLeast` 判定（拟饵种子/钳制）；
  - 窝料：`fishLayerWeightProfile` / `fishWaterConditionsFor`（`groundbait_enabled=1` 时）；
  - 水温/光照：路亚 `effectiveLureLayer` 的 fallback。
- 完整回滚 = revert 本 PR（代码 + JSON + profile_version）；数据侧还可 revert JSON + 版本 bump v5 重发，
  或管理端逐鱼改 enabled。若后续要求“开关 0 = 旧行为”，需另加默认 0 的 `lure_layer_v4` 开关并把上述
  消费者全部纳入；本 PR 不采用，因为 v4 数据本身已被窝料/鱼猎直接读取。
- v4 覆盖是单向的（version 条件只升不降），写在此处备查。

## Evidence（证据来源分层，诚实记录 provenance）

| 证据 | 来源 | 等级 |
|---|---|---|
| `companion_bait_fish` 31231 行 | 玩家库 `migrations/00005_companion.sql` 随初始发布预置，代码注释称"companion 周纪录饵效"；evidence 量级（数千）与本服 catch_log（173 条）不成比例，判定为外部导入的官服侧统计快照。**仓库内无 provenance 元数据（抓取时间/口径/分服），属已知缺口** | 官服侧推断，未证实 |
| v3 `diet_json`/`action_json` | 仓库内嵌资产 `local-sim:deterministic-v3`，docs 明示"不是官服原始生态表" | 本地合成 |
| TypeID→DiveDepth | `loadLureAttributes` 硬编码，注释明示"本地近似值，不冒充官方物理参数" | 本地近似 |
| 规则文本 bottomOnly | `fishing_bait_rules.txt` 人工攻略整理 | 本地知识 |
| TypeID↔中文名 | 静态库 `item_catalog` + 此前 14/7 实机上报与客户端弹字互证（fish_ecology.go 头注释） | 实机互证 |

客户端协议无"鱼在哪层"字段（4/22 是装备树、14/7 是拟饵动作位），水层分布是官服黑盒——
v4 是"外部统计先验 + 本地规则交叉校验"的近似基线，最终分辨力靠实机回归闭环。

防御性设计（因主驱动不可溯源）：companion 证据只占分位数 ~64%，36% 留食性先验；
动作锚定与 companion 独立；矛盾只标记不改值（剩 2 条）；全量 252 条对照表见生成报告。

## User Stories

1. 作为路亚玩家，水面系饵（波扒/老鼠/犬步）主要上层掠食鱼（狗鱼/黑鲈/鲑科），不再成批打上鲤鱼/鲶/江鳕。
2. 作为底钓玩家，跳底饵主要打底层鱼，上层滤食鱼（鲱/西鲱）不再被跳底饵成批命中。
3. 作为浮钓玩家，天然饵对拟饵向鱼种的放行收敛（PR-2/3 之上仍需食性旁路收紧配合，见 follow-up）。
4. 边界：`fish_ecology_enabled=0` 只关闭 `fishBiteEnvironmentWeight`；v4 数据/PR-2/PR-3 仍会被
   鱼猎/成就/窝料/水温光照消费，默认行为会变化（见「生效与回滚」），不再声称“完全一致”。
5. 边界：不改食性规则文本、不改钩型发鱼匹配、不动咬钩定时链。

## 门禁记录

- `go build ./...` / `go vet ./...` / `go run ./tools/doccheck` 通过；`fish_ecology`/`BaitDepth`/
  `LureMotion`/`Marine`/`Sonar`/`Hunt`/`Float`/`Bite` 全家测试绿。
- JSON diff 911/911 行（252 条鱼的 layer 三元组 + version；部分值未变），无格式噪音；除水层三元组与版本号外零字段漂移。
- 全量 `internal/gameserver` 回归见 PR 描述（含已知上游 flaky：`TestCafeNativeFlowPaysAndPushesRemoval`，
  纯上游 c502055 两遍复现，与本改动无关）。

## Follow-up（不在本 spec 范围）

- 现象 B 的根治（证据旁路加阈值/按占比计分）单独立 spec；
- 天然饵水层映射（companion 表 ~264 行 bait_id 对不上 item_def）；
- `tools/genfishdata` 文档条目改为指向 `tools/regen_fish_layers.py`。
