package gameserver

import (
	"context"
	"encoding/json"
	"math"
	"strings"
	"testing"

	"rf4server/internal/store"
)

// 五套的满级目标与起始承力来自 fish_grade_lines 蓝冠线 ×1.25 与"泄力 = 鱼拉力 10%"口径。
// 角色归一是「按各自目标反推每件自己的倍率」，所以同等级下每件的倍数不同，
// 但同一等级进度相同，最终同时到目标。
func TestGrowthItemMultiplierReachesTargetPerSet(t *testing.T) {
	for _, tc := range []struct {
		key  string
		base float64
	}{
		{"growth_sea", 238.39},
		{"growth_bottom", 39.01},
		{"growth_spin", 36.13},
		{"growth_float", 10.52},
		{"growth_pole", 14.01},
	} {
		spec, ok := growthSetSpecFor(tc.key)
		if !ok {
			t.Fatalf("套装定义缺失: %s", tc.key)
		}
		if spec.LevelCap < 6 {
			t.Fatalf("%s 级数过少: %d", tc.key, spec.LevelCap)
		}
		if spec.TargetLoadKg <= 0 {
			t.Fatalf("%s 目标承力未配置", tc.key)
		}
		if got := growthItemMultiplier(spec.TargetLoadKg, tc.base, 0, spec.LevelCap); math.Abs(got-1.0) > 1e-9 {
			t.Fatalf("%s 零级倍率应为 1.0，实得 %v", tc.key, got)
		}
		want := spec.TargetLoadKg / tc.base
		got := growthItemMultiplier(spec.TargetLoadKg, tc.base, spec.LevelCap, spec.LevelCap)
		if math.Abs(got-want) > 1e-9 {
			t.Fatalf("%s 满级倍率应 %.6f，实得 %.6f", tc.key, want, got)
		}
	}
}

// 解锁等级按「该套能钓到的最大鱼种」分档，且目标鱼必须落在该档解锁的水域里、有实测饵覆盖：
// 浮漂 40kg 镜鲤→8 级（克马羚诺也湖 L01）、手杆 45kg 鳙鱼→12 级（苏拉河 L08）、
// 路亚 375kg→16 级、底钓 1250kg→20 级（这四套必须 20 级以内可用）、海钓 3125kg→32 级。
func TestGrowthSetUnlockTierByBiggestFish(t *testing.T) {
	want := map[string]int64{
		"growth_float":  8,
		"growth_pole":   12,
		"growth_spin":   16,
		"growth_bottom": 20,
		"growth_sea":    32,
	}
	for key, cap := range want {
		spec, ok := growthSetSpecFor(key)
		if !ok {
			t.Fatalf("套装定义缺失: %s", key)
		}
		if spec.LevelCap != cap {
			t.Fatalf("%s 解锁等级应为 %d，实得 %d", key, cap, spec.LevelCap)
		}
		if key != "growth_sea" && spec.LevelCap > 20 {
			t.Fatalf("%s 必须在 20 级以内解锁，实得 %d", key, spec.LevelCap)
		}
	}
}

// 骨架件（竿/轮/线/前导/钩）不得跨套共用，否则一件货被两套引用会重现数据重叠。
// 漂和铅同样必须专属：用户口径是「同一个配件不能搭配多个套装」，
// 浮漂钓组的漂铅一旦被两套共用，同样会出现数据重叠。
// 转环是通用金属件，允许同 item_id 但各自 guid 独立归属。
func TestGrowthSkeletonPartsAreSetExclusive(t *testing.T) {
	seen := map[string]string{}
	for _, s := range growthSets {
		for _, m := range s.Members {
			switch m.Role {
			case growthRoleRod, growthRoleReel, growthRoleLine, growthRoleLeader,
				growthRoleHook, growthRoleFloat, growthRoleSinker:
			default:
				continue
			}
			if prev, dup := seen[m.ItemID]; dup {
				t.Fatalf("%s 与 %s 共用配件 %s", prev, s.Key, m.ItemID)
			}
			seen[m.ItemID] = s.Key
		}
	}
}

