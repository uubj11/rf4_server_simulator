# fish-xp-rate-table — 官服实测 XP 速率表（实测优先 + 亲缘组全覆盖）

Status: in-progress

> 代码已落地（PR #522），待用户实测确认。

## Problem Statement

模拟器 XP 走 `econ.go valueRates`（6 鱼种 11 条手工条目）+ 全局缺省 0.4 XP/g 一刀切。
2026-11 官服抓包逆向定案（见 docs 与 logs/captures/crash20261116/）：

- XP 由官服服务端计算下发（14/4 结算 813.f8→310.f0；记录卡 320.f4 同源），客户端无公式。
- 公式：**XP = trunc(c(鱼种, 档位) × W_g)**，q≈1.00（log-log 拟合 0.77~0.82 的散源是鱼种因子而非重量指数）。
- c 按 (鱼种×档位) 跨 3 个数量级（0.14~179.9，g2 中位 3.27）；旧 0.4/g 一刀切中位偏差 8 倍。
- 官服同一 (种,档) 多条鱼 XP/W 严格一致（carp g2 五条 4137~5710g 全部 trunc(0.520×W)）。

## Solution

新文件 `internal/gameserver/fish_xp_rate_table.go`（gen_tables.py 生成，数值经 float64 trunc 逐条验证）：

- `fishXPRateC`：168 组官服实测 (种,档)→c，155 组有唯一精确解（Fraction 区间交集 + 6dp 网格量化）。
  13 组同组样本不自洽（疑官服活动/加成乘子，如 ruffe 同重 ×2、atl_herring ×3）→ **取低值方向**（去加成），注释标 `[加成嫌疑]`。
- `fishXPGradeFactor = [4]float64`：档位乘子（相对 g2），多种成档实测比例取中位：
  g1=0.3063（10 种观测）、g3=6.25（10 种观测）、g4 无观测沿用 g3（保守，spec 开放项）。
- `fishXPAnchor`：146 个实测种的锚档（样本最多的档）。缺档 = 本种锚 × 档乘/锚档乘。
- `fishXPGroupC`：16 个亲缘组 × 4 档锚（组名复用拉力 K 表 `fishPullSpeciesGroup`），
  值 = 组内实测成员按档中位，缺档外推。**仅兜零实测种**（94 种）。
- `fishXPRateFor(species, grade)` 优先级：
  **本种实测 → 本种锚×档乘 → 皮肤别名→宿主整链 → 亲缘组 → 旧 valueRates → 0.4 缺省**。

关键实测修正：皮肤变种（common_carp/l_carp 等）官服有**独立经济数值**
（common_carp g3 = 6.24/g ≠ 宿主 carp g3 = 3.25/g），故本种实测/锚必须排在别名继承之前。

### 接线

- `catchValue`：xpRate 改查 `fishXPRateFor`；取整 Round→**Trunc**（官服语义）。
  银币逻辑（valueRates[1] / 缺省 0.1 / silverRateSeedDivisor）与放流链（pending.XP × release_xp_ratio）不动。
- `econ.go build813` 读 valueRates 银币速率不变。

### 实测优先原则（用户拍板）

凡有实测的种（含单点）全用实测值——单点定锚档，缺档用「本种锚 × 档位乘子」补，**不落家族值**；
亲缘组只兜零实测种。

## User Stories

- 玩家钓到有官服实测的鱼（146 种）：XP 与官服逐位一致（结算弹窗/升级曲线）。
- 玩家钓到零实测种（94 种）：XP 按亲缘组锚给出，量级正确。
- 升级礼物链路不受影响（level_gifts_enabled 开关照常）。

## Non-Goals

- 不落 `species_weight.xp_c/xp_q` 列（Go 表先行，与拉力 K 表一致；未来可迁移静态库）。
- 不动等级曲线 `levelXPReq`（30 次结算无升级样本，等级阈值另立项目采样）。
- 不追查 13 组加成嫌疑样本的官服活动规则。

## Tests

`fish_xp_rate_table_test.go`（全部通过）：

- `TestOfficialXPAnchors`：213 条官服锚点（47 结算 + 精确记录卡）逐条 `trunc(rate×W)==XP` 精确断言（回放对拍）。
- `TestXPSingleAnchorNotGroup`：单点种缺档 = 本种锚×乘子，≠ 组值。
- `TestXPGroupFallback`：零实测种落组锚。
- `TestXPAliasInheritsHost`：无实测皮肤种继承宿主。
- `TestXPCoverage`：fishPullSpeciesGroup 204 键 + 别名 16 键 × 4 档，无一落 0.4 缺省。

旧测试期望值更新（旧猜测值 → 官服真值）：progress_test（perch g2 642g 229→261）、
drink_settlement（seed 294→295 保持升级链）、qualification_settlement（bream 0.4/g → 锚外推 0.239984/g，complete 案例改 30kg）、
progress_notifications（perch g2 1120g 400→457，边界案例随移）、bait_consume_timing（关 level_gifts_enabled：
atl_cod 4200g 官服 XP=13229 会连升触发 6 级礼物 worm_01×30 并入饵堆，污染饵耗断言）。

## Risks

- 单点组的 c 未被第二条鱼验证「恒定」（115 组）；官服加成事件可能污染个别锚（已在表中标注）。
- g4 乘子沿用 g3（无观测）；后续拿到 g4 结算样本后更新 `fishXPGradeFactor` 并删此备注。
- Round→Trunc 对所有鱼种生效，非实测路径（组/缺省）XP 尾数语义变化（与官服一致化）。

## Further Notes

数据：2026-11-10~17 官服抓包 259 行配对样本（47 结算 + 189 记录卡 + 判鱼），
会话覆盖底钓/浮子/海钓/路亚/路亚2/路亚3/断电抢救；解密与解码流水线见
`logs/captures/official_trace/HANDOFF.md` 与 `logs/captures/crash20261116/`。
样本 TSV 与拟合产物（xp_rate_table.tsv 等）在 crash20261116/（不入库原始抓包，CONTRIBUTING 禁令）。
307 档案累计 XP 差分与 310.f0 逐条吻合（30/30），口径互证。
分支依赖：基于 fix/fish-pull-k-table-v2（复用 fishPullSpeciesGroup/fishPullAlias）。
