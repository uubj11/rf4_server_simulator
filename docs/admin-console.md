# 管理控制台

管理控制台直接内嵌在 `launcher` 中，通过统一控制中心 `http://127.0.0.1:9500` 的“管理后台”页签进入，不再监听独立的 `8080` 端口。admin 的 HTTP handler 挂载在 `/admin/`，前端 API 使用 `/admin/api/*`；内部 handler 仍保持 `/api/*` 路由，便于测试和独立调试。控制台使用 `config.yaml` 中的管理员凭据进行认证；登录成功后返回 JWT，供控制台和 API 使用。

该界面可以修改玩家数据、发放物品、调整经济配置以及管理会话。请仅在可信网络接口上开放。

## 玩家管理

- 按 UID 或名称搜索玩家。
- 编辑等级、经验、银币、金币和银行存款。
- 查看并修改背包和鱼护内容。
- 添加或移除鱼。
- 编辑各钓鱼技能分类的熟练度。
- 发放目录中的物品并移除已有物品。
- 将在线玩家踢下线或封禁玩家。

影响在线客户端的修改会通过 gameserver 命令队列下发。

手竿与博洛尼亚、纺车竿与枪柄竿分别共享技能等级，每组只计算一份投入；学习或重置任一项会同步整组，各项熟练度门槛仍独立。历史节点按组内最高等级读取，不批量改写旧记录，也不调整等级赠点和已有额外点数。

## 商店

- 基础商店目录为只读数据。
- 按商店和物品设置的覆盖价格与库存会单独存储。
- 价格和库存可以恢复为基础目录中的值。
- 控制台、商店浏览和购买结算使用同一套目录解析逻辑。

解析顺序和覆盖范围详见 [shop_catalog.md](shop_catalog.md)。

## 钓鱼配置

- 选择水域并配置咬钩概率和时间。
- 启用或禁用鱼种，并设置相对权重。
- 从完整鱼池中添加鱼种。
- 将水域配置重置为静态默认值。
- 「地图区域」页签：在游戏纸地图上拖拽画区域（自动吸附 40m 网格，悬停显示
  与游戏 HUD 同口径的显示坐标），点已有区域编辑；选鱼默认列出本图名册
  （带基础权重），外来鱼种切「全库搜索」。挪威海显示海图与声呐区号网格，
  按区号建区暂未开放（数据层保留 `sea_zone` 字段，硬拒绝保存，见
  `internal/admin/fish_zones.go`）；无标定地图保留「手动输入」弹窗兜底。

后续的钓鱼抽取和咬钩计时会读取这些修改，无需重启服务器。

纸地图贴图来自客户端 `Data/maps` bundle（18 张 2048×2048）。官方客户端贴图
不入仓库（素材红线，同水层掩码口径）：用离线工具导出到部署包 `ref/paper_maps/`
（已在 .gitignore 的 `/ref/` 段内），后台按 `/maps/{贴图名}.webp` 运行时加载，
缺图时显示合成占位网格，工作台几何功能不受影响：

```text
python tools/extract_paper_maps.py --client-data 'D:/Games/rf4game/Game/rf4_x64_Data' [--force]
```

期望贴图名单从 `internal/fishmaps/fishmaps.go` 解析（标定与贴图名的单一事实源），
客户端资源更新后重跑该命令即可；贴图目录缺失时无需重建，刷新页面即生效。

## 物品与鱼种图标

物品列表、鱼种列表和搜索弹窗会按需加载游戏图标。官方客户端素材同样不入仓库：
`tools/extract_item_icons.py` 从客户端导出到 `.gitignore` 覆盖的 `ref/`，再由
`tools/pack_item_icons.py` 生成单个 `ref/icons.zip`。发布装配脚本发现本地存在该包时会把
它复制进 Preview/Release 包；不存在时图标请求返回 404，前端 `onerror` 隐藏图片，
列表与所有管理功能保持可用。

```text
python tools/extract_item_icons.py --client-data '<客户端目录>/rf4_x64_Data'
python tools/pack_item_icons.py
```

导出与打包都只生成本地部署资源，不得提交客户端素材或含素材的截图。图标包在后台首次
请求时加载并按 Server 实例缓存；部署或替换 `ref/icons.zip` 后需重启管理服务。单个包
上限 128 MiB、最多 20000 个条目、单图上限 4 MiB；损坏包、路径穿越条目或超出限制时
按缺包降级。散文件仍可从 `ref/item_icons/`、`ref/fish_icons/` 直接读取，便于开发调试。