// 手杆套必须是真正的伸缩手杆（tele）。match 竿在客户端属于「竞赛钓组」，
// 渔轮是硬性必填项：不装轮整根竿拿不起来（2026-11-16 实机截图确认）。
// tele 竿走简单浮标钓组，只需 竿 + 线 + 漂 + 钩 四件：不配轮、不配铅、不配转环——
// 竞赛/浮标钓组的配重由客户端「沉子 自动安装」自己算，玩家手动塞的铅会被忽略。
func TestPoleSetIsReellessTelescopicFloatRig(t *testing.T) {
	spec, ok := growthSetSpecFor("growth_pole")
	if !ok {
		t.Fatalf("growth_pole 缺失")
	}
	var rodID, lineID, hookID, floatID string
	var rodLoad, lineLoad float64
	for _, m := range spec.Members {
		switch m.Role {
		case growthRoleRod:
			rodID, rodLoad = m.ItemID, m.BaseLoad
		case growthRoleLine:
			lineID, lineLoad = m.ItemID, m.BaseLoad
		case growthRoleHook:
			hookID = m.ItemID
		case growthRoleFloat:
			floatID = m.ItemID
		case growthRoleReel, growthRoleSwivel, growthRoleSinker:
			t.Fatalf("tele 手杆套不该含 %s 件：%s", m.Role, m.ItemID)
		}
	}
	if !strings.HasPrefix(rodID, "tele_") {
		t.Fatalf("手杆套的竿必须是 tele 伸缩手杆，实得 %q", rodID)
	}
	if rodID == "" || lineID == "" || hookID == "" || floatID == "" {
		t.Fatalf("四件不全：rod=%q line=%q hook=%q float=%q", rodID, lineID, hookID, floatID)
	}
	// 无轮手杆没有泄力放线，拉力整条压在竿上；线若明显强于竿，断的会是竿而不是线。
	if lineLoad > rodLoad*1.2 {
		t.Fatalf("线承力 %.2fkg 远高于竿承力 %.2fkg：无轮手杆会先断竿", lineLoad, rodLoad)
	}
	if spec.TargetDragKg != 0 {
		t.Fatalf("无轮手杆套不该有泄力目标，实得 %.1f", spec.TargetDragKg)
	}
	if spec.TargetLoadKg <= 0 {
		t.Fatalf("手杆套 TargetLoadKg 未配置")
	}
}

// 手杆套的漂必须被「简单浮标钓组」的浮子槽接受。
// tele 竿在客户端只允许 rig 1 / 3 / 30 / 31，其中 rig 1 的浮子槽写死为
// 「普通的或者博洛尼亚(Bolognese)浮子」，即漂型 fopbhlmkcpe 只能是 1（普通）或 2（博洛尼亚）。
// 上一版配的 BK-14 是 waggler 型（3），客户端判为不匹配：界面显示空槽并标「必须的组件」，
// 玩家以为套装缺件。这是实机截图复现过的回归点，必须用静态库里的真实漂型来守。
func TestPoleSetFloatAcceptedBySimpleFloatRig(t *testing.T) {
	spec, ok := growthSetSpecFor("growth_pole")
	if !ok {
		t.Fatalf("growth_pole 缺失")
	}
	var floatID string
	for _, m := range spec.Members {
		if m.Role == growthRoleFloat {
			floatID = m.ItemID
		}
	}
	if floatID == "" {
		t.Fatalf("手杆套缺漂，装不出浮标钓组")
	}
	db := openStaticTestDB(t)
	defer db.SQL.Close()
	var attrsJSON string
	if err := db.SQL.QueryRow(`SELECT attrs_json FROM item_def WHERE item_id=? LIMIT 1`, floatID).Scan(&attrsJSON); err != nil {
		t.Fatalf("静态库查不到漂 %s: %v", floatID, err)
	}
	var attrs map[string]any
	if err := json.Unmarshal([]byte(attrsJSON), &attrs); err != nil {
		t.Fatalf("%s attrs_json 解析失败: %v", floatID, err)
	}
	kind, ok := attrs["fopbhlmkcpe"].(float64)
	if !ok {
		t.Fatalf("%s 没有漂型字段 fopbhlmkcpe", floatID)
	}
	if kind != 1 && kind != 2 {
		t.Fatalf("%s 的漂型是 %v：rig_FloatSimple 只收普通(1)/博洛尼亚(2)，waggler(3) 会显示成空槽",
			floatID, kind)
	}
}

