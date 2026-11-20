package gameserver

import (
	"math"
	"testing"
	"time"

	"rf4server/internal/proto"
)

// TestFightCurvesKeepSprintTensionStages 验证曲线池是官方口径的纯 uniform 缩放：
// 全部行 = 池原值 × (pull/池基准)，张力包络即池自身包络（crucian 池 1.414..3.677，
// 基准 2.828 → 0.50×pull..1.30×pull；官方 crucian 293g 实测 min≈0.49×pull）。
func TestFightCurvesKeepSprintTensionStages(t *testing.T) {
	srv, _ := newRigTestServer(t)
	srv.tunMu.Lock()
	srv.tunCache = map[string]float64{"fight_curve_tension_scale": 1}
	srv.tunText = map[string]string{"fight_curve_tension_scale": "1"}
	srv.tunAt = time.Now()
	srv.tunMu.Unlock()
	const pull = 50.0
	curves := srv.buildFightCurves("crucian", pull)
	if curves == nil || len(curves.Items) < 20 {
		t.Fatalf("搏鱼曲线行数=%d，期望完整冲刺阶段池", len(curves.Items))
	}

	stageIDs := map[uint32]bool{}
	minTension, maxTension := math.Inf(1), 0.0
	for _, row := range curves.Items {
		stageField := row.Field(0)
		stage := stageField.U32()
		stageIDs[stage] = true
		for _, idx := range []int{8, 9} {
			v := protoF32Field(row, idx)
			if v <= 0 {
				t.Fatalf("阶段 %d 的 f%d 张力无效: %f", stage, idx, v)
			}
			minTension = math.Min(minTension, v)
			maxTension = math.Max(maxTension, v)
		}
	}
	for _, stage := range []uint32{104, 105, 301, 302, 306} {
		if !stageIDs[stage] {
			t.Errorf("缺少冲刺/恢复阶段 %d", stage)
		}
	}
	// uniform 缩放：min/max 比例固定为池包络，与 pull 无关。
	if minTension < pull*0.45 || maxTension > pull*1.35 {
		t.Fatalf("曲线张力范围 %.2f..%.2f 偏离 crucian 池 uniform 包络（基准 %.2f）",
			minTension, maxTension, pull)
	}
	if minTension >= pull || maxTension <= pull {
		t.Fatalf("张力包络未跨过基准拉力 %.2f，冲刺差异丢失", pull)
	}
}

// TestFishPullScalesWithSpeciesAndWeight 验证 f8 的官方规律：
// 同鱼种体重大→拉力大（ln 斜率）；掠食者 K0 高于静水小鲤；海水鳕科最高。
func TestFishPullScalesWithSpeciesAndWeight(t *testing.T) {
	srv, _ := newRigTestServer(t)
	pikeSmall := srv.fishFightPullFor(&RolledFish{Species: "n.pike", WeightG: 150})
	pikeLarge := srv.fishFightPullFor(&RolledFish{Species: "n.pike", WeightG: 8000})
	roachPull := srv.fishFightPullFor(&RolledFish{Species: "c.roach", WeightG: 1000})
	crucianPull := srv.fishFightPullFor(&RolledFish{Species: "crucian", WeightG: 1000})

	if pikeLarge <= pikeSmall {
		t.Fatalf("同鱼种大体型拉力应更强（ln 斜率）: small=%f large=%f", pikeSmall, pikeLarge)
	}
	if roachPull <= crucianPull {
		t.Fatalf("拟鲤族 K0 应高于静水小鲤: roach=%f crucian=%f", roachPull, crucianPull)
	}
	// 同重量（1kg，ln 项归零）下比 K0：海水鳕科 > 掠食者 > 拟鲤族 > 静水小鲤。
	pikeK1 := srv.fishFightPullFor(&RolledFish{Species: "n.pike", WeightG: 1000})
	merlangK1 := srv.fishFightPullFor(&RolledFish{Species: "merlang", WeightG: 1000})
	if merlangK1 <= pikeK1 {
		t.Fatalf("海水鳕科 K0 应高于掠食者: merlang(1kg)=%f pike(1kg)=%f", merlangK1, pikeK1)
	}

	// 未实测鱼种走习性组/fallback，不得为 0 或负。
	fallback := srv.fishFightPullFor(&RolledFish{Species: "not_in_calib", WeightG: 500})
	if fallback <= 0 {
		t.Fatalf("未测种拉力=%f，应走组表/fallback", fallback)
	}
}

