package gameserver

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
)

// ---- 每图钓鱼熟练度（本地 mod，0.9.58）----
//
// 原版没有「按地图累计」的概念：同一套技能熟练度跟人走，换图上鱼不看你在那张图钓过多少。
// 这里加一层纯玩家进度：每张地图单独记「钓到过多少尾」，尾数跨档就升级；等级越高，
// 该图的稀有/超稀有鱼种权重越高、偏重个体越容易出现。等级由尾数派生，不单独存账。
//
// 三根轴都有 tunable，任一置 0 即退回原版：
//   map_prof_enabled=0            整条链路关闭（不读等级、不写表、不播报）
//   map_prof_steps="0,10,..."     档位表（严格递增，首项必须 0；非法即视为 Lv0）
//   map_prof_rare_step=0.08       每级对「稀有度 n」的候选乘 1+step×level×n
//   map_prof_heavy_step=0.03      每级给 n% 的重抽概率（取两次较大值）
//   grade_pct_below_min/step      未达标（不足最小尺寸）鱼占比随等级下调的底与步长
const (
	tunableMapProfEnabled   = "map_prof_enabled"
	tunableMapProfSteps     = "map_prof_steps"
	tunableMapProfRareStep  = "map_prof_rare_step"
	tunableMapProfHeavyStep = "map_prof_heavy_step"
	// 未达标（不足最小尺寸）鱼占比随本图熟练度下调：base 是 grade_pct_below，
	// 每级减 step，触底 min。三条都走 tunable，缺省即下面的默认值。
	tunableGradeBelowMin  = "grade_pct_below_min"
	tunableGradeBelowStep = "grade_pct_below_step"

	defMapProfSteps     = "0,10,30,70,150,300,600,1200,2400,4800"
	defMapProfRareStep  = 0.08
	defMapProfHeavyStep = 0.03
	defGradeBelowMin    = 0.5
	defGradeBelowStep   = 0.25
)

// gradeBelowPctFor 返回某张图在某等级下的「未达标鱼占比」，从 base 起每级减 step，
// 触底 min；等级 0 或 step<=0 时原样返回 base。
func gradeBelowPctFor(base, min, step float64, level int) float64 {
	if level <= 0 || step <= 0 {
		return base
	}
	v := base - step*float64(level)
	if v < min {
		v = min
	}
	if v < 0 {
		v = 0
	}
	return v
}

// mapProfHeavyHit 判断这一竿要不要重抽体重：掷点小于概率才命中。
func mapProfHeavyHit(chance float64, roll func() float64) bool {
	if chance <= 0 || roll == nil {
		return false
	}
	return roll() < chance
}

const mapProfHeavyMaxChance = 0.9

// mapProfRoll 是一次发鱼要用的熟练度快照（Lv0 时全零，等于原版行为）。
type mapProfRoll struct {
	level       int
	rareStep    float64
	heavyChance float64
}

// mapProfProgress 是一次计数前后的等级与累计尾数。
type mapProfProgress struct {
	Before  int
	After   int
	Catches int64
}

// mapProfStepList 解析档位表；严格递增、首项为 0 才认，否则返回 nil（按 Lv0 处理）。
func mapProfStepList(raw string) []int64 {
	parts := strings.Split(raw, ",")
	out := make([]int64, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		v, err := strconv.ParseInt(p, 10, 64)
		if err != nil || v < 0 {
			return nil
		}
		if len(out) > 0 && v <= out[len(out)-1] {
			return nil
		}
		out = append(out, v)
	}
	if len(out) < 2 || out[0] != 0 {
		return nil
	}
	return out
}

// mapProfLevelFor 返回累计尾数对应的等级（档位下标，封顶最后一档）。
func mapProfLevelFor(catches int64, steps []int64) int {
	if len(steps) < 2 {
		return 0
	}
	level := 0
	for i, need := range steps {
		if catches >= need {
			level = i
			continue
		}
		break
	}
	return level
}