// 玩家自选的钩/拟饵不在 growth_item 里、没有自己的倍率。如果完全不给托底，
// 一把低等级的弱钩配满级竿，钩永远是最弱环，"套装能钓大鱼"就是假话。
// 规则：托底值 = 竿目标承力 × 1.05 × 等级进度，自选件取 max(自身, 托底)，只托不削。
func TestGrowthRodGivesSelectedHookFloor(t *testing.T) {
	srv, db := newStaticRigTestServer(t)
	defer db.SQL.Close()
	ctx := context.Background()

	const (
		uid     = int64(424242)
		rodGUID = "aaaabbbbccccddddeeeeffff00001111"
	)
	if got := srv.growthPartFloorForRod(ctx, rodGUID, growthRoleHook); got != 0 {
		t.Fatalf("未登记的竿不该有托底，得到 %v", got)
	}
	if _, err := db.SQL.ExecContext(ctx,
		`INSERT INTO growth_item(guid,uid,set_key,role,created_ts) VALUES(?,?,'growth_spin','rod',0)`,
		rodGUID, uid); err != nil {
		t.Fatal(err)
	}
	if got := srv.growthPartFloorForRod(ctx, rodGUID, growthRoleHook); got != 0 {
		t.Fatalf("等级为 0 时不该有托底，得到 %v", got)
	}
	if _, err := db.SQL.ExecContext(ctx,
		`INSERT INTO growth_set_state(uid,set_key,level,xp) VALUES(?,'growth_spin',8,0)`, uid); err != nil {
		t.Fatal(err)
	}
	// 路亚套 TargetLoadKg 375，零件目标 375×1.05，半程 = 375 × 1.05 × 0.5
	want := 375.0 * growthPartHeadroom * 0.5
	if got := srv.growthPartFloorForRod(ctx, rodGUID, growthRoleHook); math.Abs(got-want) > 1e-6 {
		t.Fatalf("托底值 = %v，期望 %v", got, want)
	}
	// 不承担力的角色不该拿到托底
	if got := srv.growthPartFloorForRod(ctx, rodGUID, growthRoleFloat); got != 0 {
		t.Fatalf("浮漂不该有承力托底，得到 %v", got)
	}
}

// 托底只补不削：玩家自己带更强的钩时，立刻用他自己的数值。
func TestGrownPartNeverWeakensOwnGear(t *testing.T) {
	if got := grownPartKg(120, 100); got != 120 {
		t.Fatalf("自身更强时被削弱了：%v", got)
	}
	if got := grownPartKg(5, 196.875); got != 196.875 {
		t.Fatalf("弱件没被托底：%v", got)
	}
}

// 每套必须自带成长钩：钩的物理上限只有 660kg（单钩 clas 更低），不纳入成长的话，
// 底钓 1250kg / 海钓 3125kg / 手杆 250kg / 浮漂 56kg 都会在末端被钩卡断。
// 玩家仍可换任意钩自选钓大鱼或小鱼，但换掉就没有套装的钩倍率了。
func TestEverySetHasGrowthHook(t *testing.T) {
	for _, s := range growthSets {
		var hookBase, leaderBase float64
		for _, m := range s.Members {
			switch m.Role {
			case growthRoleHook:
				hookBase = m.BaseLoad
			case growthRoleLeader:
				leaderBase = m.BaseLoad
			}
		}
		if hookBase > 0 {
			continue
		}
		// man-made lure rig（rig_Lure）的组件表里没有「独立钩」这一行：钩挂在拟饵上，
		// 拟饵与钩都由玩家自选。这类套装必须自带成长前导，末端承力链才有锚点，
		// 玩家的钩靠 growthPartFloorForRod 的等级托底，不会成为新的最弱环。
		if leaderBase <= 0 {
			t.Fatalf("%s 既没有成长钩也没有成长前导，承力链会在末端断掉", s.Key)
		}
	}
}

// 手杆（match 竿，持竿式中没有轮座）不配轮：配轮会平白多一道高等级门槛，
// 正是上一版被 spin_5691(32 级) 卡死、21 级也装不上的根因之一。
// 浮漂竿（tele）本身带轮座，可以放线拉扯，属于带轮套装，不在此列。
func TestPoleSetHasNoReel(t *testing.T) {
	spec, ok := growthSetSpecFor("growth_pole")
	if !ok {
		t.Fatalf("套装定义缺失: growth_pole")
	}
	for _, m := range spec.Members {
		if m.Role == growthRoleReel {
			t.Fatalf("growth_pole 不应配轮，实得 %s", m.ItemID)
		}
	}
}

