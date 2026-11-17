package gameserver

import "testing"

// 派鱼重量上限必须在两条分支上一致：hunt（含双轨保底）与普通分支都取
// 「承力口径」与「半小时口径」里更严的一侧。§3.33 的 113 kg 鲇鱼就是因为
// hunt 分支没套半小时上限，才被塞给一根只有 9.10 kg 承力的手杆。
func TestHuntDispatchCapUsesHalfHourLimit(t *testing.T) {
	t.Parallel()
	s, _ := newDetachTestServer(t)

	weak := gearPhysicsSpec{RodPowerKg: 9.10, LineStrengthKg: 10.0}
	capG := s.rollCapGFor("w_catfish", weak, 0)
	if capG <= 0 {
		t.Fatalf("弱钓具应算出半小时上限（>0），实得 %.0f", capG)
	}
	if capG >= 113297 {
		t.Fatalf("弱钓具的派鱼上限必须小于 §3.33 的 113.297 kg，实得 %.0f g", capG)
	}

	// 钓具够强时两条口径都不收紧，保持「鱼来不来不看承力」的原口径。
	strong := gearPhysicsSpec{RodPowerKg: 115, LineStrengthKg: 120, ReelPresent: true, ReelDragKg: 37.5}
	if got := s.rollCapGFor("w_catfish", strong, 0); got <= 113297 {
		t.Fatalf("强钓具不应把这个个体截掉，实得 %.0f g", got)
	}

	// 已有显式承力上限时取更严的一侧（单位 g）。
	if got := s.rollCapGFor("w_catfish", strong, 5000); got != 5000 {
		t.Fatalf("显式上限 5 kg 应生效，实得 %.0f g", got)
	}

	// 开关关掉后完全不截断。
	setTunableForTest(t, s, "fish_time_cap", "0")
	if got := s.rollCapGFor("w_catfish", weak, 0); got != 0 {
		t.Fatalf("fish_time_cap=0 时不应截断，实得 %.0f", got)
	}
}
