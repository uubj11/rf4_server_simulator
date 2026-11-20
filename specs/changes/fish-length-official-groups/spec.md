# fish-length-official-groups — 官服实测体长表 v3（拟合 + 单点锚 + 亲缘组全覆盖）

Status: in-progress

> 代码已落地（PR #522），待用户实测确认。

## Problem Statement

体长现有三层覆盖（species_weight 库值 → fish_length_scale.go 的 KOverride 188 种 /
POverride 33 种 / Linear 53 种）。2026-11 官服抓包拿到 146 种 267 个 (重量, 体长) 真值点，
残差对账发现：**鲤鱼族 + 部分海鱼 7 种（carp/c.carp/carp_ghost/coalfish/m_carp/makrel/n_carp）
旧模型系统性偏长 ~23%**（多点均值残差 -17% ~ -26%），另有若干旧校准与官服真值矛盾。

## Solution

新文件 `internal/gameserver/fish_length_official.go`（gen_tables.py 生成），三层新表 + 优先级扩展：

- `fishLengthOfficialKP`：21 种多点拟合（n≥3，log-log 回归 L=k·W^p，R²≥0.87，注释样本数/W 范围/R²/最大残差）。
  退化拟合自动降级（p 出 [0.15,0.80] 或 R²<0.5，如 makrel 3 点重量跨度过窄）。
- `fishLengthSingleK`：103 种单点/两点锚（低置信）：k = L_obs/W_obs^p，p 沿用该种现行值
  （POverride → 库非默认 → 1/3）。**实测优先（含单点），不落家族值**。
- `fishLengthGroupAnchor`：16 组组锚（组名复用 fishPullSpeciesGroup），组内第一个有标定成员的整参数，
  仅兜新旧表都不含且静态库为默认行的种。
- `calibratedLengthKP` 优先级：**官服拟合 → 单点锚 → 旧 KOverride/POverride（仍只覆盖静态库默认行）
  → 组锚 → 缺省**。DB 非默认行=旧官方标定，被新拟合/单点取代（更新鲜的官服真值优先），
  Linear 表种不入新表（53 种的 Linear 分支原样保留，重叠 22 种已排除出新表）。

### 别名继承

- 皮肤变种自身有官服实测时继续使用自身锚；没有自身实测时继承宿主的拟合/单点参数，
  避免旧 KOverride 继续把宿主已修正的偏长误差带给变种。
- `common_carp`、`m_carp_ghost` 等自身有实测的变种仍优先自身参数。

### 修复清单（残差>15%）

carp（14 点，p=0.4216）、c.carp（5 点）、carp_ghost（3 点）、coalfish（8 点）、
m_carp（7 点）、makrel（退化降级→单点）、n.pike（3 点，旧 0.0655/1.1m@5kg → 新 0.01841/0.75m@5kg）。

## User Stories

- 有官服真值的 146 种：体长按官服实测模型生成，鲤鱼族不再"又长又轻"。
- 零标定种：按亲缘组锚生成，体型符合族群。
- Linear 53 种显示不变。

## Non-Goals

- 不改 species_weight.length_k/length_p 静态库内容（内容哈希稳定原则）。
- 不动 sonar_profile / size_med 等声呐表。

## Tests

`fish_length_official_test.go`（全部通过）：

- `TestOfficialLengthFitAnchors`：42 条端点锚 ±tol%（tol=max(6%, 最大残差+1%)，官服体况抖动容差）。
- `TestOfficialLengthSingleAnchors`：单点种返回自己的 (k,p) 且在该点精确复现（±1e-4）。
- `TestLengthRepairSpecies`：7 修复种 k 必须异于旧值且端点在容差内。
- `TestLengthGroupFallback`：零标定种落组锚。
- `TestLengthLinearUntouched`：Linear 种不得进入新表（防止 Linear 分支被旁路）。
- `TestLengthCoverage`：fishPullSpeciesGroup 每键落在 拟合/单点/KOverride/组锚 之一（全覆盖）。
- `TestLengthAliasInheritsHostMeasured`：无独立实测的皮肤变种继承宿主官服参数，自身有实测者保持自身锚。

旧测试更新：`TestCalibratedLengthKP`（n.pike/carp 新拟合值；perch/c.roach/crucian 旧标定被拟合取代、
s.bream Linear 保持；crawfish 有官服单点 186g/0.2134m 不落组）、`TestCachedSpeciesLengthKPAppliesCalibration`
（carp/c.roach 缓存链路走新拟合）、`TestFishLengthCommonSenseOutliers`（b_gill 1200g 官服 0.43m、
s_freaks 30kg 官服单点 1.21m，均按官服真值放宽区间）。

## Risks

- 单点锚（103 种）只定截距，p 沿用现行值；补多点后升级拟合。
- 官服体况抖动 CV≈0.25（rf4d 507 样本回归），单点 L 本身是抖动采样——锚点测试容差已按此放宽。
- 旧 KOverride 中与新拟合矛盾的条目仍留在 Go 源码里（被优先级遮蔽），后续清理另行提交。

## Further Notes

数据源与流水线同 fish-xp-rate-table（xp_len_pairs.tsv 259 行 + fish_all/rec1/rec2 判鱼 14/14 体长点）。
拟合口径与 speciesLengthM 一致：L(米) = k · W(克)^p；W~L 幂律斜率官服实测 2.63~2.98（近立方）。
分支依赖：基于 fix/fish-pull-k-table-v2（复用 fishPullSpeciesGroup）。