// 浮漂竿（tele）带轮座：带轮套装靠泄力放线控制张力，泄力必须能在半小时内拉起该套最大鱼。
// m_carp 镜鲤蓝冠 40kg；体力率 = verifiedStaminaPerGram × cyprinid 1.38 × (0.66+0.58)，grade4 取满。
func TestFloatSetReelLandsBiggestFishInHalfHour(t *testing.T) {
	spec, ok := growthSetSpecFor("growth_float")
	if !ok {
		t.Fatalf("growth_float 缺失")
	}
	hasReel := false
	for _, m := range spec.Members {
		if m.Role == growthRoleReel {
			hasReel = true
		}
	}
	if !hasReel {
		t.Fatalf("浮漂竿 tele 带轮座，套装应配轮")
	}
	if spec.TargetDragKg <= 0 {
		t.Fatalf("浮漂轮泄力未设")
	}
	rate := verifiedStaminaPerGram * 1.38 * (0.66 + 0.58)
	capG := halfHourCapGrams(spec.TargetDragKg, rate, defaultFishTimeCapSeconds)
	if capG < 40000 {
		t.Fatalf("浮漂泄力 %.2fkgf 半小时只能拉起 %d g，够不到 m_carp 镜鲤蓝冠 40000 g",
			spec.TargetDragKg, capG)
	}
}

// TestReellessSetsOutpullBiggestFish 锁死一条物理规则：没有轮的套装没有泄力放线缓冲，
// 鱼拉力一旦超过竿承力就直接断线（有轮套装则是打滑放线、张力恒定，所以可以低于拉力）。
// 拉力口径 f8(N) = w_kg × (K0 + fishPullLnSlope×ln w_kg)，K0 走 fishPullK0 / fishPullGroupK0。
func TestReellessSetsOutpullBiggestFish(t *testing.T) {
	pullKgf := func(species string, wKg float64) float64 {
		k0 := 0.0
		if k, ok := fishPullK0[species]; ok {
			k0 = k
		} else if g, ok := fishPullSpeciesGroup[species]; ok {
			k0 = fishPullGroupK0[g]
		}
		if k0 <= 0 {
			t.Fatalf("%s 缺 K0，无法核算拉力", species)
		}
		return wKg * (k0 + fishPullLnSlope*math.Log(wKg)) / newtonPerKgf
	}
	cases := []struct {
		key     string
		species string
		wKg     float64
	}{
		{"growth_pole", "bh_carp", 45}, // 鳙鱼蓝冠 45kg，手杆是唯一无轮套装
	}
	for _, tc := range cases {
		spec, ok := growthSetSpecFor(tc.key)
		if !ok {
			t.Fatalf("%s 缺失", tc.key)
		}
		need := pullKgf(tc.species, tc.wKg) * 1.15 // 硬抗 15% 余量
		if spec.TargetLoadKg < need {
			t.Fatalf("%s 无轮，必须硬抗 %s %.0fkg 的拉力 %.0fkgf（含 15%% 余量），现只给 %.0f",
				tc.key, tc.species, tc.wKg, need, spec.TargetLoadKg)
		}
	}
}

// TestGrowthItemMultiplierNeverShrinks 锁死「升级不能变弱」：
// 手杆套的转环 base 250.75kg 已经高于该套配件目标 120.75kg，按倍率公式会被算成 0.48 倍，
// 玩家满级时反而从 250kg 掉到 120kg。base ≥ target 时必须恒为 1.0。
func TestGrowthItemMultiplierNeverShrinks(t *testing.T) {
	spec, ok := growthSetSpecFor("growth_pole")
	if !ok {
		t.Fatalf("growth_pole 缺失")
	}
	for _, base := range []float64{250.75, 200, spec.partTargetLoadKg() + 1} {
		for lv := int64(0); lv <= spec.LevelCap; lv++ {
			if got := growthItemMultiplier(spec.partTargetLoadKg(), base, lv, spec.LevelCap); got < 1.0-1e-9 {
				t.Fatalf("base %.2f 高于目标 %.2f 时倍率不得小于 1.0，level=%d 实得 %.4f",
					base, spec.partTargetLoadKg(), lv, got)
			}
		}
	}
}

