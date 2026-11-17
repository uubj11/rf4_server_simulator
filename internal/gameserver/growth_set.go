package gameserver

import "math"

// 成长套装（growth set）定义与倍率计算。
//
// 一套一个等级：等级存在 growth_set_state(uid,set_key) 上，套装内所有件共用同一等级进度。
// 每件按自己的满级目标反推倍率，所以同等级时每件「完成比例」一致，满级同时到目标。
//
// 数值锚点（见 docs/2026-11-16_成长套装_plan.md）：fish_grade_lines 蓝冠线 ×1.25 是实际可
// roll 到的重量上限；泄力取鱼拉力的 10%（体力池幂律 exp 0.22 下折算 14~22 分钟）。
const (
	// growthPartHeadroom 是配件目标承力相对竿的余量：满级时线/钩/转环/前导必须高于竿，
	// 否则竿再强也会被配件卡成新的最弱环。
	growthPartHeadroom = 1.05

	growthRoleRod    = "rod"
	growthRoleReel   = "reel"
	growthRoleLine   = "line"
	growthRoleLeader = "leader"
	growthRoleHook   = "hook"
	growthRoleSwivel = "swivel"
	growthRoleFloat  = "float"
	growthRoleSinker = "sinker"
)

type growthMember struct {
	ItemID   string
	Role     string
	BaseLoad float64 // 该件起始承力 kg；轮填起始泄力 kgf
}

type growthSetSpec struct {
	Key          string
	Name         string
	TargetLoadKg float64
	TargetDragKg float64
	LevelCap     int64
	BundleItemID string
	Members      []growthMember

	// 主线线长成长（单位米）：Base 是静态定义的整盘长度，Target 是满级长度。
	// Target<=Base 或 Base<=0 表示该套线长不参与成长（线本来就能填满线杯）。
	// 线长是物品实例字段（fields_json 的 csQty 槽），不是定义值，所以只能写实例。
	LineBaseLenM   float64
	LineTargetLenM float64
}

func (s growthSetSpec) partTargetLoadKg() float64 {
	if s.TargetLoadKg <= 0 {
		return 0
	}
	return s.TargetLoadKg * growthPartHeadroom
}

// targetFor 返回某角色在满级时的目标值：竿看目标承力，轮看目标泄力，
// 其余承力件看「竿目标 × 余量」，浮漂/铅不参与承力。
func (s growthSetSpec) targetFor(role string) float64 {
	switch role {
	case growthRoleReel:
		return s.TargetDragKg
	case growthRoleRod:
		return s.TargetLoadKg
	case growthRoleFloat, growthRoleSinker:
		return 0
	default:
		return s.partTargetLoadKg()
	}
}

