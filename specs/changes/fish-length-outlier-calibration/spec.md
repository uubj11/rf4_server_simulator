# 异常鱼种体长校准

Status: done

## Problem Statement

全量扫描 `species_weight` 的 252 个鱼种后，发现一批鱼的体长明显短于同重量
常见体型，客户端按体长等比缩放模型时表现为“体型偏小”。最典型的是
`gc_smelt`（湖鳟鱼）：4.558kg 被算成 0.192m，10kg 奖杯线只有 0.250m。

根因是部分鱼种的静态 `max_g` 只是低样本观测值，体长 `k` 却按这个上限
校准；重量档位又按更高的奖杯线/超级奖杯线发鱼，造成重量和体长两套口径。
`gc_smelt` 的静态 `max_g=1648g`，而公开统计的廓里湖最大重量为 17,995g。

## Solution

按常见体型补一批异常鱼种的 `k/p`，对静态曲线不是默认值的 `s.bream`
改用线性体长表。优先处理扫描中条件因子 `K=100·W/L³` 明显偏大的鱼种：

- `gc_smelt`：湖鳟鱼按 1kg≈0.40m、10kg≈0.95m、18kg≈1.2m；
- `midori_g`、`m_scorpius`、`b_gill`、`fwdrum`、`s_freaks`、
  `l_minnow`、`syb_vendace`；
- `b_omul`、`pinagor`、`s_bass`、`black_crappie` 按常见区间补齐；
- `s.bream` 补 0.05~1.2kg 线性段：0.7kg≈0.325m、1.2kg≈0.45m。

这些是经验/常见体型值，不是官服成对实测点；后续拿到官服体重-体长记录后
应替换为实测曲线。

## Testing Decisions

- 新增 `TestFishLengthCommonSenseOutliers`，对每个补丁鱼种在代表性重量上
  断言常见体长区间；`s.bream` 同时验证 0.7kg / 1.2kg 两个线性锚点。回滚后
  `gc_smelt` 等会重新落到明显偏小的值并变红。
- 全量扫描结果保存在 `ref/fish_length_scan.csv`（本地参考，不提交）。

## Out of Scope

- 修改 `species_weight.max_g` 或奖杯线本身；本次只保证下发体长与重量匹配。
- 为所有 252 个鱼种补官服实测曲线；本次只处理扫描出的明显异常。
- 回填历史 `catch_log` / `keepnet_items` 的旧体长。