“新吃饵逻辑”是全水域开关，默认关闭。开启后按地图名册、后台启禁、已识别饵料和本地实钓搭配筛鱼；没有匹配鱼时不使用备用鱼池。明确不吃的饵优先排除，关闭后恢复旧判鱼流程。开关独立于调试预设保存，最多 30 秒影响后续判鱼，不重新抽取已咬钩的鱼；GM 指定下一条鱼仍保留原行为。

海图区号已接入：挪威海按抛竿落点所在水域定位区号，攻略里写“海图 NN 号区域”的鱼只在对应区号咬钩，“海图，区域不限”只要求当前在海图上；定位不到落点时保持中性。顺水漂流、`jig阶梯`、饵在水中运动按 `14/7` 钓组动画状态位判定，没有动作上报时不据此限制上鱼。船票与上船按 3/7 验票有效期加 `3/10` boat_use 实时上报判定：验票通过或收到上船上报视为在船上，收到下船上报立即判离船；没收到下船上报时租用到期才算已下船，本次会话没见过上船流程时保持中性。双饵沿用已有实现：多钩逐钩用各自的饵判鱼并保留组合加成。仍未接入的是内陆湖攻略网格坐标，控制台会显示这些范围。未识别且没有实钓证据的搭配暂不放行。规则随服务端发布，本次不自动联网更新统计数据。

## 技能升级概率

“数值调整 → 经验与奖励”提供三档技能升级概率：

- **原版概率**：八类技能全部写回 `0`，按熟练度衰减公式判定；
- **百分百**：八类技能全部写为 `100`，每次符合条件都增长；
- **自定义**：逐项填写 `0–100`；`0` 回退原版，`1–100` 直接按百分比判定。

原版与百分百通过 `POST /api/skill_gain_mode` 一次事务写入八个 `skill_gain_pct_*` 和 `skill_gain_mode` 标记，避免批量请求中途失败留下混合状态。固定百分比模式不叠加欢乐时光对原版概率的修正；制作按每件、挖饵按每次独立判定。

## 邮件

向玩家发送一条文本邮件，并可附带零个或多个目录物品。每项附件单独设置数量。邮件投递由 gameserver 执行。

## 审计日志

管理操作产生的变更会写入 `audit_log`。控制台提供只读的审计列表。

## API

统一入口下，以下路径均增加 `/admin` 前缀，例如登录为 `POST /admin/api/login`。除登录外，其他请求均需将 JWT 作为 bearer token 提交。独立调试 `cmd/admin` 时仍直接使用 `/api/*`。

```text
POST /api/login
GET  /api/me
GET  /api/dashboard
GET  /api/tunables
POST /api/tunables
POST /api/skill_gain_mode
GET  /api/players
GET  /api/players/detail
POST /api/players/update
POST /api/players/grant
POST /api/players/items/delete
POST /api/players/keepnet/add
POST /api/players/keepnet/delete
POST /api/players/skills/update
POST /api/players/kick
POST /api/players/ban
GET  /api/catalog/items
GET  /api/catalog/groups
GET  /api/stores
GET  /api/stores/items
POST /api/stores/items/update
POST /api/stores/items/reset
GET  /api/fish/areas        （每行附 level / map_kind grid|sea|none / texture / calibration）
GET  /api/fish/config
POST /api/fish/config
POST /api/fish/config/reset
GET  /api/fish/pool
GET  /api/fish/zones
POST /api/fish/zones
POST /api/fish/zones/delete
GET  /api/fish/bite_mode
POST /api/fish/bite_mode
POST /api/mail/send
GET  /api/audit
```

阶段奖励选择器先调用 `/api/catalog/groups?complete_only=1` 读取商品分类，再调用 `/api/catalog/items?complete_only=1&group=<key>` 读取当前分类。商品来自完整静态目录，因此活动专属商品即使没有 `shop_item` 货架记录也可以选择；竿坯、手柄、线轴和渔轮内部零件会被排除。普通目录调用不受这些筛选参数影响。

新年与周年活动均关闭时，阶段奖励编辑器处于禁用状态。开启任一活动后，奖励编辑器只显示已保存的活动分支；分支列表为空时不能编辑。移除分支会保留其已有奖励，重新加入后可以继续维护；“清空可选分支奖励”只清除当前可选分支的奖励。

活动分支 14、18 有 10 个阶段，其余活动分支有 7 个阶段；保存奖励时校验阶段是否存在和物品 ID 是否对应静态目录中的完整商品。数量表示实际件数：可堆叠物品合并，非堆叠装备逐件入包。游戏预览只显示物品种类，不表达数量；同物品多行配置的数量合计发放。关闭全部活动或从活动分支列表移除某分支后，该分支停止发奖，重新开启时可补发尚未领取的已完成阶段。