// TestFightCurvesUniformEnvelope 验证同一池不同拉力下曲线是纯 uniform：
// 每行张力 / pull 恒等于池原比例（官方逐行精确成立的规律）。
func TestFightCurvesUniformEnvelope(t *testing.T) {
	srv, _ := newRigTestServer(t)
	ratio := func(pull float64) []float64 {
		curves := srv.buildFightCurves("crucian", pull)
		var out []float64
		for _, row := range curves.Items {
			out = append(out, protoF32Field(row, 8)/pull)
		}
		return out
	}
	a, b := ratio(10), ratio(80)
	if len(a) != len(b) || len(a) == 0 {
		t.Fatalf("曲线行数不一致: %d vs %d", len(a), len(b))
	}
	for i := range a {
		if math.Abs(a[i]-b[i]) > 1e-4 {
			t.Fatalf("行 %d 非 uniform 缩放: %f vs %f", i, a[i], b[i])
		}
	}
}

func protoF32Field(row *proto.Value, idx int) float64 {
	if row == nil || idx < 0 || idx >= len(row.Fields) || row.Fields[idx].Num == nil {
		return 0
	}
	return float64(proto.F32FromBits(uint32(u64Of(row.Fields[idx].Num))))
}

// setOfficialF6 隔离 A/B 的 fish_f6_scale，直接核对官方表。
func setOfficialF6(srv *Server) {
	srv.tunMu.Lock()
	if srv.tunCache == nil {
		srv.tunCache = map[string]float64{}
	}
	if srv.tunText == nil {
		srv.tunText = map[string]string{}
	}
	srv.tunCache["fish_f6_scale"] = 1
	srv.tunText["fish_f6_scale"] = "1"
	srv.tunAt = time.Now()
	srv.tunMu.Unlock()
}

// TestFishFightGroupFallbackCanBeDisabled 验证组兜底热切换关闭后，
// 未实测鱼种完整回到旧 fallback 静态参数、旧 f5 公式和旧 f6 常数。
func TestFishFightGroupFallbackCanBeDisabled(t *testing.T) {
	srv, _ := newRigTestServer(t)
	srv.tunMu.Lock()
	srv.tunCache = map[string]float64{"fish_fight_group_fallback": 0}
	srv.tunText = map[string]string{"fish_fight_group_fallback": "0"}
	srv.tunAt = time.Now()
	srv.tunMu.Unlock()

	f := &RolledFish{Species: "not_in_calib", Grade: 1, WeightG: 100, LengthM: 0.2}
	tree := srv.buildFish27(f, "", "", 24)
	wantStamina := verifiedStaminaPerGram * 100 * srv.fishFightProfileFor(f).StaminaScale
	if got := f32At(tree, 5); math.Abs(got-wantStamina) > 0.001 {
		t.Fatalf("关闭组兜底后 f5=%f，期望旧公式 %f", got, wantStamina)
	}
	if got := f32At(tree, 6); math.Abs(got-defaultFishInitialStaminaRatio) > 1e-6 {
		t.Fatalf("关闭组兜底后 f6=%f，期望旧常数 %f", got, defaultFishInitialStaminaRatio)
	}
	if got := f32At(tree, 14); math.Abs(got-0.5) > 1e-6 {
		t.Fatalf("关闭组兜底后 f14=%f，期望旧 fallback 0.5", got)
	}
	if got := f32At(tree, 16); math.Abs(got-0.833907) > 1e-6 {
		t.Fatalf("关闭组兜底后 f16=%f，期望旧 fallback 0.833907", got)
	}
}