// growthSets 是五套定稿参数。BundleItemID 复用官方已下架的套盒 item_id，
// 客户端本来就认识这些 id，服务端只改内含物、名称与商店条目。
var growthSets = []growthSetSpec{
	{
		// 海钓：最大鱼 cetorhinus_m 3125kg。海钓竿最低档就是 32 级船竿，
		// 所以解锁也放到 32，符合「海竿后期解锁」的设定。
		Key: "growth_sea", Name: "海钓成长套装", TargetLoadKg: 3125, TargetDragKg: 312.5,
		LevelCap: 32, BundleItemID: "sb_box",
		// 主线 braid_40907 本身 1200 米，远大于该套线杯容量，不需要线长成长。
		LineBaseLenM: 1200, LineTargetLenM: 1200,
		Members: []growthMember{
			{"boatr_10222_SBH80_100", growthRoleRod, 238.39},
			{"spin_5721", growthRoleReel, 38.55},
			{"braid_40907", growthRoleLine, 205},
			{"mn_lm_40809", growthRoleLeader, 181},
			{"triple_21070", growthRoleHook, 660},
			{"snap_n_swivel_40850", growthRoleSwivel, 250.75},
		},
	},
	{
		// 底钓：最大鱼 beluga 1250kg。竿取 16 级 carp_10047_360XH，线取 20 级尼龙，
		// 解锁 20 级——四套前期套的上限档。
		// 只配 竿 + 轮 + 线 + 钩 四件：沉底钓组（12）的引线槽只收氟碳前导
		// （品类 13，承力 20kg 级），尼龙引线（品类 15，113kg）装不进去；
		// 转环在所有沉底钓组里都没有独立槽位。钩由玩家自选，承力走套装等级托底。
		Key: "growth_bottom", Name: "底钓成长套装", TargetLoadKg: 1250, TargetDragKg: 125,
		LevelCap: 20, BundleItemID: "box_boilies_op",
		// 主线 mono_15279 本身 500 米，已超过线杯容量，不需要线长成长。
		LineBaseLenM: 500, LineTargetLenM: 500,
		Members: []growthMember{
			{"carp_10047_360XH", growthRoleRod, 39.01},
			{"spin_6020", growthRoleReel, 18.10},
			{"mono_15279", growthRoleLine, 56},
			// 钩必须是「淡水单钩」型（fopbhlmkcpe=1）。上一版给 triple_21069（SW Tristar 14/0），
			// 它是海钓三本钩（钩型 3），承力 495kg 虽然最强，但客户端在沉底钓组（12）的钩槽
			// 根本不渲染这种钩型——2026-11-16 实机截图：钓组界面只显示渔轮和线，钩/沉子/饵
			// 三行全是灰色「必须的组件」。换成 clas_20112「CHK 501 S1」（钩型 1、承力 22.8kg、
			// 需 8 级）后正常显示。承力差距由套装等级托底（满级按竿目标 ×1.05 抬到 1312kg）。
			{"clas_20112", growthRoleHook, 22.8},
		},
	},
	{
		// 路亚：最大鱼 osetr_balt 375kg。竿/线/前导全部压到 16 级，解锁 16。
		// 只配 竿 + 轮 + 线 + 前导 四件：rig_Lure（人造拟饵钓组）的组件表里
		// 没有「独立钩」和「转环」这两行——钩挂在拟饵上，转环由竿/线/前导自带。
		// 上一版塞了 triple_21062 和 snap_n_swivel_40848，服务端写进了 rig_component
		// （slot12@250 / slot14），客户端没有对应槽位，界面必然是空的。
		// 引线同理不能用尼龙（品类 15）：引线槽只收氟碳类（品类 13），
		// 改用 HiPower Marine Leader 170LB 1m（承力 77kg，是品类 13 的上限档）。
		// 钩与拟饵改为玩家自选：选什么钩决定 gape（钓大鱼还是小鱼），
		// 承力下限由套装等级托底（growthPartFloorForRod），不会因为选小钩就断线。
		Key: "growth_spin", Name: "路亚成长套装", TargetLoadKg: 375, TargetDragKg: 37.5,
		LevelCap: 16, BundleItemID: "box_spin_start",
		// 主线 braid_15048 静态只有 110 米，装进 spin_5140_sbtn（Venga SBH 10000，
		// 线杯 0.48 毫米 / 502 米）只填了 22%，所以这套的线长必须跟着等级长。
		// 满级正好补到线杯容量：L0=110、L3=184、L16=502。
		LineBaseLenM: 110, LineTargetLenM: 502,
		Members: []growthMember{
			{"spin_10105_SPXF98H", growthRoleRod, 36.13},
			{"spin_5140_sbtn", growthRoleReel, 31.70},
			{"braid_15048", growthRoleLine, 36},
			{"mar_leader_40834", growthRoleLeader, 77},
		},
	},
	{
		// 浮漂：最大鱼 m_carp 镜鲤蓝冠 40kg（克马羚诺也湖，L01 即开；官方 tele 竿该水域纪录
		// 25.463kg 鲤鱼）。原定 n_carp 无鳞鲤 45kg 已废：它只在铜湖(L18)有实测饵覆盖，
		// 8 级套去不了，等于纸上目标。
		// tele 竿本身带轮座，可以放线拉扯，属于带轮套装而不是无轮硬抗——解锁 8。
		// 泄力取 17.8kgf：按鲤科体力池（cyprinid 1.38、grade4 取满）反解，这是半小时窗口
		// 能拉起 40kg 蓝冠的最小泄力；轮 base 6.4 → 成长 2.78 倍，与其它套的轮成长幅度同量级。
		// 钩改 clas_20218（18mm、gape 62.99kg、承力 42kg、需 8 级）：原 clas_20161 只有 5mm，
		// 对 40kg 鱼 r=40/1.35=29.6，普通鱼 hold 掉到 0.50，属于必然脱钩；18mm 后 r=0.63、hold=1.00。
		// 不配前导；漂与铅是末端件，不成长、由玩家自选。
		Key: "growth_float", Name: "浮漂成长套装", TargetLoadKg: 100, TargetDragKg: 17.8,
		LevelCap: 8, BundleItemID: "hvz_waggler_box",
		// 主线 braid_15124 本身 350 米，远大于该套线杯容量，不需要线长成长。
		LineBaseLenM: 350, LineTargetLenM: 350,
		Members: []growthMember{
			{"tele_10254_26", growthRoleRod, 10.52},
			{"spin_5184", growthRoleReel, 6.4},
			{"braid_15124", growthRoleLine, 25},
			{"clas_20218", growthRoleHook, 42},
		},
	},
	{
		// 手杆：最大鱼 bh_carp 鳙鱼蓝冠 45kg，f8 拉力 89.9kgf（herb_carp 18.2 组）。
		// 原定 w_catfish 欧洲六须鲇 200kg 已下调：官方最强 match 竿（Blade Monster Match，32 级）
		// 承力上限只有 65kg，200kg 是 3 倍越级；而且单钩物理上限就是 18mm / 63kg（classic 族），
		// 200kg 鱼 r=3.17，普通鱼 hold 掉到 0.33，非蓝冠必脱钩。降到 45kg 后 r=0.71、hold=1.00。
		// 45kg 鳙鱼在苏拉河（水域编号 L08，8 级开）有实测饵覆盖，12 级解锁即可达。
		// 手持竿不配轮：没有泄力放线缓冲，鱼拉力整条压在竿上，所以承力取 115（1.28 倍余量）。
		//
		// 竿型必须是 tele 伸缩手杆，不能是 match 比赛竿：match 竿在客户端属于「竞赛钓组」，
		// 渔轮是硬性必填项，不装轮整根竿拿不起来（2026-11-16 实机截图：渔轮行标红「必须的组件」）。
		// tele 竿走简单浮标钓组，竿 + 线 + 漂 + 钩 四件即可抛投。
		//
		// 竿 tele_10166_BK8「梁赞 BK8」：8 级可买、承力 9.10kg、测试重量 2~25g，是 12 级以内
		// 能买到的 tele 竿里承力最高的一档（其余 7~8.8kg）。
		// 线 braid_15120「Hybrid - 22LB 350M Black」：10.0kg、3 级，与梁赞 BK8 的 9.1kg 匹配。
		// 线不能显著强于竿：无轮手杆没有泄力放线，拉力整条压在竿上，线过强会先爆竿。
		// 钩 clas_20472「Rage Series S2 S4/0」：60kg，对 45kg 鳙鱼 r=0.71、hold=1.00。
		//
		// 不配铅也不配转环：实机截图里浮标/竞赛钓组的配重由客户端「沉子 自动安装」给出，
		// 玩家手动塞的 1.75g 咬铅与转环在组件列表里根本不渲染，装了也是隐形冗余。
		//
		// 漂必须是「普通(classic, 1)」或「博洛尼亚(bolognese, 2)」型：tele 竿在客户端只允许
		// rig 1 / 3 / 30 / 31，其中 rig 1 的浮子槽写死为「普通的或者博洛尼亚(Bolognese)浮子」。
		// 上一版塞的 BK-14 是 waggler 型（3），槽位直接判为不匹配，界面显示成空槽并标「必须的组件」。
		// bob_25084「Original 215-080」：博洛尼亚型、载重 8.00g、自重 1.9g、需 12 级，
		// 是 12 级以内能买到的最大载重博洛尼亚漂（其余同型只有 3g 档）。
		// 不配铅：截图上「沉子 质量 0.25 克 自动安装」，配重由客户端按「载重 − 自重」自己补。
		//
		// 无轮所以泄力目标为 0；承力仍取 115kg（45kg 鳙鱼 f8 拉力 89.9kgf 的 1.28 倍余量），
		// 满级由 tele 竿 9.10kg 成长约 12.6 倍达到。
		Key: "growth_pole", Name: "手杆成长套装", TargetLoadKg: 115, TargetDragKg: 0,
		LevelCap: 12, BundleItemID: "box_feeder_start",
		// 手杆无轮，主线 braid_15120 本身 350 米，不参与线长成长。
		LineBaseLenM: 350, LineTargetLenM: 350,
		Members: []growthMember{
			{"tele_10166_BK8", growthRoleRod, 9.10},
			{"braid_15120", growthRoleLine, 10.0},
			{"clas_20472", growthRoleHook, 60},
			{"bob_25084", growthRoleFloat, 0},
		},
	},
}

