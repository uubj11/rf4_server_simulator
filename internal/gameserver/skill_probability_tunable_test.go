package gameserver

import (
	"context"
	"testing"
	"time"
)

func TestSkillProficiencyFixedPercent(t *testing.T) {
	const cat = int64(2)
	const current = 60.0

	s := &Server{
		tunCache: map[string]float64{"skill_gain_pct_2": 50},
		tunText:  map[string]string{},
		tunAt:    time.Now(),
	}
	// 固定 50%：0.49 应命中，0.51 应失败；不再受原版 60% 熟练度概率影响。
	if got := s.skillProficiencyGainForRoll(cat, current, 0.49, 0); got <= 0 {
		t.Fatalf("50%% 概率下 0.49 应命中，实际 gain=%.2f", got)
	}
	if got := s.skillProficiencyGainForRoll(cat, current, 0.51, 0); got != 0 {
		t.Fatalf("50%% 概率下 0.51 不应命中，实际 gain=%.2f", got)
	}

	// 0 = 原版概率：随机数 0 应命中。
	s.tunCache["skill_gain_pct_2"] = 0
	if got := s.skillProficiencyGainForRoll(cat, current, 0, 0); got <= 0 {
		t.Fatalf("0 应回退原版概率并命中，实际 gain=%.2f", got)
	}

	// 100 = 必定命中，即使随机数接近 1。
	s.tunCache["skill_gain_pct_2"] = 100
	if got := s.skillProficiencyGainForRoll(cat, current, 0.9999, 0); got <= 0 {
		t.Fatalf("100%% 概率应必定命中，实际 gain=%.2f", got)
	}

	// 未配置的大类回退原版公式。
	s.tunCache = map[string]float64{}
	if got := s.skillProficiencyGainForRoll(99, current, 0, 0); got <= 0 {
		t.Fatalf("未配置大类应回退原版并命中，实际 gain=%.2f", got)
	}
}

func setSkillGainCache(s *Server, key string, value float64) {
	s.tunMu.Lock()
	s.tunCache = map[string]float64{key: value}
	s.tunText = map[string]string{}
	s.tunAt = time.Now()
	s.tunMu.Unlock()
}

func TestCraftProficiencyFixedPercent(t *testing.T) {
	srv, db := newProgressSrv(t)
	ctx := context.Background()
	const uid = int64(1000001)
	if _, err := db.EnsureProfile(ctx, uid); err != nil {
		t.Fatal(err)
	}
	setSkillGainCache(srv, "skill_gain_pct_6", 100)
	srv.randFloat = fixedRolls(0.9999, 0.99)
	if got := srv.awardCraftProficiency(ctx, uid, craftCookSkillID, 3, false); got != 0.3 {
		t.Fatalf("100%% 三件制作应增加 0.3，实得 %.2f", got)
	}
	if got, err := db.GetSkillPoints(ctx, uid, craftCookSkillID); err != nil || got != 0.3 {
		t.Fatalf("写入烹饪熟练度=%.2f err=%v, want 0.3", got, err)
	}
}

func TestCatchSettlementFixedPercent(t *testing.T) {
	srv, db := newProgressSrv(t)
	ctx := context.Background()
	const uid = int64(1000001)
	if _, err := db.EnsureProfile(ctx, uid); err != nil {
		t.Fatal(err)
	}
	setSkillGainCache(srv, "skill_gain_pct_2", 100)
	srv.randFloat = fixedRolls(0.9999, 0.99)
	fish := &RolledFish{FishGUID: newFishGUID(), Species: "perch", Grade: 2, WeightG: 642}
	result := srv.settleCatchXP(ctx, uid, fish, 5)
	if result == nil || result.skillID != 2 || result.skillGain <= 0 {
		t.Fatalf("100%% 固定概率应让纺车熟练度增长: %+v", result)
	}
	if got, err := db.GetSkillPoints(ctx, uid, 2); err != nil || got <= 0 {
		t.Fatalf("写入纺车熟练度=%.2f err=%v", got, err)
	}
}

func TestFixedChanceSkipsHappyHourRoll(t *testing.T) {
	srv := &Server{}
	setSkillGainCache(srv, "skill_gain_pct_2", 50)
	// 0.6 在固定 50% 下失败；若错误地叠加欢乐时光修正会变成 0.48 并命中。
	srv.randFloat = fixedRolls(0.6, 0.99)
	if got := srv.proficiencyGainForCatch(2, 60, true); got != 0 {
		t.Fatalf("固定模式不应叠加欢乐时光修正，实得 gain=%.2f", got)
	}
}

func TestBaitDigFixedHit(t *testing.T) {
	srv := &Server{}
	setSkillGainCache(srv, "skill_gain_pct_4", 100)
	srv.randFloat = func() float64 { return 0.9999 }
	if hit, fixed := srv.skillProficiencyFixedHit(captureBaitSkillID); !fixed || !hit {
		t.Fatalf("100%% 挖饵判定应固定命中, hit=%v fixed=%v", hit, fixed)
	}
	setSkillGainCache(srv, "skill_gain_pct_4", 50)
	srv.randFloat = func() float64 { return 0.51 }
	if hit, fixed := srv.skillProficiencyFixedHit(captureBaitSkillID); !fixed || hit {
		t.Fatalf("50%% 挖饵判定 0.51 应失败, hit=%v fixed=%v", hit, fixed)
	}
}