// mapProfSpeciesWeight 返回某稀有度在指定等级下的权重倍率。
// rarity 0（普通）不抬；倍率封顶 5 倍，避免高等级把普通鱼挤没。
func mapProfSpeciesWeight(rarity, level int64, step float64) float64 {
	if rarity <= 0 || level <= 0 || step <= 0 {
		return 1
	}
	mult := 1 + step*float64(level)*float64(rarity)
	if mult <= 1 {
		return 1
	}
	if mult > 5 {
		return 5
	}
	return mult
}

// mapProfHeavyChanceFor 返回单次咬钩「重抽一次体重并取较大值」的概率。
func mapProfHeavyChanceFor(level int64, step float64) float64 {
	if level <= 0 || step <= 0 {
		return 0
	}
	chance := step * float64(level)
	if chance > mapProfHeavyMaxChance {
		return mapProfHeavyMaxChance
	}
	return chance
}

// mapProfHeavyDraw 先抽一次；命中概率时再抽一次取较大值。
// roll 单独作为参数，便于测试确定性地命中/不命中。
func mapProfHeavyDraw(chance float64, sample func() float64, roll func() float64) float64 {
	v := sample()
	if chance <= 0 {
		return v
	}
	if roll() < chance {
		if v2 := sample(); v2 > v {
			v = v2
		}
	}
	return v
}

// applyMapProfSpeciesBias 把稀有度倍率乘到候选权重上；普通鱼与顺序都不动。
func applyMapProfSpeciesBias(keys []string, wts []float64, rarity map[string]int64, level int64, step float64) {
	if level <= 0 || step <= 0 {
		return
	}
	for i, key := range keys {
		if i >= len(wts) {
			return
		}
		wts[i] *= mapProfSpeciesWeight(rarity[key], level, step)
	}
}

// mapProfForRoll 读玩家在该图的等级并换算成发鱼用的三根轴。
// 读不到（新玩家 / 表缺行 / 读失败）一律按 Lv0，不允许因为进度读失败把鱼判没了。
func (s *Server) mapProfForRoll(ctx context.Context, uid int64, loc string) mapProfRoll {
	if s == nil || s.DB == nil || uid <= 0 || loc == "" || !s.mapProfEnabled() {
		return mapProfRoll{}
	}
	var catches int64
	err := s.DB.SQL.QueryRowContext(ctx,
		`SELECT catches FROM map_proficiency WHERE uid=? AND loc=?`, uid, loc).Scan(&catches)
	if err != nil {
		return mapProfRoll{}
	}
	level := mapProfLevelFor(catches, s.mapProfSteps())
	if level <= 0 {
		return mapProfRoll{}
	}
	return mapProfRoll{
		level:       level,
		rareStep:    clampFloat(s.tunableF(tunableMapProfRareStep, defMapProfRareStep), 0, 1),
		heavyChance: mapProfHeavyChanceFor(int64(level), clampFloat(s.tunableF(tunableMapProfHeavyStep, defMapProfHeavyStep), 0, 1)),
	}
}

// addMapProfCatchTx 在结算事务里给 (uid, loc) 加一尾，并返回跨级前后的等级。
// 与经验、熟练度、图鉴同事务：任一步失败整笔回滚，不会出现「鱼到手了但等级没涨」。
func (s *Server) addMapProfCatchTx(ctx context.Context, tx *sql.Tx, uid int64, loc string, now float64) (mapProfProgress, error) {
	if s == nil || tx == nil || uid <= 0 || loc == "" || !s.mapProfEnabled() {
		return mapProfProgress{}, nil
	}
	var catches int64
	err := tx.QueryRowContext(ctx,
		`SELECT catches FROM map_proficiency WHERE uid=? AND loc=?`, uid, loc).Scan(&catches)
	if err != nil && err != sql.ErrNoRows {
		return mapProfProgress{}, err
	}
	before := mapProfLevelFor(catches, s.mapProfSteps())
	catches++
	if _, err := tx.ExecContext(ctx, `INSERT INTO map_proficiency(uid,loc,catches,last_ts) VALUES(?,?,?,?)
		ON CONFLICT(uid,loc) DO UPDATE SET catches=excluded.catches, last_ts=excluded.last_ts`,
		uid, loc, catches, now); err != nil {
		return mapProfProgress{}, err
	}
	return mapProfProgress{
		Before:  before,
		After:   mapProfLevelFor(catches, s.mapProfSteps()),
		Catches: catches,
	}, nil
}

