# 硬性鱼口门、食性双票与空轮体验三件套

Status: done

## Problem Statement

鱼口混乱：路亚（纺车）在水面能钓到泥鳅，用非食谱饵能中鱼，底栖鱼能在水面被钓获。
部署库实测四个生态层开关（`fish_ecology_enabled` 无后台入口、`fish_bait_rules_enabled`、
`fish_weather_enabled`、`fish_distribution_enabled`）全部默认关闭且 `bite_probability=1`，
判鱼退化为「全图名册按捕获计数加权随机」；即便全部打开，水层/食性也只是带下限的软乘子，
荒谬组合被压低但永不剔除，权重全零时 `weightedChoice` 还会退化成纯随机保出鱼。
生态食性表 `diet_json` 的唯一消费者是窝料，咬口判定根本不读它。

## Solution

判鱼管线在水域鱼种覆盖之后插入资格裁定层：

1. **硬门 G1（拟饵×食性，硬剔除）**：钩上实际饵是拟饵（item_def 类别 17）时，
   `diet_json.lure < bite_gate_lure_diet_min`（默认 0.30）的鱼直接出候选。
   泥鳅 0.20/鲤族 0 不再攻亮片；狗鱼 2.07、江鳕 0.63、鲟族 0.30+（jig 钓鲟是真实玩法）照常。
2. **水层门 G2（软地板）**：目标水层超出 `[LayerMin−0.15, LayerMax+0.15]` 时权重乘
   `bite_gate_layer_floor`（默认 0.01），不剔除——深水鱼偶尔在浅水被钓起是有意保留的杂口。
   0.15 容差减少相邻水层误伤；当前生态表仍有 `LayerMax < 0.78` 的上层/中层鱼，
   底钓（0.93）时它们会按设计落到地板，不能宣称零误伤。
3. **食性双票 G-diet**：天然饵（类别 27）按饵类型映射到 diet 键（baitType 2/6/7→worm、
   3/4/14/15→fish、8/9/17→boilie、10/11/13→plant、12/16→crustacean、
   1→植物/煮饵/鱼虾贝任一），
   与攻略证据（fishing_bait_rules.txt / companion_bait_fish / loc_bait_fish）双票合成：
   双认原权重、单认 ×`bite_gate_diet_floor`（0.05）、零票或明确禁饵剔除。
4. **空候选语义**：门把候选剔空时走「本轮无符合条件的鱼」重排，并且经
   `fishRollContext.gateBlocked` 压制内置鱼池兜底——门拦下的杆不捞外图鱼。
5. **空轮体验三件套**：空因审计（每轮空杆带落点+40m 网格+空因原因，服务端日志不推玩家）、
   `bite_empty_retry_scale` 空轮重排间隔折扣（默认 1=不变）、
   `bite_gate_pity_rounds` 连续空杆 N 轮后放宽环境因素保底杂口（默认 0=关）。
   红线：拟饵硬剔和明确禁饵不受保底放宽——泥鳅永不攻亮片。
6. **鱼汛模式预设**：`BiteModeFor`/`BiteModeSet`（realistic=五开 / legacy=五关 / mixed，
   单事务原子写），五个开关也可在鱼群规则条逐个手调。后台页面接线随区域名册 PR。

代码默认全部关闭（遵守「开关默认关闭不得静默改变现有手感」契约）；
部署侧由管理员一键切到「官方鱼汛」。

## User Stories

1. 作为玩家，我希望用亮片收线时永远不中泥鳅，以便「专业钓法钓专业的鱼」成立。
2. 作为玩家，我希望钓层离谱时（水面钓江鳕）只有极小概率杂口，以便保留偶发惊喜但不再荒谬。
3. 作为玩家，我希望玉米钓不到泥鳅（零票）但正常钓到鲫鱼（双票），以便饵料选择有意义。
4. 边界：门剔空候选 = 本轮无口重排（不是必出鱼、不从鱼池捞外图鱼）；
   GM 指定鱼（arm_next_fish）绕过全部判鱼不受影响；切回 legacy 后字节级回到旧手感。

## Implementation Decisions

- 模块：`internal/gameserver/fish_bite_gates.go`（新，G1/G2/G-diet）、
  `fishing.go` 接线（`rollFishFromDBWithRules` C1 之后、`rollFishPoolsWithRules` 权重构建后）、
  `internal/store/repos2.go`（`BiteModeSet`/`BiteModeFor`）。
- tunables：`bite_gate_enabled`（默认 0）、`bite_gate_lure_diet_min`（0.30）、
  `bite_gate_layer_floor`（0.01）、`bite_gate_diet_min`（0.25，高于合成基线 0.2 噪声地板）、
  `bite_gate_diet_floor`（0.05）、`bite_empty_retry_scale`（1）、
  `bite_gate_pity_rounds`（0）。本 PR 提供后端参数；管理页接线随后续区域名册 PR 完成。
- 数据：不动表结构、不动静态库；`fishRollContext` 新增 `gateBlocked`/`emptyReason`/
  `pityEnv`/`EmptyStreak` 纯运行时字段。
- 兼容：客户端对鱼种选择零语义校验（14/14 发什么渲染什么），服务端任意收紧不触协议；
  全部开关默认 0，未部署预设的部署行为不变；回滚 = 切回 legacy / 关 bite_gate_enabled。
- 测试：`fish_bite_gates_test.go`（G1/G2/G-diet 全因子矩阵）、`fish_bite_pity_test.go`
  （保底只放宽环境因素、G1 红线不放宽）；既有套件全量回归
  （预存负载抖动清单内用例单跑全过）。

## Non-Goals

- 不改延迟链、概率掷骰、档位采样、嘬饵/保持率（阶段 A/B/D/E 原样）。
- 不修正 genfishdata 合成生态数据的噪声（如贝类 lure 0.59），门阈值只取在噪声带之上的线；
  数据修缮另开任务。
- 不含区域名册（`fish_zone` 表 + 后台划区 + 地图工作台），见后续区域名册 PR。
- 窝料、鱼猎、查鱼助手主链路行为不变。
