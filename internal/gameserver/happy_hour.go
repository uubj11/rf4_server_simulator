package gameserver

import (
	"fmt"
	"math"
	"math/rand"
	"time"

	"rf4server/internal/store"
)

func (sess *Session) updateHappyHour(now time.Time, closing bool) store.HappyHourStatus {
	if sess == nil || sess.Srv == nil || sess.Srv.DB == nil {
		return store.HappyHourStatus{}
	}
	s := sess.Srv
	sess.happyOpMu.Lock()
	defer sess.happyOpMu.Unlock()
	closing = closing || sess.Closed()
	// 原版“欢乐时光”是登录后的一段现实时间窗口（经验与技能提升都翻倍），
	// 与是否抛竿无关。本服是永久会员、没有时长上限，等价口径就是“在线即生效”，
	// 制作与挖饵因此也能在非钓鱼状态下吃到技能加成。
	online := !closing

	owner := fmt.Sprintf("%d:%d", sess.ID, sess.startedAt.UnixNano())
	status, err := s.DB.AdvanceHappyHour(s.dbCtx(), sess.Userid, owner, now, online, !closing,
		max(30*time.Second, 3*s.cfg.KeepaliveInterval()))
	if err != nil {
		s.log.Warn("欢乐时光状态未保存", "err", err)
		return store.HappyHourStatus{}
	}
	sess.happyMu.Lock()
	sess.happyStatus = status
	sess.happyMu.Unlock()
	return status
}

func (sess *Session) currentHappyHour() store.HappyHourStatus {
	if sess == nil {
		return store.HappyHourStatus{}
	}
	sess.happyMu.RLock()
	defer sess.happyMu.RUnlock()
	return sess.happyStatus
}

// happyHourBiteChance 只提高已有吃饵概率，保留低兴趣上限及禁饵。
func happyHourBiteChance(interest, bonus float64) float64 {
	if interest <= 0 {
		return 0
	}
	chance := interest * (1 + clampFloat(bonus, 0, .1))
	if interest <= .1 {
		return math.Min(.1, chance)
	}
	return math.Min(1, chance)
}

func (c fishRollContext) acceptHappyHourBite(interest float64, activities ...float64) bool {
	roll := rand.Float64
	if c.biteRoll != nil {
		roll = c.biteRoll
	}
	chance := happyHourBiteChance(interest, c.happyBite)
	if len(activities) > 0 {
		chance *= clampFloat(activities[0], 0, 1)
	}
	return roll() < chance
}

// happyHourProficiencyRoll 复刻原版“欢乐时光：技能提升翻倍”的口径：判定概率
// 变为 base × 2（上限 0.95），随机数按比例缩放，命中后的单次增量仍是 0.1/0.2。
func happyHourProficiencyRoll(current, roll float64, active bool) float64 {
	if !active {
		return roll
	}
	base := fishingProficiencyGainChance(current)
	chance := math.Min(.95, base*2)
	if chance > 0 {
		return roll * base / chance
	}
	return roll
}
