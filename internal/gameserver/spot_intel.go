package gameserver

// 本地服新增：抛竿时的“钓点情报”。
//
// 玩家抛出一竿后，在聊天框推一条本钓点的鱼种清单，并分成两类：
//   - 现在这套装备能稳稳起的；
//   - 现在起不来的，以及它们各要多少重量余量 / 短板是哪一件。
//
// 数据来源全部是库里现成的表：
//   loc_alias       地图 id（level_002_kirzah_river） → 中文地点名
//   loc_wiki_fish   地点 → 鱼种全集（fish_key + fish_zh），比 loc_fish 更全
//   loc_fish        捕获统计里的中文名（wiki 名缺失时兜底）
//   species_weight  鱼种 → min_g / max_g / avg_g
//
// 能不能起用服务端同一套物理口径：钩入口上限、竿承力、主线拉力，取最弱一环。

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

var spotIntelThrottle sync.Map

// 同一用户+地点+竿 15 分钟内只推一次，避免反复抛竿刷屏。
const spotIntelInterval = 15 * time.Minute

// 鱼重超过最弱一环但不到这个倍数，算“能咬、能溜，只是吃力”；
// 超过才算现阶段起不来。
const spotIntelSoftFactor = 2.5

type spotFishEntry struct {
	key  string
	name string
	minG int64 // 0 表示库里没有下限数据
	maxG int64 // 0 表示连估算上限都没有，不参与判定
}