func TestGrowthItemMultiplierIsMonotonic(t *testing.T) {
	spec, ok := growthSetSpecFor("growth_sea")
	if !ok {
		t.Fatal("缺少海钓套装定义")
	}
	prev := 0.0
	for lv := int64(0); lv <= spec.LevelCap; lv++ {
		got := growthItemMultiplier(spec.TargetLoadKg, 238.39, lv, spec.LevelCap)
		if lv > 0 && got <= prev {
			t.Fatalf("等级 %d 倍率未上升: %v -> %v", lv, prev, got)
		}
		prev = got
	}
}

// 满级时配件承力必须高于竿，否则线/钩变成新的最弱环，竿再强也拉不起来。
// 起始状态线本来比竿弱（原装备如此），但差距必须随等级收窄。
func TestGrowthPartsStayAboveRodAtCap(t *testing.T) {
	spec, ok := growthSetSpecFor("growth_sea")
	if !ok {
		t.Fatal("缺少海钓套装定义")
	}
	rodBase, partBase := 238.39, 181.0 // Leader Nylon 400LB 是原套里最弱的那件
	rodCap := rodBase * growthItemMultiplier(spec.TargetLoadKg, rodBase, spec.LevelCap, spec.LevelCap)
	partCap := partBase * growthItemMultiplier(spec.partTargetLoadKg(), partBase, spec.LevelCap, spec.LevelCap)
	if partCap < rodCap*1.049 {
		t.Fatalf("满级配件 %.2f 未高于竿 %.2f 的 1.05 倍", partCap, rodCap)
	}
	if ratio0, ratioCap := partBase/rodBase, partCap/rodCap; ratioCap <= ratio0 {
		t.Fatalf("配件/竿 比值未随等级收窄: %.4f -> %.4f", ratio0, ratioCap)
	}
}

