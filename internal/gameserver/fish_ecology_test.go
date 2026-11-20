package gameserver

import (
	"context"
	"math"
	"path/filepath"
	"testing"
	"time"

	"rf4server/internal/logging"
	"rf4server/internal/proto"
)

func TestFishEcologySeedsSpeciesAndMatchesWaterLayers(t *testing.T) {
	srv, db := newRigTestServer(t)
	ctx := context.Background()
	staticPath := filepath.Join("..", "..", "server_data", "static.sqlite3")
	if _, err := db.SQL.Exec(`ATTACH DATABASE ? AS src`, staticPath); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL.Exec(`INSERT OR IGNORE INTO species_weight SELECT * FROM src.species_weight`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL.Exec(`DETACH DATABASE src`); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.ensureFishEcology(ctx); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.SQL.QueryRow(`SELECT COUNT(*) FROM fish_ecology`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 253 {
		t.Fatalf("鱼种生态档案初始化 %d 条，期望 253", count)
	}
	if _, err := db.SQL.Exec(`UPDATE fish_ecology SET profile_version=1, layer_pref=.01, diel_curve='' WHERE fish_key='n.pike'`); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.ensureFishEcology(ctx); err != nil {
		t.Fatal(err)
	}
	var version int
	var layer float64
	if err := db.SQL.QueryRow(`SELECT profile_version,layer_pref FROM fish_ecology WHERE fish_key='n.pike'`).Scan(&version, &layer); err != nil {
		t.Fatal(err)
	}
	if version != fishEcologyProfileVersion || layer <= .01 {
		t.Fatalf("旧版本生态行未自动升级: version=%d layer=%f", version, layer)
	}
	srv.tunCache = map[string]float64{"fish_ecology_enabled": 1}
	srv.tunAt = time.Now()

	// 水层匹配与季节/时段无关，直接测水层因子：合成鱼口权重还乘了季节与水温，
	// 冬季鲶鱼季节权重低，会把「底层更偏鲶鱼」这条断言带偏。
	pike := srv.fishEcology(ctx, "n.pike")
	catfish := srv.fishEcology(ctx, "w_catfish")
	surface := fishRollContext{rigType: 1, depthCM: 50}
	bottom := fishRollContext{rigType: 12, depthCM: 500}
	pikeSurface := srv.fishLayerWeightProfile(pike, surface)
	pikeBottom := srv.fishLayerWeightProfile(pike, bottom)
	catSurface := srv.fishLayerWeightProfile(catfish, surface)
	catBottom := srv.fishLayerWeightProfile(catfish, bottom)
	if pikeSurface <= catSurface {
		t.Fatalf("上层水层应更偏向掠食鱼: pike=%f catfish=%f", pikeSurface, catSurface)
	}
	if catBottom <= pikeBottom {
		t.Fatalf("底层应更偏向鲶鱼: catfish=%f pike=%f", catBottom, pikeBottom)
	}

	// 合成鱼口权重：暖季底层偏向鲶鱼，冬季鲶鱼被季节权重压低（season_mask 不含冬）。
	// 游戏历 24 倍速，这里固定游戏时刻，避免真实日期漂移。
	summer := time.Unix(worldDayEpochUnix+180*24*realSecPerGameHour+12*realSecPerGameHour, 0)
	winter := time.Unix(worldDayEpochUnix+350*24*realSecPerGameHour+12*realSecPerGameHour, 0)
	catSummer := srv.fishBiteEnvironmentWeight(ctx, "w_catfish", fishRollContext{rigType: 12, depthCM: 500, now: summer})
	catWinter := srv.fishBiteEnvironmentWeight(ctx, "w_catfish", fishRollContext{rigType: 12, depthCM: 500, now: winter})
	pikeSummer := srv.fishBiteEnvironmentWeight(ctx, "n.pike", fishRollContext{rigType: 12, depthCM: 500, now: summer})
	if catSummer <= pikeSummer {
		t.Fatalf("暖季底层应偏向鲶鱼: catfish=%f pike=%f", catSummer, pikeSummer)
	}
	if catWinter >= catSummer {
		t.Fatalf("冬季鲶鱼应被季节权重压低: 冬=%f 夏=%f", catWinter, catSummer)
	}
}

func TestEffectiveBaitDepthTracksFloatSinkingAndLureLayer(t *testing.T) {
	now := time.Now()
	floatState := &FishState{
		RigType: 1,
		DepthCM: 300,
		CastTs:  now.Add(-4 * time.Second),
		FallTs:  now.Add(-4 * time.Second),
	}
	if got := floatState.effectiveBaitDepthCMLocked(now); got != 100 {
		t.Fatalf("手杆下沉 4 秒后饵深=%d，期望 100cm", got)
	}
	floatState.FallTs = now.Add(-20 * time.Second)
	if got := floatState.effectiveBaitDepthCMLocked(now); got != 300 {
		t.Fatalf("手杆应停在调漂目标 300cm，实得 %d", got)
	}
	lureState := &FishState{RigType: 5, LureLayer: .8}
	if got := lureState.effectiveBaitDepthCMLocked(now); got != 400 {
		t.Fatalf("路亚水层 0.8 应折算 400cm，实得 %d", got)
	}
	if got := classifyLureMotion(0x800); got != "bottom_motionless" {
		t.Fatalf("0x800 应识别为在水底无移动，实得 %q", got)
	}
	if got := lureLayerFromMotion(0x1000, .5, 0); got != .08 {
		t.Fatalf("水面运动应映射到浅层，实得 %f", got)
	}
	// 水面系动作：犬步/激溅必须落到水面层，否则水面拟饵按中层判鱼，
	// 只能抽到中层鱼种，客户端表现为"鱼来了但不咬"。
	if got := lureLayerFromMotion(0xD1000100, .5, 0); got != .06 {
		t.Fatalf("犬步(walk_dog)应映射水面层 .06，实得 %f", got)
	}
	for _, mask := range []uint32{0x20, 0x420, 0x220, 0x820} {
		if got := lureLayerFromMotion(mask, .5, 0); got != .08 {
			t.Fatalf("激溅组合 mask=%#x 应映射水面层 .08，实得 %f", mask, got)
		}
	}
	// 犬步与激溅同时出现时，犬步仍是更具体的水面动作，保持 .06。
	if got := lureLayerFromMotion(0x520, .5, 0); got != .06 {
		t.Fatalf("犬步+激溅+收线 mask=0x520 应映射 .06，实得 %f", got)
	}
	if got := lureLayerFromMotion(0x400, .5, 0); got != .53 {
		t.Fatalf("普通收线应仍按 +.03 变化，实得 %f", got)
	}
	if got := (&Server{}).rigWaterLayer(fishRollContext{rigType: 5, lureLayer: lureLayerFromMotion(0xD1000100, .5, 0)}); got > .2 {
		t.Fatalf("水面拟饵收线时水层应判水面，实得 %f", got)
	}
}

func TestLureLayerDiveClampAndRigWaterLayer(t *testing.T) {
	// dive>0 时运动推导值不得越过饵的物理泳层 dive+0.15：水面系饵匀速收线
	// 旧版按 +.03 无界漂深，深潜铁板的跳底位也越过物理深度。
	if got := lureLayerFromMotion(0x400, .30, .10); got != .25 {
		t.Fatalf("水面系饵（dive=.10）收线应钳在 dive+.15=.25，实得 %f", got)
	}
	if got := lureLayerFromMotion(0x400, .90, .10); got != .25 {
		t.Fatalf("历史漂深的水面饵收线应钳回 .25，实得 %f", got)
	}
	if got := lureLayerFromMotion(0x2000, .5, .72); got != .87 {
		t.Fatalf("深潜饵（dive=.72）跳底位应钳在 .87，实得 %f", got)
	}
	// 水面动作位不受钳：把深潜饵快收到水面是真实操作。
	if got := lureLayerFromMotion(0x1000, .5, .72); got != .08 {
		t.Fatalf("深潜饵收上水面应映射 .08，实得 %f", got)
	}
	// dive=0（未知）保持旧语义不钳。
	if got := lureLayerFromMotion(0x400, .5, 0); got != .53 {
		t.Fatalf("无 dive 时保持 +.03 语义，实得 %f", got)
	}
	srv := &Server{}
	// 路亚无运动证据：按饵名义泳层兜底，不再一律中层 .5。
	if got := srv.rigWaterLayer(fishRollContext{rigType: 5,
		runtime: &fishRollRuntime{lureKnown: true, lure: lureAttributes{DiveDepth: .10}}}); got > .15 {
		t.Fatalf("水面系饵无运动证据应判表层，实得 %f", got)
	}
	if got := srv.rigWaterLayer(fishRollContext{rigType: 5}); got != .5 {
		t.Fatalf("无饵证据时保持 .5 兜底，实得 %f", got)
	}
	// 浮钓水层保持 5m 满刻度（评审 P2-2：实际水深归一会让区域批量捕获
	// 与逐点生产权重对不上，故统一回退到与 fishWaterConditionsFor 相同的旧口径）。
	if got := srv.rigWaterLayer(fishRollContext{rigType: 1, depthCM: 300}); got != .6 {
		t.Fatalf("浮钓应保持 5m 满刻度 .6，实得 %f", got)
	}
}

func TestClassifyLureMotionUsesClientStatusBits(t *testing.T) {
	cases := []struct {
		mask uint32
		want string
	}{
		{0xD1000010, "twitch"},
		{0xD1000080, "jig"},
		{0xD1000100, "walk_dog"},
		{0xD1002000, "bottom_move"},
		{0xD1004000, "trolling"},
		{0xD1010000, "stop_go"},
		{0xD1020000, "medium_jerk"},
		{0xD1040000, "little_jerk"},
		{0xD1400000, "bottom_layer_move"},
		{0xD1000404, "retrieval_pause"},
		{0xD1000400, "steady"},
		{0xD1000420, "splash"},
		{0xD1000520, "walk_dog"},
		{0xD1008000, ""},
		{0xD1100000, ""},
	}
	for _, mask := range []uint32{0x4, 0x40, 0x80, 0x100, 0x2000, 0x4000, 0x20000, 0x40000} {
		if lureMotionInWaterMask&mask == 0 {
			t.Errorf("0x%X 应算饵在水中运动", mask)
		}
	}
	for _, mask := range []uint32{0x8, 0x200, 0x800, 0x1000} {
		if lureMotionInWaterMask&mask != 0 {
			t.Errorf("0x%X 是静置或触底姿态，不应算饵在水中运动", mask)
		}
	}
	for _, tc := range cases {
		if got := classifyLureMotion(tc.mask); got != tc.want {
			t.Errorf("classifyLureMotion(0x%X)=%q，期望 %q", tc.mask, got, tc.want)
		}
	}
	if got := lureLayerFromMotion(0xD1080000, .1, 0); got != .18 {
		t.Fatalf("下沉动作应增加水层深度到 .18，实得 %f", got)
	}
}

func TestFishLayerWeightProfileSymmetricDecay(t *testing.T) {
	srv := &Server{}
	mk := func(lo, pref, hi float64) fishEcologyProfile {
		return fishEcologyProfile{LayerMin: lo, LayerPref: pref, LayerMax: hi, Enabled: true}
	}
	// rigWaterLayer 口径：float 按 depthCM/500，路亚按 lureLayer，底层杆恒 .93。
	cases := []struct {
		name string
		p    fishEcologyProfile
		c    fishRollContext
		flo  float64
		fhi  float64
	}{
		{"区间内命中 pref 峰≈1", mk(.50, .70, .90), fishRollContext{rigType: 1, depthCM: 350}, .99, 1.001},
		{"区间内偏离 pref 落地板 0.75", mk(.50, .70, .90), fishRollContext{rigType: 1, depthCM: 255}, .74, .76},
		{"水面饵打鲤鱼型（出界对称衰减）", mk(.32, .65, .77), fishRollContext{rigType: 5, lureLayer: .08}, .14, .22},
		{"底钓打表层鱼（对称，保留杂口）", mk(.08, .20, .30), fishRollContext{rigType: 12}, .015, .04},
		{"极端出界取 floor 0.02", mk(.52, .78, .96), fishRollContext{rigType: 5, lureLayer: .06}, .015, .03},
		{"跳底饵打上层滤食鱼", mk(.10, .30, .55), fishRollContext{rigType: 5, lureLayer: .92}, .05, .07},
	}
	for _, tc := range cases {
		got := srv.fishLayerWeightProfile(tc.p, tc.c)
		if got < tc.flo || got > tc.fhi {
			t.Errorf("%s: factor=%f，期望 [%f,%f]", tc.name, got, tc.flo, tc.fhi)
		}
	}
	// 对称性：同量级出界距离的因子一致（旧版对"饵比鱼浅"无硬惩罚，只有高斯尾巴）。
	shallowFish := srv.fishLayerWeightProfile(mk(.08, .20, .30), fishRollContext{rigType: 12})
	deepFish := srv.fishLayerWeightProfile(mk(.52, .78, .96), fishRollContext{rigType: 5, lureLayer: .06})
	if math.Abs(shallowFish-deepFish) > .02 {
		t.Errorf("对称衰减应方向无关: 浅鱼×底钓=%f 深鱼×水面饵=%f", shallowFish, deepFish)
	}
	// runtime/spotEnv 组合（评审 P3）：路亚无运动证据时按 runtime.lure 兜底，
	// 组合下对称性同样成立。
	runtimeDeep := srv.fishLayerWeightProfile(mk(.52, .78, .96),
		fishRollContext{rigType: 5, runtime: &fishRollRuntime{lureKnown: true, lure: lureAttributes{DiveDepth: .06}}})
	if math.Abs(runtimeDeep-deepFish) > .02 {
		t.Errorf("runtime 兜底路径应与 lureLayer 路径同因子: %f vs %f", runtimeDeep, deepFish)
	}
	runtimeSurface := srv.fishLayerWeightProfile(mk(.08, .20, .30),
		fishRollContext{rigType: 5, runtime: &fishRollRuntime{lureKnown: true, lure: lureAttributes{DiveDepth: .93}}})
	if runtimeSurface > .1 {
		t.Errorf("深潜饵底钓打表层鱼应出界强衰减: %f", runtimeSurface)
	}
}

func TestFishEcologyActionWeightFallsBackToLegacyRows(t *testing.T) {
	legacy := map[string]float64{
		"moving":                  1.25,
		"bottom_motionless":       1.4,
		"water_column_motionless": 1.1,
		"surface_motionless":      1.2,
	}
	cases := []struct {
		action string
		want   float64
	}{
		{"steady", 1.25},
		{"bottom_move", 1.4},
		{"bottom_contact", 1.4},
		{"surface", 1.2},
		{"sinking", 1.1},
	}
	for _, tc := range cases {
		got, ok := fishEcologyActionWeight(legacy, tc.action)
		if !ok || got != tc.want {
			t.Errorf("action %s weight=%f,%v，期望 %f,true", tc.action, got, ok, tc.want)
		}
	}
}

func TestLurePulseOverridesSteadyReelState(t *testing.T) {
	now := time.Now()
	st := &FishState{LureMotionMask: 0xD1000400}
	st.recordLurePulseLocked(now)
	if st.LureAction != "twitch" {
		t.Fatalf("单次右键脉冲应覆盖 steady，得到 %q", st.LureAction)
	}
	st.recordLurePulseLocked(now.Add(100 * time.Millisecond))
	if st.LureAction != "jerk" {
		t.Fatalf("第二次右键脉冲应为 jerk，得到 %q", st.LureAction)
	}
	st.recordLurePulseLocked(now.Add(200 * time.Millisecond))
	if st.LureAction != "hard_twitch" {
		t.Fatalf("连续三次右键脉冲应为 hard_twitch，得到 %q", st.LureAction)
	}
}

// hCastPrepare 桥接（评审 P1/P2-4）：真实抛竿路径上验证 LureDive/LureLayer
// 无条件重置——上一杆深潜铁板的残留不得污染下一杆。
func TestCastPrepareResetsLureDiveAndLayer(t *testing.T) {
	s, _ := newRigTestServer(t)
	ctx := context.Background()
	const uid = int64(1000001)
	// 路亚竿：同一竿连续两杆（评审 P1 场景：换饵后旧 LureDive 残留）
	rod, err := s.CreateItem(ctx, uid, "cast_10006_C60MH", "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	sess := newProbeSess(s)
	ctxCast := &Ctx{Sess: sess, Main: 14, Sub: 1, Tree: rigRequestTree(t, proto.Guid(rod.GUID)),
		DecodeOK: true, ExpectResponse: true, Log: logging.Discard()}
	if _, handled := hCastPrepare(ctxCast); !handled {
		t.Fatal("hCastPrepare 未处理")
	}
	// 模拟上一杆深潜铁板的残留（写进该竿的 rod-state）
	st := s.fishingStateForRod(uid, rod.GUID)
	st.mu.Lock()
	st.LureDive, st.LureLayer = .72, .72
	rig := st.RigType
	st.mu.Unlock()
	// 第二杆：无拟饵装配（currentBaits 空，lureDiveForGear=0）
	if _, handled := hCastPrepare(ctxCast); !handled {
		t.Fatal("第二杆 hCastPrepare 未处理")
	}
	st.mu.Lock()
	dive, layer := st.LureDive, st.LureLayer
	st.mu.Unlock()
	if dive != 0 {
		t.Fatalf("第二杆 LureDive 应清零（上一杆 .72 残留）: %f", dive)
	}
	if rigTypeLureFamily[rig] && layer != 0 {
		t.Fatalf("路亚第二杆 LureLayer 应清零走 runtime 兜底: %f", layer)
	}
	// 换非路亚竿：旧竿的 LureDive 残留不得带过去（各竿独立 state，天然为 0；
	// 断言新竿 state 从零开始）
	tele, err := s.CreateItem(ctx, uid, "tele_10012_tl16", "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, handled := hCastPrepare(&Ctx{Sess: sess, Main: 14, Sub: 1, Tree: rigRequestTree(t, proto.Guid(tele.GUID)),
		DecodeOK: true, ExpectResponse: true, Log: logging.Discard()}); !handled {
		t.Fatal("hCastPrepare 未处理")
	}
	st.mu.Lock()
	dive, layer = st.LureDive, st.LureLayer
	st.mu.Unlock()
	if dive != 0 || layer != 0 {
		t.Fatalf("换竿后旧竿残留不得迁移: dive=%f layer=%f", dive, layer)
	}
}

// 换饵 + 14/7 联动（评审 P2-4 后半）：同竿装上拟饵组件后，
// lureDiveForGear 取到新饵、抛竿种子写入，14/7 运动推导受新钳制。
func TestCastPrepareLureSwapAndStepLink(t *testing.T) {
	s, _ := newRigTestServer(t)
	ctx := context.Background()
	sess := newProbeSess(s)
	uid := sess.Userid
	rod, err := s.CreateItem(ctx, uid, "cast_10006_C60MH", "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	lure, err := s.CreateItem(ctx, uid, "hm_popper_65f_001_lq", "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	hook, err := s.CreateItem(ctx, uid, "c_hook_20712", "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	hookRes := s.rigSetComponent(ctx, uid, rod.GUID, catHook, hook.GUID, 250,
		rigSetOpts{ParentType: familyRod, EnforceCategory: true})
	if hookRes.Rejected || hookRes.MountedGUID == "" {
		t.Fatalf("mount hook: %+v", hookRes)
	}
	mounted := s.rigSetComponent(ctx, uid, rod.GUID, catLure, lure.GUID, 1002,
		rigSetOpts{ParentType: familyRod, EnforceCategory: true})
	if mounted.Rejected || mounted.MountedGUID == "" {
		t.Fatalf("mount lure: %+v", mounted)
	}
	st := s.fishingStateForRod(uid, rod.GUID)
	// 不预设 RigType（新竿 state 默认 0）：复审 P2 修复前，第一杆会因
	// 旧 st.RigType=0 不做路亚种子——修复后按竿行 rigType 判定，必须种子。
	if _, handled := hCastPrepare(&Ctx{Sess: sess, Main: 14, Sub: 1, Tree: rigRequestTree(t, proto.Guid(rod.GUID)),
		DecodeOK: true, ExpectResponse: true, Log: logging.New("debug", "console")}); !handled {
		t.Fatal("hCastPrepare 未处理")
	}
	st.mu.Lock()
	dive, layer := st.LureDive, st.LureLayer
	st.mu.Unlock()
	if dive != .10 || layer != .10 {
		t.Fatalf("新竿第一杆（RigType=0）种子应为 dive=layer=0.10: dive=%f layer=%f", dive, layer)
	}
	// 场景 B：float 残留（RigType=1）→ 换路亚钓组第一杆同样要种子
	st.mu.Lock()
	st.RigType = 1
	st.LureDive, st.LureLayer = 0, 0
	st.mu.Unlock()
	if _, handled := hCastPrepare(&Ctx{Sess: sess, Main: 14, Sub: 1, Tree: rigRequestTree(t, proto.Guid(rod.GUID)),
		DecodeOK: true, ExpectResponse: true, Log: logging.New("debug", "console")}); !handled {
		t.Fatal("float 残留后 hCastPrepare 未处理")
	}
	st.mu.Lock()
	dive, layer = st.LureDive, st.LureLayer
	st.mu.Unlock()
	if dive != .10 || layer != .10 {
		t.Fatalf("float 残留第一杆种子应为 dive=layer=0.10: dive=%f layer=%f", dive, layer)
	}
	sendStep := func(anim uint64) {
		t.Helper()
		fields := make([]proto.Field, 7)
		for i := range fields {
			fields[i] = proto.Field{Op: "U32", Num: ptrU64(0)}
		}
		fields[5] = proto.Field{Op: "U32", Num: ptrU64(anim)}
		fields[6] = proto.Field{Op: "STR", Str: strPtr("loc1_02")}
		if _, ok := hFishingStep(&Ctx{Sess: sess, Main: 14, Sub: 7,
			Tree: proto.Args524(proto.Obj("lobemkloehp", 0, fields...)), DecodeOK: true, Log: logging.New("debug", "console")}); !ok {
			t.Fatal("14/7 未处理")
		}
	}
	sendStep(0x400)
	st.mu.Lock()
	layer = st.LureLayer
	st.mu.Unlock()
	if layer != .13 {
		t.Fatalf("steady 收线应 +0.03: %f", layer)
	}
	sendStep(0x100)
	st.mu.Lock()
	layer = st.LureLayer
	st.mu.Unlock()
	if layer != .06 {
		t.Fatalf("犬步应映射水面层 .06: %f", layer)
	}
}

// 评审 P2-1：同一根竿从路亚换回非路亚钓组时，旧 LureAction/LureMotionMask
// 不得残留。旧实现只按 newRigType 判断，非路亚分支不再重置；这里用一根
// tele 竿 + 手工写入旧路亚状态复现该路径。
func TestCastPrepareClearsStaleLureStateOnNonLureRod(t *testing.T) {
	s, _ := newRigTestServer(t)
	ctx := context.Background()
	sess := newProbeSess(s)
	uid := sess.Userid
	rod, err := s.CreateItem(ctx, uid, "tele_10012_tl16", "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	st := s.fishingStateForRod(uid, rod.GUID)
	st.mu.Lock()
	st.RigType = 5 // 模拟这根竿此前是路亚钓组
	st.LureAction = "steady"
	st.LureMotionMask = 0x400
	st.LureLayer = .72
	st.LureDive = .72
	st.mu.Unlock()
	if _, handled := hCastPrepare(&Ctx{Sess: sess, Main: 14, Sub: 1, Tree: rigRequestTree(t, proto.Guid(rod.GUID)),
		DecodeOK: true, ExpectResponse: true, Log: logging.Discard()}); !handled {
		t.Fatal("hCastPrepare 未处理")
	}
	st.mu.Lock()
	action, mask, layer, dive := st.LureAction, st.LureMotionMask, st.LureLayer, st.LureDive
	st.mu.Unlock()
	if action != "" || mask != 0 || layer != 0 || dive != 0 {
		t.Fatalf("非路亚竿抛竿后旧拟饵状态必须清零: action=%q mask=%#x layer=%f dive=%f",
			action, mask, layer, dive)
	}
}

// 评审 P2-2：浮钓水层口径必须与 fishWaterConditionsFor 一致，且不能按
// spotEnv 实际水深归一——区域批量捕获没有点位 spotEnv，实际水深归一会让
// 批量与逐点生产权重对不上（TestFishQueryRegionCaptureMatchesPointCalculation）。
func TestFloatWaterLayerStaysFiveMeterScale(t *testing.T) {
	at := time.Unix(worldDayEpochUnix+180*24*realSecPerGameHour+12*realSecPerGameHour, 0)
	shallow := fishRollContext{rigType: 1, depthCM: 300,
		spotEnv: fishSpotEnvironment{Known: true, DepthM: 10}, now: at}
	legacy := shallow
	legacy.spotEnv = fishSpotEnvironment{}
	if got := (&Server{}).rigWaterLayer(shallow); got != .6 {
		t.Fatalf("浮钓应保持 5m 满刻度 .6，实得 %f", got)
	}
	profile := fishEcologyProfile{Activity: 1, TempMinC: 5, TempPrefC: 17, TempMaxC: 27,
		ClarityPref: .5, LightPref: .5}
	wShallow := fishWaterConditionsFor(profile, shallow)
	wLegacy := fishWaterConditionsFor(profile, legacy)
	if wShallow.WaterTempC != wLegacy.WaterTempC || wShallow.Light != wLegacy.Light {
		t.Fatalf("水温/光照不应因 spotEnv 实际水深变化: shallow=%+v legacy=%+v", wShallow, wLegacy)
	}
}