// notifySpotIntel 抛新竿时推送钓点情报。
func (s *Server) notifySpotIntel(sess *Session, loc, rodGUID string) {
	if s == nil || sess == nil || loc == "" {
		return
	}
	if s.tunableF("spot_intel", 0) == 0 {
		return
	}
	key := fmt.Sprintf("%d:%s:%s", sess.Userid, loc, rodGUID)
	if last, ok := spotIntelThrottle.Load(key); ok {
		if t, ok2 := last.(time.Time); ok2 && time.Since(t) < spotIntelInterval {
			return
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	text := s.spotIntelText(ctx, sess.Userid, loc, rodGUID)
	if text == "" {
		// 生成失败（通常是地点没解析到、鱼库查不到）不留节流窗口，下次抛竿还能再试。
		return
	}
	spotIntelThrottle.Store(key, time.Now())
	sess.pushSystemChat(text)
	s.log.Info("钓点情报已推送", "userid", sess.Userid, "loc", loc, "rod", rodGUID, "chars", len([]rune(text)))
}

// pushSpotIntelNow 立即重推一次钓点情报，供玩家在聊天框输入 1 主动查询。
// 与 notifySpotIntel 的区别是不受 15 分钟节流限制；同时把节流窗口刷新，
// 避免刚查完又被下一竿立刻重复推一遍。
func (s *Server) pushSpotIntelNow(sess *Session, loc, rodGUID string) {
	if s == nil || sess == nil || loc == "" {
		return
	}
	if s.tunableF("spot_intel", 0) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	text := s.spotIntelText(ctx, sess.Userid, loc, rodGUID)
	if text == "" {
		return
	}
	spotIntelThrottle.Store(fmt.Sprintf("%d:%s:%s", sess.Userid, loc, rodGUID), time.Now())
	sess.pushSystemChat(text)
	s.log.Info("钓点情报已重推", "userid", sess.Userid, "loc", loc, "rod", rodGUID, "chars", len([]rune(text)))
}

// spotIntelText 生成钓点情报文本。
func (s *Server) spotIntelText(ctx context.Context, uid int64, loc, rodGUID string) string {
	if s == nil || s.DB == nil || loc == "" {
		return ""
	}
	locName := s.locZhName(ctx, loc)
	fish := s.locFishList(ctx, locName)
	if len(fish) == 0 {
		return ""
	}

	// 当前这套装备的最弱一环。
	ids := s.rigGearIDs(ctx, uid, rodGUID)
	spec := s.loadGearSpecForRig(ctx, ids, rodGUID)
	weakest := 0.0
	weakName := ""
	consider := func(g float64, name string) {
		if g <= 0 {
			return
		}
		if weakest == 0 || g < weakest {
			weakest, weakName = g, name
		}
	}
	consider(hookGapeG(spec), "钩入口上限")
	consider(spec.RodPowerKg*1000, "竿承力")
	consider(spec.LineStrengthKg*1000, "主线拉力")

	canFish := make([]spotFishEntry, 0, len(fish))
	softFish := make([]spotFishEntry, 0, len(fish))
	cantFish := make([]spotFishEntry, 0, len(fish))
	unknownNames := make([]string, 0, len(fish))
	allMax := 0.0
	for _, f := range fish {
		if f.maxG <= 0 {
			unknownNames = append(unknownNames, f.name)
			continue
		}
		if float64(f.maxG) > allMax {
			allMax = float64(f.maxG)
		}
		switch {
		case weakest <= 0 || float64(f.maxG) <= weakest:
			canFish = append(canFish, f)
		case float64(f.maxG) <= weakest*spotIntelSoftFactor:
			// 超过承力但没超太多：能咬、能溜，只是断线和嘬饵概率高。
			softFish = append(softFish, f)
		default:
			cantFish = append(cantFish, f)
		}
	}

	// 默认简洁模式：按聊天框宽度分行；想连缺资料鱼种一起看，把 spot_intel_detail 设 1。
	if s.tunableF("spot_intel_detail", 0) <= 0 {
		text := spotIntelCompact(locName, len(fish), weakest, weakName, canFish, softFish, cantFish, allMax, len(unknownNames))
		if lines := s.spotRecommendLines(ctx, locName); len(lines) > 1 {
			text += "\n" + strings.Join(lines, "\n")
		}
		return text
	}

	var b strings.Builder
	fmt.Fprintf(&b, "【钓点·%s】共 %d 种鱼", locName, len(fish))
	if weakest > 0 {
		fmt.Fprintf(&b, "｜最弱一环 %s %.0f g（鱼照来，装备只决定拉不拉得上）", weakName, weakest)
	}

	if len(canFish) > 0 {
		names := make([]string, 0, len(canFish))
		for _, f := range canFish {
			names = append(names, f.name)
		}
		for _, line := range wrapPrefix("能稳起：", names) {
			b.WriteString("\n")
			b.WriteString(line)
		}
	}

	if len(softFish) > 0 {
		sort.Slice(softFish, func(i, j int) bool { return softFish[i].maxG < softFish[j].maxG })
		names := make([]string, 0, len(softFish))
		for _, f := range softFish {
			names = append(names, fmt.Sprintf("%s %.1fkg", f.name, float64(f.maxG)/1000))
		}
		for _, line := range wrapPrefix("能搏但吃力：", names) {
			b.WriteString("\n")
			b.WriteString(line)
		}
	}

	if len(cantFish) > 0 {
		// 按需要的最大重量升序，先报“差一点就能起”的。
		sort.Slice(cantFish, func(i, j int) bool { return cantFish[i].maxG < cantFish[j].maxG })
		items := make([]string, 0, len(cantFish))
		for _, f := range cantFish {
			if f.minG > 0 {
				items = append(items, fmt.Sprintf("%s %.1f-%.1fkg", f.name, float64(f.minG)/1000, float64(f.maxG)/1000))
			} else {
				items = append(items, fmt.Sprintf("%s ≤%.1fkg", f.name, float64(f.maxG)/1000))
			}
		}
		for _, line := range wrapPrefix("超装备上限（易嘬饵/易断线）：", items) {
			b.WriteString("\n")
			b.WriteString(line)
		}
	}

	if len(cantFish) > 0 && allMax > 0 && weakName != "" {
		fmt.Fprintf(&b, "\n想吃下本点全部鱼：最弱一环（%s）要提到 %.1f kg 以上", weakName, allMax/1000)
	}

	if len(unknownNames) > 0 {
		prefix := fmt.Sprintf("另有 %d 种库中无重量数据（不参与判定）：", len(unknownNames))
		for _, line := range wrapPrefix(prefix, unknownNames) {
			b.WriteString("\n")
			b.WriteString(line)
		}
	}

	return b.String()
}

// 聊天框可见宽度约 20 个中文字，折行按半角宽 36 计，保证每行都不被客户端二次折行。
const chatLineWidth = 36

// textWidth 估算显示宽度：ASCII 算 1，中文/全角算 2。
func textWidth(s string) int {
	w := 0
	for _, r := range s {
		if r < 128 {
			w++
		} else {
			w += 2
		}
	}
	return w
}

// wrapPrefix 先写前缀（如“稳起 5：”），鱼名用空格分隔，超出宽度就在空格处换行，续行缩进两格。
func wrapPrefix(prefix string, names []string) []string {
	lines := make([]string, 0, 4)
	cur := prefix
	for _, n := range names {
		add := n
		if cur != prefix {
			add = " " + n
		}
		if cur != prefix && textWidth(cur)+textWidth(add) > chatLineWidth {
			lines = append(lines, cur)
			cur = "  " + n
			continue
		}
		cur += add
	}
	return append(lines, cur)
}

// spotIntelCompact 简洁版：三类鱼全部点名，按聊天框宽度分行。
func spotIntelCompact(locName string, total int, weakest float64, weakName string,
	canFish, softFish, cantFish []spotFishEntry, allMax float64, unknown int) string {

	shortGear := func(n string) string {
		switch {
		case strings.Contains(n, "钩"):
			return "钩"
		case strings.Contains(n, "竿"):
			return "竿"
		case strings.Contains(n, "线"):
			return "线"
		}
		return n
	}
	namesOf := func(g []spotFishEntry) []string {
		sort.Slice(g, func(i, j int) bool { return g[i].maxG < g[j].maxG })
		names := make([]string, 0, len(g))
		for _, f := range g {
			names = append(names, f.name)
		}
		return names
	}

	lines := []string{fmt.Sprintf("【钓点】%s %d 种", locName, total)}
	if weakest > 0 {
		lines = append(lines, fmt.Sprintf("上限 %.0fg（%s）", weakest, shortGear(weakName)))
	}
	if names := namesOf(canFish); len(names) > 0 {
		lines = append(lines, wrapPrefix(fmt.Sprintf("稳起 %d：", len(names)), names)...)
	}
	if names := namesOf(softFish); len(names) > 0 {
		lines = append(lines, wrapPrefix(fmt.Sprintf("吃力 %d：", len(names)), names)...)
	}
	if names := namesOf(cantFish); len(names) > 0 {
		lines = append(lines, wrapPrefix(fmt.Sprintf("超限 %d：", len(names)), names)...)
		if allMax > 0 && weakName != "" {
			lines = append(lines, fmt.Sprintf("全收需 %.1fkg", allMax/1000))
		}
	}
	return strings.Join(lines, "\n")
}

// locZhName 地点 → 中文地点名。
// 注意 st.Loc 存的是客户端上报的场景短名（lvl2_01 / loc1_01），而 loc_wiki_fish
// 和 loc_alias.loc 用的是中文名，必须先经 ResolveStatLoc 把场景名解析成中文名，
// 否则整条情报会因为查不到鱼种而变成空文本、什么都不推。
func (s *Server) locZhName(ctx context.Context, loc string) string {
	if s == nil || s.DB == nil || loc == "" {
		return loc
	}
	if name := s.DB.ResolveStatLoc(ctx, loc); name != "" {
		return name
	}
	return loc
}

// locFishList 取该地点鱼种全集（loc_wiki_fish 为准，名字缺时用 loc_fish 兜底），
// 并带出重量区间；没有 max_g 的用 avg_g×2 估算，两者都缺的记 0（不参与判定）。
func (s *Server) locFishList(ctx context.Context, locName string) []spotFishEntry {
	if s == nil || s.DB == nil {
		return nil
	}
	rows, err := s.DB.SQL.QueryContext(ctx, `
		SELECT w.fish_key,
		       COALESCE(NULLIF(w.fish_zh,''), NULLIF(l.name_zh,''), w.fish_key),
		       COALESCE(s.min_g,0),
		       CASE WHEN s.max_g > 0 THEN s.max_g
		            WHEN s.avg_g > 0 THEN s.avg_g*2
		            ELSE 0 END
		FROM loc_wiki_fish w
		LEFT JOIN loc_fish l ON l.loc = w.loc AND l.fish_key = w.fish_key
		LEFT JOIN species_weight s ON s.fish_key = w.fish_key
		WHERE w.loc = ?`, locName)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var out []spotFishEntry
	seen := map[string]bool{}
	for rows.Next() {
		var e spotFishEntry
		if err := rows.Scan(&e.key, &e.name, &e.minG, &e.maxG); err != nil {
			continue
		}
		if e.key == "" || seen[e.key] {
			continue
		}
		seen[e.key] = true
		if e.name == "" {
			e.name = e.key
		}
		out = append(out, e)
	}
	return out
}

// spotRecommendLines 生成“本图每种鱼 → 纪录最高钓点”的行。
// 只读 spot_fish（真实渔获统计）：该表只有“网格坐标点位 + 鱼种 + 次数”，
// 没有朝向字段，所以这里不给抛竿方向，只指到点位。
func (s *Server) spotRecommendLines(ctx context.Context, locName string) []string {
	if s == nil || s.DB == nil || locName == "" {
		return nil
	}
	if s.tunableF("spot_recommend", 1) == 0 {
		return nil
	}
	rows, err := s.DB.SQL.QueryContext(ctx, `
		SELECT sf.fish_key,
		       COALESCE(
		         NULLIF((SELECT w.fish_zh FROM loc_wiki_fish w
		                 WHERE w.loc = sf.loc AND w.fish_key = sf.fish_key AND w.fish_zh <> '' LIMIT 1), ''),
		         NULLIF((SELECT l.name_zh FROM loc_fish l
		                 WHERE l.loc = sf.loc AND l.fish_key = sf.fish_key AND l.name_zh <> '' LIMIT 1), ''),
		         sf.fish_key),
		       COALESCE((SELECT MAX(sw.max_g) FROM species_weight sw WHERE sw.fish_key = sf.fish_key), 0),
		       COALESCE((SELECT sp.spot FROM spot_fish sp
		                 WHERE sp.loc = sf.loc AND sp.fish_key = sf.fish_key
		                 GROUP BY sp.spot ORDER BY SUM(sp.catches) DESC, sp.spot LIMIT 1), '')
		FROM spot_fish sf
		WHERE sf.loc = ?
		GROUP BY sf.fish_key`, locName)
	if err != nil {
		return nil
	}
	defer rows.Close()

	type spotRec struct {
		name string
		maxG int64
		spot string
	}
	list := make([]spotRec, 0, 64)
	for rows.Next() {
		var key string
		var r spotRec
		if err := rows.Scan(&key, &r.name, &r.maxG, &r.spot); err != nil {
			continue
		}
		if r.spot == "" {
			continue
		}
		if r.name == "" {
			r.name = key
		}
		list = append(list, r)
	}
	if len(list) == 0 {
		return nil
	}

	// 体型上限大的排前面；没有体重数据的按名字排在后面。
	sort.Slice(list, func(i, j int) bool {
		if list[i].maxG != list[j].maxG {
			return list[i].maxG > list[j].maxG
		}
		return list[i].name < list[j].name
	})

	limit := 0
	if v := int(s.tunableF("spot_recommend_max", 0)); v > 0 {
		limit = v
	}
	items := make([]string, 0, len(list))
	for _, r := range list {
		if limit > 0 && len(items) >= limit {
			break
		}
		items = append(items, fmt.Sprintf("%s %s", r.name, r.spot))
	}

	lines := []string{fmt.Sprintf("【本图点位】%s %d 种有记录", locName, len(list))}
	lines = append(lines, wrapItems("点位：", items)...)
	if limit > 0 && len(list) > limit {
		lines = append(lines, fmt.Sprintf("另有 %d 种未列出", len(list)-limit))
	}
	return lines
}

// wrapItems 用“｜”分隔条目并按聊天框宽度折行；条目内部允许含空格（如“鱼名 坐标”）。
func wrapItems(prefix string, items []string) []string {
	lines := make([]string, 0, 8)
	cur := prefix
	for _, it := range items {
		if cur == prefix {
			cur += it
			continue
		}
		if textWidth(cur)+textWidth(it)+3 > chatLineWidth {
			lines = append(lines, cur)
			cur = "  " + it
			continue
		}
		cur += "｜" + it
	}
	return append(lines, cur)
}
