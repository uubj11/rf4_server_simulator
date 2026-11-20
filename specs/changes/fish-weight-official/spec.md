# 出鱼权重改用 rf4info 周档统计（fish_weight_override）

Status: done

## Problem Statement

`fish_weight_override` 决定每个水域「哪几条鱼、以什么比例出现在候选池里」，是判鱼权重唯一的
人工覆盖层。此前这 600 余行是后台一次性批量写入的**手调常数**（权重区间 3–25），与真实渔获结构
无关，抽查可见明显偏差：

- 廓里湖 top1 是 `riv_rb_trout`（28.4%），而周档统计里 `n.pike` 占 57.4%；
- 旧奥斯特罗格湖 top1 是 `s.bream`（18.6%），周档统计是 `bream` 49.8%；
- 阿赫图巴河 top1 是 `shemaya`（12.2%），周档统计是 `sh_barbel` 20.0%。

此外该表还混入了**不在该水域名册里**的鱼（17 个水域共 42 行），而 `internal/gameserver/fishing.go`
对 override 里名册没有的鱼会「追加进候选池并赋权」，等于让白河能钓起鲤鱼湖的鱼。

## Solution

1. 用社区公开抓取库 `ghoomt03-sketch/rf4info` 的 15 个周档（2026-W32 ~ W46）重建每图每鱼的相对权重：
   - 三张证据表按可信度加权：`catch_records`（渔获）×1.0、`community_spots`（点位）×0.5、
     `official_weekly_records`（官方周榜）×1.0；
   - 按图做幂压缩 `w ** gamma`，逐图求解使第一名占比 ≤ 20%、前三名合计 ≤ 52%；本来就分散的图
     取 `gamma=0.95`，保留 5% 压缩以统一图间口径；
   - 对压缩后权重取 `权重 / 该图最大权重 × 100`，保留两位小数，最小钳到 1.0（不置 0，避免鱼「消失」）；
   - 只写名册内存在的鱼：先按 `loc_wiki_fish` 过滤；迁移整表重置周档权重，因此不会残留名册外旧行。
2. 数据以迁移 `internal/store/migrations/00071_fish_weight_official.sql` 落库，
   `updated_by='rf4info'`；迁移按当前静态名册过滤后整表重置周档权重。
3. 同步脚本 `tools/build_fish_weights.py`：从 rf4info 周档重新生成上述迁移 SQL 与基础权重种子 SQL，
   便于后续按新周档复核。

## 数据来源与时间跨度（可追溯）

| 项 | 内容 |
| --- | --- |
| 来源 | `https://github.com/ghoomt03-sketch/rf4info`，`public_data/weeks/2026-W32.sqlite3 … 2026-W46.sqlite3` |
| 时间跨度 | **2026 年第 32 周至第 46 周（15 个周档）** |
| 有效样本 | `catch_records` 225,032 条、`community_spots` 73,524 条、`official_weekly_records` 2,099 条 |
| 覆盖 | 18 个水域、225 个鱼种有渔获记录；与名册并集后 251/252 |
| 数据形态 | 玩家渔获与官方周榜的**记录条数**，不是上鱼概率；按图内相对比例使用 |

周档样本量分布（说明为何是「3 个月」而不是「15 周等权」）：

```text
W32 5    W33 69   W34 244  W35 259  W36 305  W37 513  W38 839  W39 786
W40 853  W41 680  W42 3230 W43 10088 W44 116634 W45 85705 W46 4822
```

W32–W42 合计仅 7,783 条（3.5%），抓取器 2026-10 底才稳定，因此权重实际上主要由
**W43–W45（约 95%）** 决定，等价于「近三周的真实渔获结构」。使用限制：

- `community_spots` 是「活动点位在列」而非上鱼次数，按 0.5 降权；
- 官方周榜只有 2,099 条，仅作少量补正；
- 沃尔霍夫河、拉多加湖、草湖样本偏少（top1 渔获 10 / 156 / 233），这三图的排序稳定性弱于其他图。

## 本地复现方式

```text
# 1. 下载周档（仓库 public_data/weeks/）
gh repo clone ghoomt03-sketch/rf4info
# 2. 合并三表并生成 SQL
python tools/build_fish_weights.py --weeks ./rf4info/public_data/weeks --db ./data/rf4server.db --out ./out
# 3. 生成的 out/00071_fish_weight_official.sql 即迁移内容，核对后放入 migrations/
```

## User Stories

1. 作为玩家，我在白河下竿时遇到的是白河真实的鱼群结构（茴鱼、圆腹雅罗鱼、褐鳟），而不是别的湖的鱼。
2. 作为玩家，阿赫图巴河最常见的应该是短头梭鲃、普通鲤鱼，而不是随机抽到某条罕见鱼。
3. 作为维护者，我可以用新一周的周档重跑脚本，把权重滚到最新，不必再手调常数。

## Implementation Decisions

- 权重存的是**相对值**（该图最大值归一为 100），不是绝对概率；判鱼时乘在名册权重上。
- 猎获条数带打法/上传/时间窗偏置，不能直接当水体结构；先做保序幂压缩再归一，避免热门鱼挤掉整图多样性。
- 每个水域只保留名册内的鱼：`loc_wiki_fish` 是静态名册，override 是动态权重，两者必须一一对应。
- 名册里有、但窗口内 0 记录的鱼保留 1.0，避免「周档这周没人钓到」直接变成该鱼消失。
- 不删除任何名册项：公开周档是滚动窗口，用窗口缺失去删静态名册会误删真实存在的鱼。

## Testing Decisions

- 后台仅改咬钩参数时，未变化的周档权重必须保留 `official` 来源；只有实际改动权重/启停的行才转为 `manual`。
- 「恢复默认鱼口」在有周档基线时恢复基线，在旧库缺基线时才保持静态回退。

- `internal/store/fish_weight_official_test.go`：迁移执行后每图行数等于该图名册数、权重全部 >0、
  每图最大值 ==100、`updated_by` 全部为 `rf4info`；每图第一名占比 ≤20%、前三名合计 ≤52%。
- 迁移可重复执行：连续两次应用不报错、行数不变。
- 名册外多余行检查：`SELECT COUNT(*) FROM fish_weight_override o LEFT JOIN loc_wiki_fish w
  ON w.loc=o.loc AND w.fish_key=o.fish_key WHERE w.fish_key IS NULL` 必须为 0。

## Out of Scope

- 不修改名册本身（`loc_wiki_fish` / `map_fish_roster`）。
- 不修改稀有度、档位线、昼夜曲线。
- 不引入按季节/按玩家等级的权重变化。

## Further Notes

原始数据不进仓库：周档 sqlite 体积 28 KB–48.7 MB，按 `CONTRIBUTING.md`「不得提交数据库」只保留
聚合后的 SQL 与生成脚本；`_dist_out/fish_distribution_3m.csv` 为中间产物，同样不入库。
