# 鱼饵偏好判定与官服证据对齐

Status: done

## Problem Statement

玩家会遇到三种说不通的情况：

1. 攻略和官服统计都写着某条鱼主吃某款饵，本地挂上却半天没口；换一款规则表里恰好写了的饵，口立刻回来。
2. 同一条鱼，后台「鱼饵偏好」页显示「会咬」，进游戏却怎么都不咬；或者反过来。
3. 规则表里写着「鱼肉」「中立类」「粪便蠕虫」这类词，实际一款都对不上；有些鱼整条规则等于空的。

根因不在鱼口概率，而在**证据链断在中间**：

- 官方渔获统计表 `rf4records_catch` 是空的，运行时按饵占比打分的 `rf4BaitShare()` 恒返回 -1，官方数据一条都没参与判定；
- 规则表是手写窄名单，与官服实测占比脱节，且存在异体字、别名指错、已下线商品名；
- 判定逻辑对坏数据过于敏感：一个坏饵 id 让整池候选归零，一款禁饵把同杆其它合法饵一起废掉；
- 后台页的「是否参与严格判定」用的是另一套口径，与运行时不一致。

## Solution

判定以三套证据并列为基础：规则表（静态）、官服占比（动态）、本地饵效证据（快照）。

- 官服占比接入判定，并按最小样本量过滤噪声；
- 规则匹配改为归一化匹配（全角/半角、连字符、空格、同义词、鱼名异体字），规则文本按官服占比补齐；
- 坏数据只影响它自己：坏饵 id 跳过、禁饵只跳过该款、多钩组不连坐；
- 后台页直接复用运行时的判定函数，页面结论与游戏行为同源。

## User Stories

1. 作为玩家，我希望攻略/官服统计里排在前面的饵挂上去真的会咬，而不是被一份手写名单挡在外面。
2. 作为玩家，我希望同一条鱼在后台页看到的「会咬 / 只能保底 / 不吃」和进游戏后的实际表现一致。
3. 边界：挂了一款规则明确不吃的饵，同杆其它合法饵仍应正常上鱼；挂了不存在的饵 id，不应让整杆变成空竿。
4. 边界：只有个位数纪录的饵不应因为「1/2 = 50%」被当成强证据。

## Implementation Decisions

- `internal/gameserver/fishing_bait_rules.go`
  - 规则词归一：`ruleKey`（全角转半角、去空格/连字符/下划线/标点）+ 同义词表 `ruleBaitCanon`（中立→中性、鱼肉/鱼肉块→鱼块、面包屑→湿面包、河鲈饵→河鲈、梭鲈饵→梭鲈、Gummi Makk→Super GummiMakk）。
  - 型号数字边界判断，避免「Hunter 1系列」误配 `Hunter 1000-001`。
  - 鱼名异体字归一 `ruleVariantRune` + `lookupFishBaitRule`（兼容「泥鳅 / 泥鳅鱼」「大西洋鲑 / 大西洋鲑鱼」「钓鮟鱇 / 钓𩽾𩾌」），所有规则查询点统一走它。
  - `loadLocalBaitEvidence` 不再按两个精确字面量查 `loc_bait_fish`，改为归一化 + 组合饵拆分比对。
  - `filterFishBaitSet`：坏饵 id `continue`；`deny` 与「底钓只吃」只跳过该款饵；满档条件中 companion 证据仅在「无规则」或「规则一款都没匹配上」时生效；多饵叠加系数由固定 ×1.15 改为随饵数收敛。
  - `loadRf4Share` 增加最小样本量（`bait_share_min_count`，默认 5）并改为先算分母再算分子。
- `internal/gameserver/fishing_roll_cache.go`：旧 id → 新 id 回退表，救回历史证据行。
- `internal/gameserver/fishing_bait_rules.txt`：按官服占比 ≥5% 补齐允许名单，清理已下线/笔误词（只增不删，除笔误词）。
- `internal/gameserver/assets/groundbait_affinity.json`：规则变更后按现有公式重算窝料亲和（只更新 `appetite/special` 与行集合，`affinity` 沿用旧值）。
- `internal/admin/bait_prefs.go`：行结构增加官服 top8 占比与「会咬 / 保底 / 不吃」判定；判定复用 `gameserver.ExportedBaitVerdict`。
- `internal/admin/web/index.html`：鱼饵偏好页新增「吃不吃」列，钓场页「爱吃的饵」列同步显示规则 / 官服占比 / 结论。
- 数据与迁移：不新增表、不改表结构；`rf4records_catch` 由既有 `tools/fetch_rf4records.py` 抓取填充，空表时行为与改动前一致（回滚安全）。
- 兼容：不改协议、不改客户端版本基线（4.0.25034）。回滚方式为还原上述文件。

## Testing Decisions

- 单元层（已有接缝，真实 SQLite）：
  - `fishing_bait_rules_test.go`：归一化匹配、同义词、型号边界、鱼名异体字。
  - `bait_preference_test.go`：禁饵只跳过自身、坏 id 不计分、空列表不计分。
  - `bait_evidence_threshold_test.go`：四档证据分级的阈值行为（含最小样本量）。
  - `fishing_bait_rule_name_test.go`（新增）：三组异体字鱼名必须命中同一规则。
  - `internal/admin/bait_prefs_test.go`：官服占比 top 排序与阈值过滤。
- 期望值来源：官服 API 抓取的纪录数/占比（可复现命令见 PR 说明）、客户端本地化表中的商品名、手工核对样例。
- 回滚后变红的测试：上述五处（尤其异体字与禁饵语义两条），符合「行为改动必带测试」。

## Out of Scope

- 体重档位线、奖杯率、`fish_weight_override` 等经济与档位问题；
- 饵料消耗/扣饵链路；
- 重建 `companion_bait_fish` 快照（本轮只修读取侧与旧 id 回退）；
- 把第三方站点（rf4db / rf4-stat / rf4map）数据写入运行时库；
- 后台页面整体改版。

## Further Notes

证据等级（定义见 `docs/agents/domain.md`）：

- `[事实]` 官服 `rf4records` 公共 API 的鱼×饵聚合：252 鱼 / 581,304 条原始纪录；饵名到本地 item_id 命中 94.8%。
- `[事实]` 独立社区源 rf4map.ru 254 个鱼页（1,270 个饵位）与官服 top15 的一致率 86.6%（0 冲突）。
- `[事实]` rf4-stat（cn 镜像）199+ 页 top 饵统计与官服同源，用于一致性校验。
- `[事实]` rf4db 饵页 929 个，其中 345 页带「玩家用它钓到的鱼」；与官服 top5 重合仅 0.63/5，故只作「这条鱼确实吃这款饵」的覆盖面证据，不作排序依据。
- `[事实]` 规则表 252 条中有 124 条含对不上任何商品的词；鱼名异体字导致 3 条鱼整条规则失效。
- `[推断]` 官方数据与 rf4map 可能同源（后者自述来源为官网与公开资料），因此 86.6% 只能算一致性校验，不是独立验证。
- 未验证：改动后实机咬口分布是否与官服占比同阶；需要玩家侧长时间采样。