// TestFightLegacyModelRestoresOldScales 旧版完整模型：f8 回到
// verifiedForcePerGram × ForceScale，f5 回到 2.388/g × StaminaScale，
// f6 回到旧常数。
func TestFightLegacyModelRestoresOldScales(t *testing.T) {
	srv, _ := newRigTestServer(t)
	srv.tunMu.Lock()
	srv.tunCache = map[string]float64{"fight_legacy_model": 1}
	srv.tunText = map[string]string{"fight_legacy_model": "1"}
	srv.tunAt = time.Now()
	srv.tunMu.Unlock()
	setOfficialF6(srv)

	f := &RolledFish{Species: "keta", WeightG: 1000, Grade: 2}
	profile := srv.fishFightProfileFor(f)
	wantPull := verifiedForcePerGram * 1000 * profile.ForceScale
	if got := srv.fishFightPullFor(f); math.Abs(got-wantPull) > 1e-3 {
		t.Fatalf("legacy f8=%v，期望旧公式 %v", got, wantPull)
	}
	wantStamina := verifiedStaminaPerGram * 1000 * profile.StaminaScale
	if got := srv.fishStaminaForProfile(f, profile); math.Abs(got-wantStamina) > 1e-6 {
		t.Fatalf("legacy f5=%v，期望旧公式 %v", got, wantStamina)
	}
	if got := srv.fishF6For(f); got != srv.fishInitialStaminaRatio() {
		t.Fatalf("legacy f6=%v，期望旧常数 %v", got, srv.fishInitialStaminaRatio())
	}
}

// TestFightOfficialStrengthOverridesLegacy 混合模式：保留 legacy 曲线，
// 但 f8/f5/f6 强制回官方口径。
func TestFightOfficialStrengthOverridesLegacy(t *testing.T) {
	srv, _ := newRigTestServer(t)
	srv.tunMu.Lock()
	srv.tunCache = map[string]float64{"fight_legacy_model": 1, "fight_official_strength": 1, "fish_f6_scale": 1}
	srv.tunText = map[string]string{"fight_legacy_model": "1", "fight_official_strength": "1", "fish_f6_scale": "1"}
	srv.tunAt = time.Now()
	srv.tunMu.Unlock()

	f := &RolledFish{Species: "b_carp", WeightG: 1000, Grade: 2}
	profile := srv.fishFightProfileFor(f)
	wantPull := 1.0 * (srv.fishPullK0For("b_carp") + fishPullLnSlope*math.Log(1))
	if got := srv.fishFightPullFor(f); math.Abs(got-wantPull) > 1e-3 {
		t.Fatalf("official strength f8=%v，期望官方公式 %v", got, wantPull)
	}
	if got := srv.fishF6For(f); got != fishF6Table["b_carp"] {
		t.Fatalf("official strength f6=%v，期望 %v", got, fishF6Table["b_carp"])
	}
	wantStamina := fishStaminaRatePerGram["b_carp"] * 1000
	if got := srv.fishStaminaForProfile(f, profile); math.Abs(got-wantStamina) > 1e-6 {
		t.Fatalf("official strength f5=%v，期望 %v", got, wantStamina)
	}
}

// TestFishF6Scale 疲劳系数倍率：b_carp 0.70×1.5 钳到 1.0，
// c.roach 0.50×1.5=0.75。
func TestFishF6Scale(t *testing.T) {
	srv, _ := newRigTestServer(t)
	srv.tunMu.Lock()
	srv.tunCache = map[string]float64{"fish_f6_scale": 1.5}
	srv.tunText = map[string]string{"fish_f6_scale": "1.5"}
	srv.tunAt = time.Now()
	srv.tunMu.Unlock()
	if got := srv.fishF6For(&RolledFish{Species: "b_carp", WeightG: 1000}); got != 1.0 {
		t.Fatalf("b_carp f6=%v，期望钳到 1.0", got)
	}
	if got := srv.fishF6For(&RolledFish{Species: "c.roach", WeightG: 300}); got != 0.75 {
		t.Fatalf("c.roach f6=%v，期望 0.75", got)
	}
}

// TestFightPhaseScales 钉住旧版阶段倍率，防止 legacy 曲线悄悄漂移。
func TestFightPhaseScales(t *testing.T) {
	if got := fightPhaseTensionScale(302); got != 1.78 {
		t.Fatalf("302 张力倍率=%v，期望 1.78", got)
	}
	if got := fightPhaseTensionScale(306); got != 0.42 {
		t.Fatalf("306 张力倍率=%v，期望 0.42", got)
	}
	if got := fightPhaseDurationScale(306); got != 1.55 {
		t.Fatalf("306 时长倍率=%v，期望 1.55", got)
	}
	if got := fightPhaseDurationScale(101); got != 0.72 {
		t.Fatalf("101 时长倍率=%v，期望 0.72", got)
	}
	if !fightIsRestPhase(104) || !fightIsRestPhase(105) || !fightIsRestPhase(306) {
		t.Fatal("104/105/306 应识别为休息阶段")
	}
	if fightIsRestPhase(302) {
		t.Fatal("302 不应识别为休息阶段")
	}
}