// mapProfLocName 取中文地图名（loc_alias 的 alias→loc）；查不到返回空串。
func (s *Server) mapProfLocName(ctx context.Context, loc string) string {
	if s == nil || s.DB == nil || loc == "" {
		return ""
	}
	var name string
	if err := s.DB.SQL.QueryRowContext(ctx,
		`SELECT loc FROM loc_alias WHERE alias=? LIMIT 1`, loc).Scan(&name); err != nil {
		return ""
	}
	return strings.TrimSpace(name)
}

// notifyMapProfCatch 给在线会话推一条熟练度播报：默认每抓一条都弹，
// map_prof_chat_every_catch=0 时退回「只在升级弹」。
func (s *Server) notifyMapProfCatch(ctx context.Context, uid int64, loc string, prog mapProfProgress) {
	if s == nil || uid <= 0 || prog.Catches <= 0 {
		return
	}
	if prog.After <= prog.Before && !s.mapProfChatEveryCatch() {
		return
	}
	text := mapProfCatchText(s.mapProfLocName(ctx, loc), prog.Before, prog.After, prog.Catches, s.mapProfSteps())
	for _, sess := range s.sessionsForUID(uid) {
		sess.pushSystemChat(text)
	}
}

// mapProfChatEveryCatch 是否每尾都播报（默认开）。
func (s *Server) mapProfChatEveryCatch() bool {
	if s == nil {
		return false
	}
	return s.tunableF("map_prof_chat_every_catch", 1) != 0
}

// mapProfCatchText 每尾播报文案：升级用「升至」，未升级显示累计与差额，满级不算差额。
func mapProfCatchText(name string, before, after int, catches int64, steps []int64) string {
	if strings.TrimSpace(name) == "" {
		name = "未知水域"
	}
	next := mapProfNextNeed(after, steps)
	if after > before {
		if next < 0 {
			return fmt.Sprintf("「%s」钓鱼熟练度升至 Lv.%d（累计 %d 尾，已满级）", name, after, catches)
		}
		return fmt.Sprintf("「%s」钓鱼熟练度升至 Lv.%d（累计 %d 尾，下一级 %d 尾）",
			name, after, catches, next)
	}
	if next < 0 {
		return fmt.Sprintf("「%s」钓鱼熟练度 Lv.%d（累计 %d 尾，已满级）", name, after, catches)
	}
	left := next - catches
	if left < 0 {
		left = 0
	}
	return fmt.Sprintf("「%s」钓鱼熟练度 Lv.%d · 累计 %d/%d 尾（还差 %d 尾升 Lv.%d）",
		name, after, catches, next, left, after+1)
}

// mapProfNextNeed 返回升到下一档需要的累计尾数（满级返回 -1）。
func mapProfNextNeed(level int, steps []int64) int64 {
	if len(steps) == 0 || level+1 >= len(steps) {
		return -1
	}
	if level < 0 {
		level = 0
	}
	if level+1 >= len(steps) {
		return -1
	}
	return steps[level+1]
}

// mapProfEnabled 整条链路的开关（默认开）。
func (s *Server) mapProfEnabled() bool {
	if s == nil {
		return false
	}
	return s.tunableF(tunableMapProfEnabled, 1) >= 1
}

// mapProfSteps 档位表：非法或缺失时退回默认曲线。
func (s *Server) mapProfSteps() []int64 {
	if s == nil {
		return mapProfStepList(defMapProfSteps)
	}
	if steps := mapProfStepList(s.tunableS(tunableMapProfSteps, defMapProfSteps)); steps != nil {
		return steps
	}
	return mapProfStepList(defMapProfSteps)
}