// lineLengthAt 返回该套在给定等级下主线应有的长度（米）。
//
// 与承力倍率同一套线性口径：level/cap 的完成比例直接从静态整盘长度推到满级长度，
// 满级刚好填满该套渔轮的线杯。Target<=Base 表示该盘线本来就不短于线杯容量，
// 返回 0 让调用方跳过，绝不把原本更长的线削短。
func (s growthSetSpec) lineLengthAt(level int64) int64 {
	if s.LineBaseLenM <= 0 || s.LevelCap <= 0 {
		return 0
	}
	if s.LineTargetLenM <= s.LineBaseLenM {
		return 0
	}
	if level < 0 {
		level = 0
	}
	if level > s.LevelCap {
		level = s.LevelCap
	}
	v := s.LineBaseLenM + (s.LineTargetLenM-s.LineBaseLenM)*(float64(level)/float64(s.LevelCap))
	return int64(math.Round(v))
}

func growthSetSpecFor(key string) (growthSetSpec, bool) {
	for _, s := range growthSets {
		if s.Key == key {
			return s, true
		}
	}
	return growthSetSpec{}, false
}

// growthSetForBundle 把套盒 item_id 映射回套装定义，购买与开箱都靠它认领归属。
func growthSetForBundle(itemID string) (growthSetSpec, bool) {
	for _, s := range growthSets {
		if s.BundleItemID == itemID {
			return s, true
		}
	}
	return growthSetSpec{}, false
}

// growthItemMultiplier 按等级进度把某件从 base 线性推到 target。
// level=0 返回 1.0（原样），level=cap 返回 target/base，中间按比例。
//
// 若 base 已经高于 target（例如手杆套的转环 250.75kg 对一个只要 120.75kg 的套装目标），
// 成长没有意义——此时恒返回 1.0，绝不把原本更强的件按倍率缩小。否则「升级」反而会让
// 配件变弱，玩家满级时凭空损失承力。
func growthItemMultiplier(target, base float64, level, cap int64) float64 {
	if base <= 0 || target <= 0 || cap <= 0 {
		return 1.0
	}
	if target <= base {
		return 1.0
	}
	if level < 0 {
		level = 0
	}
	if level > cap {
		level = cap
	}
	full := target/base - 1.0
	return 1.0 + full*(float64(level)/float64(cap))
}

// growthRoleForMember 把套装成员 item_id 映射成角色，用于展开购买时逐件登记归属。
func growthRoleForMember(spec growthSetSpec, itemID string) string {
	for _, m := range spec.Members {
		if m.ItemID == itemID {
			return m.Role
		}
	}
	return ""
}
