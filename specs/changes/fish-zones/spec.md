# 区域固定鱼种名册 + 后台地图工作台（含素材出库）

Status: done

## Problem Statement

判鱼只有「全图名册按捕获计数加权随机」这一层，没有任何「这片水域固定出什么鱼」的
区域级名册（40m 网格只是哈希散布系数）；管理员想运营「湾口鲤区」「深坑鳕鱼区」
这类玩法无从下手。即便给出手填 gx0/gz0/gx1/gz1 的弹窗，管理员也不知道自己在划哪块
水域；区域选鱼用全鱼库通用搜索，看不到「这张图本来有哪些鱼」；重叠区域静默失效、
不规则水域框不准、地图看不清。硬门（G1/G2/G-diet）见 `specs/changes/bite-gates/`，
本变更在其上加区域名册与后台工作台。

## Solution

1. **区域模型**（`fish_zone` + `fish_zone_fish` 表，迁移 00065~00069）：四种几何——
   内陆 40m 网格矩形（grid_rect，与 `fishArea()` 同一推导，含端点）、格子刷选
   （grid_cells，`cells_json` 上限 2048 格）、圆（circle，米级）、海图区号（sea_zone，
   数据层保留字段）。模式：独占（名单内 ∩ 地图名册才出鱼）/ 增益（名单内 ×boost×weight、
   名单外 ×keep_others（0~1 合同值，表单默认 0.35，沿用海图观测语义）。
2. **叠加语义**：每块画出来的区都生效——独占名单取交集（交集空=不出鱼）、增益连乘、
   独占先过滤后增益在幸存者上继续乘，不再按 id 择一，无静默死区。重叠格子铺红斜纹
   可视化。区域名册与鱼群随机分布乘法复合。未配置区域的水域完全不受影响。
3. **挪威海暂不开放建区**（2026-11-14 决定：海图区号/坑名与判鱼信号易引起误解）：
   保存硬拒绝，数据层保留 `sea_zone`/`sea_zones_json` 字段，地图仅供浏览定位
   （声呐区号网格、📡 我在哪读数随入口一并隐藏）。查鱼助手区域评分只覆盖三种内陆几何
   （评分按落点逐格计算，不带海图区号上下文）。
4. **后台地图工作台**：「钓场与鱼口」页编辑区双页签；纸地图上拖拽画矩形（自动吸附
   40m 网格、悬停显示与游戏 HUD 同口径的显示坐标）、按住刷格子（Alt 擦除）、
   圆形半径；滚轮缩放（光标锚定 ≤12×）、✋ 平移、⤢ 复位；点已有区域编辑；选鱼默认
   本图名册（带基础权重），外来鱼种切「全库搜索」；无标定地图保留手填弹窗兜底。
   鱼汛模式卡（见 bite-gates spec）接线到同一页顶部。
5. **素材出库**：18 张官方纸地图 WebP（约 7.5MB）与声呐网格 JSON 副本不入仓库
   （CONTRIBUTING 素材红线，同水层掩码口径）。WebP 由 `tools/extract_paper_maps.py`
   导出到部署包 `ref/paper_maps/`（.gitignore `/ref/` 段内），后台按
   `/maps/{贴图名}.webp` 运行时加载，缺图返回合成占位 SVG，几何功能不受影响；
   声呐网格 JSON 只保留 fishmaps 一份，gameserver 经 `SonarGridsJSON()` 取用。

保存后最多 30 秒生效（管理队列 `reload_fish_zones` 主动失效 + 30s TTL 兜底）。

## User Stories

1. 作为管理员，我希望在真实地图上拖拽/刷选画出任意形状的区域并看到吸附网格与
   显示坐标，以便精确对应游戏内点位划区。
2. 作为管理员，我希望重叠的每块区域都生效并在图上看见重叠，以便叠加运营多个玩法区。
3. 作为玩家，我希望「湾口只出鲤鱼」这类区域玩法成立，以便不同水域有不同目标。
4. 边界：门/名册剔空候选 = 本轮无口重排（不从外图捞鱼）；拼错鱼种、乘数离谱在
   保存时被拒绝而不是得到死区；GM 指定鱼（arm_next_fish）绕过区域判鱼。

## Implementation Decisions

- 模块：`internal/gameserver/fish_zone.go`（区域匹配/叠加聚合/查鱼系数）、
  `internal/store/repos_fish_zone.go`（CRUD + Validate）、`internal/admin/fish_zones.go`
  （API + 鱼种存在性校验）、`internal/fishmaps`（标定/贴图名/声呐网格单一事实源，
  gameserver 与 admin 共用，admin 不 import gameserver）；`fishing.go` 在权重构建后
  作用名册，咬钩审计日志带命中区名目。
- 迁移 00065~00069：上游 00060~00062（skill_cast_effects 等）占用后按编号纪律改号；
  已按旧编号建过列的库经 `retireStaleMigrationRows`（按文件名退役 + 向上探测退役段
  空位）与 `renumberedColumnMigrations` 重放守卫（列已在跳过 ALTER，00068 只跑
  IF NOT EXISTS 索引段）平滑升级，端到端测试复刻部署态。
- 保存校验：kind/mode 白名单、矩形翻正、格子 ≤2048、半径 2~500、区号 ≤300、
  boost 0.1~100、keep_others 0~1（缺省归一先于校验）、fish_key 逐一核对全库名册；
  整笔事务（区域+名册替换）+ 审计日志。
- 素材：`/maps/` 路由先于 SPA FileServer 注册，`findPaperMap` 沿 findLocalFile
  同源序（工作目录→上级→可执行文件→上级）解析；客户端资源更新后重跑导出工具即可，
  无需重建。
- 兼容：无迁移的部分零 schema 变化；fishmaps 搬家后既有 17 图 1e-9 回归保证行为等价；
  回滚 = 换回旧 launcher/gameserver。

## Testing Decisions

- `fish_zone_test.go`：几何（矩形/格子/圆/海图）命中、叠加语义（独占∩独占∩增益
  系数 6/0/1 + 判鱼只出 frog）、查鱼口径；期望值手算。
- `internal/admin`：zones API 往返（含未知鱼种/乘数离谱拒绝、矩形翻正）、areas 富化
  字段手算（grid/sea/none 三分支）、bite_mode 接线。
- `internal/fishmaps`：标定形状守卫 + TextureFor 特例；gameserver 既有 17 图回归全绿。
- 迁移：`TestMigrateReplaysRenumberedZoneMigrationsOnLegacyDB` 端到端复刻
  （旧编号登记 + 退役段被占 → 改号重放全绿）。
- UI（惯例接缝 cjs，本地手跑不入 make test）：`zones_map_ui_test.cjs` 拖拽/保存/
  名册/点选/海图纯浏览（建区入口隐藏断言）/无图兜底；`fishing_rules_ui_test.cjs`
  同步既有断言。

## Non-Goals

- 不改硬门判定本身（见 `specs/changes/bite-gates/`）；窝料、鱼猎、查鱼主链路除
  区域评分外不变。
- 海图区号区域不开放创建（保留字段）；不做多边形自由绘制与刷选布尔运算；
  不做区域静态基线预置；cjs UI 测试不进 CI（无 node 门禁，手动跑）。