// 单件归属与套装持有：一人一套的判定只认 growth_item，不认背包里有没有同型竿。
func TestGrowthItemOwnershipBlocksSecondSet(t *testing.T) {
	_, db := newBundleTestServer(t)
	ctx := context.Background()
	const uid = int64(5001)
	if err := db.GrowthItemInsert(ctx, store.GrowthItem{GUID: "g-rod", UID: uid, SetKey: "growth_sea", Role: growthRoleRod}); err != nil {
		t.Fatal(err)
	}
	if err := db.GrowthItemInsert(ctx, store.GrowthItem{GUID: "g-line", UID: uid, SetKey: "growth_sea", Role: growthRoleLine}); err != nil {
		t.Fatal(err)
	}
	roles, err := db.GrowthItemRoles(ctx, uid, "growth_sea")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := roles[growthRoleRod]; !ok {
		t.Fatalf("竿角色未登记: %+v", roles)
	}
	if _, ok := roles[growthRoleLine]; !ok {
		t.Fatalf("线角色未登记: %+v", roles)
	}
	owned, err := db.GrowthSetKeysOwned(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	if !owned["growth_sea"] {
		t.Fatalf("已持有海钓套却未识别: %+v", owned)
	}
	if owned["growth_float"] {
		t.Fatalf("误判持有浮漂套: %+v", owned)
	}
}

// 服务端判定用的倍率必须跟着套装等级走，而不是看单件自己有没有"变强"标记。
func TestGrowthMultiplierForItemFollowsSetLevel(t *testing.T) {
	srv, db := newBundleTestServer(t)
	ctx := context.Background()
	const uid = int64(6001)
	if err := db.GrowthItemInsert(ctx, store.GrowthItem{GUID: "g-rod", UID: uid, SetKey: "growth_sea", Role: growthRoleRod}); err != nil {
		t.Fatal(err)
	}
	if err := db.GrowthSetStateUpsert(ctx, nil, store.GrowthSetState{UID: uid, SetKey: "growth_sea", Level: 5}); err != nil {
		t.Fatal(err)
	}
	spec, _ := growthSetSpecFor("growth_sea")
	want := growthItemMultiplier(spec.TargetLoadKg, 238.39, 5, spec.LevelCap)
	if got := srv.growthMultiplierForItem(ctx, "g-rod"); math.Abs(got-want) > 1e-9 {
		t.Fatalf("竿倍率应 %.6f，实得 %.6f", want, got)
	}
	if got := srv.growthMultiplierForItem(ctx, "g-not-a-set-item"); math.Abs(got-1.0) > 1e-9 {
		t.Fatalf("非套装件倍率应为 1.0，实得 %.6f", got)
	}
	if err := db.GrowthItemInsert(ctx, store.GrowthItem{GUID: "g-reel", UID: uid, SetKey: "growth_sea", Role: growthRoleReel}); err != nil {
		t.Fatal(err)
	}
	wantReel := growthItemMultiplier(spec.TargetDragKg, 38.55, 5, spec.LevelCap)
	if got := srv.growthMultiplierForItem(ctx, "g-reel"); math.Abs(got-wantReel) > 1e-9 {
		t.Fatalf("轮倍率应按泄力目标算 %.6f，实得 %.6f", wantReel, got)
	}
}

// 成长件永不彻底损坏：磨损累积到下限就停住，钓上大鱼能回耐久，小鱼基本只抵磨损。
func TestGrowthDurabilityFloorAndRecover(t *testing.T) {
	d := 0.12
	for i := 0; i < 60; i++ {
		d = growthDurabilityAfterCatch(d, 0.05, 0, true)
	}
	if math.Abs(d-growthDurabilityFloor) > 1e-9 {
		t.Fatalf("连续磨损应停在下限 %.2f，实得 %.4f", growthDurabilityFloor, d)
	}
	if got := growthDurabilityAfterCatch(0.1, 0.05, 50, true); got <= 0.1 {
		t.Fatalf("50kg 鱼应回复耐久，实得 %.4f", got)
	}
	// 小鱼（保底 1%）只够抵一部分磨损，补不上更大的磨损量（2026-11-16 回复口径改为
	// 每公斤 1%＋每尾保底 1%＋单尾封顶 10%，所以这里用 0.02 的磨损来体现"补不上"）。
	if got := growthDurabilityAfterCatch(0.5, 0.02, 0.2, true); got >= 0.5 {
		t.Fatalf("0.2kg 鱼的保底回复不该把耐久推高，实得 %.4f", got)
	}
	if got := growthDurabilityAfterCatch(1.0, 0.05, 300, true); got > 1 {
		t.Fatalf("耐久上限应封顶 1.0，实得 %.4f", got)
	}
	if got := growthDurabilityAfterCatch(0.05, 0.05, 50, false); got != 0 {
		t.Fatalf("非成长件应照旧磨损归零，实得 %.4f", got)
	}
}

// 一人一套：购买即算持有（礼盒本体就登记归属），不必等开箱；不同类型互不影响。
func TestGrowthSetPurchaseBlockedOnceOwned(t *testing.T) {
	srv, db := newBundleTestServer(t)
	ctx := context.Background()
	const uid = int64(7001)
	if blocked, err := srv.growthPurchaseBlocked(ctx, uid, "growth_sea"); err != nil || blocked {
		t.Fatalf("未购买时不应拦截: blocked=%v err=%v", blocked, err)
	}
	if err := db.GrowthItemInsert(ctx, store.GrowthItem{GUID: "g-bundle", UID: uid, SetKey: "growth_sea", Role: "bundle"}); err != nil {
		t.Fatal(err)
	}
	if blocked, err := srv.growthPurchaseBlocked(ctx, uid, "growth_sea"); err != nil || !blocked {
		t.Fatalf("已持有海钓套应拦截: blocked=%v err=%v", blocked, err)
	}
	if blocked, err := srv.growthPurchaseBlocked(ctx, uid, "growth_float"); err != nil || blocked {
		t.Fatalf("别的套装不该被连带拦截: blocked=%v err=%v", blocked, err)
	}
}

// 成长经验：按鱼重折算，等级按累计经验反解，满级封顶。
func TestGrowthLevelFromXP(t *testing.T) {
	if got := growthXPForCatch(25.06); got != 2506 {
		t.Fatalf("25.06kg 应折 2506 点经验，实得 %d", got)
	}
	if got := growthXPForCatch(0); got != 0 {
		t.Fatalf("零重鱼不该给经验，实得 %d", got)
	}
	for _, tc := range []struct {
		xp   int64
		want int64
	}{
		{0, 0},
		{2999, 0},
		{3000, 1},
		{3000 + 6000, 2},
	} {
		if got := growthLevelFromXP(tc.xp, 15); got != tc.want {
			t.Fatalf("xp=%d 应为 %d 级，实得 %d", tc.xp, tc.want, got)
		}
	}
	if got := growthLevelFromXP(1<<40, 15); got != 15 {
		t.Fatalf("经验溢出时应封顶 15 级，实得 %d", got)
	}
}
