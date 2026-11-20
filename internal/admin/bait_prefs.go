package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"rf4server/internal/gameserver"
	"rf4server/internal/store"
)

// baitPrefRow 一条鱼的饵料偏好摘要（管理后台只读展示 + 可编辑覆盖）。
type baitPrefRow struct {
	FishKey  string             `json:"fish_key"`
	NameZh   string             `json:"name_zh"`
	HasRule  bool               `json:"has_rule"`
	Allow    []string           `json:"allow"`       // 规则表"吃"列表
	Deny     []string           `json:"deny"`        // 规则表"不吃"列表
	BottomOh []string           `json:"bottom_only"` // 底钓只吃
	Cond     string             `json:"condition"`
	Tier3    int                `json:"tier3"`     // tier3 饵数量
	Tier2    int                `json:"tier2"`     // tier2 饵数量
	Total    int                `json:"total"`     // 有证据的饵总数
	Baits    []baitPrefEvidence `json:"baits"`     // 证据最强的若干条
	Official []baitPrefShare    `json:"official"`  // 官方纪录里的饵料占比（rf4records_catch）
	Locs     []string           `json:"locs"`      // 出现水域（截断）
	InStrict bool               `json:"in_strict"` // 严格模式下是否还能咬（规则命中或 tier>0）
	// 这鱼吃不吃：把候选饵逐条过一遍运行时判定，给玩家一句人话结论。
	BiteReady    int    `json:"bite_ready"`    // 会咬的饵数
	BiteFallback int    `json:"bite_fallback"` // 只保底的饵数
	BiteBlocked  int    `json:"bite_blocked"`  // 被规则挡掉的饵数
	BiteVerdict  string `json:"bite_verdict"`  // 一句话结论

	// 鱼口画像（fish_ecology，与判鱼 G 层同表同源）：按类食性权重 + 水层 + 档位体重线。
	Diet   map[string]float64 `json:"diet,omitempty"` // worm/fish/boilie/plant/crustacean/lure
	LureOK bool               `json:"lure_ok"`        // lure≥0.30 过 G1 硬门
	Layer  [3]float64         `json:"layer"`          // min/pref/max（0水面~1水底）
	QualG  int64              `json:"qual_g"`         // 合格档体重（钩容量适配计算用）
	TrophG int64              `json:"trophy_g"`

	// 可编辑覆盖与轮换：source=override 表示该鱼已被后台规则覆盖。
	Source     string                   `json:"source"` // default | override
	Enabled    bool                     `json:"enabled"`
	Groups     [][]string               `json:"groups"`
	RotateSec  int64                    `json:"rotate_sec"`
	RotateMode string                   `json:"rotate_mode"`
	Rotation   gameserver.RotationState `json:"rotation"`
}

type baitPrefEvidence struct {
	BaitID   string `json:"bait_id"`
	Tier     int    `json:"tier"`
	Evidence int    `json:"evidence"`
}

// baitPrefShare 官方渔获纪录中该饵占该鱼的比例，判鱼 strict 档位直接用它。
type baitPrefShare struct {
	BaitID  string  `json:"bait_id"`
	Pct     float64 `json:"pct"`
	Count   int     `json:"count"`
	Verdict string  `json:"verdict,omitempty"` // bite / fallback / blocked
	Reason  string  `json:"reason,omitempty"`
}

// baitMeta 是判定「会不会咬」所需的单款饵静态属性。
type baitMeta struct {
	ID       string
	NameZh   string
	Category int64
	LureType int64
	BaitType int64
}

// loadBaitMeta 读 item_def + item_catalog，得到饵料静态属性表。
func loadBaitMeta(ctx context.Context, db *store.DB) map[string]baitMeta {
	out := map[string]baitMeta{}
	rows, err := db.SQL.QueryContext(ctx, `
		SELECT d.item_id, COALESCE(c.name_zh, c.name, ''), d.category,
		       CAST(COALESCE(json_extract(d.attrs_json,'$.mbemakijdeh'),'0') AS INTEGER),
		       CAST(COALESCE(json_extract(d.attrs_json,'$.fopbhlmkcpe'),'0') AS INTEGER)
		FROM item_def d LEFT JOIN item_catalog c ON c.item_id = d.item_id
		WHERE d.category IN (17,27)`)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var m baitMeta
		if rows.Scan(&m.ID, &m.NameZh, &m.Category, &m.LureType, &m.BaitType) == nil {
			out[m.ID] = m
		}
	}
	return out
}

// applyBiteVerdict 把该鱼的热门饵逐条过运行时判定，产出一句人话结论：
// 「能咬 12 种（含 3 种首选）」「只能保底 4 种」「被规则挡掉 5 种」。
func applyBiteVerdict(row *baitPrefRow, meta map[string]baitMeta) {
	type cand struct {
		id   string
		name string
		cat  int64
		lure int64
		bt   int64
		pct  float64
		tier int
	}
	var cands []cand
	seen := map[string]bool{}
	for _, o := range row.Official {
		if seen[o.BaitID] {
			continue
		}
		seen[o.BaitID] = true
		m := meta[o.BaitID]
		cands = append(cands, cand{o.BaitID, m.NameZh, m.Category, m.LureType, m.BaitType, o.Pct, 0})
	}
	for _, b := range row.Baits {
		if seen[b.BaitID] {
			continue
		}
		seen[b.BaitID] = true
		m := meta[b.BaitID]
		cands = append(cands, cand{b.BaitID, m.NameZh, m.Category, m.LureType, m.BaitType, 0, b.Tier})
	}
	if len(cands) == 0 {
		row.BiteVerdict = "无可用饵"
		return
	}
	for _, c := range cands {
		verdict, _, _ := gameserver.ExportedBaitVerdict(row.NameZh, c.id, c.name, c.cat, c.lure, c.bt, c.pct, c.tier)
		switch verdict {
		case "bite":
			row.BiteReady++
		case "fallback":
			row.BiteFallback++
		case "blocked":
			row.BiteBlocked++
		}
	}
	switch {
	case row.BiteReady > 0 && row.BiteFallback > 0:
		row.BiteVerdict = fmt.Sprintf("会咬 %d 种，另有 %d 种只能保底", row.BiteReady, row.BiteFallback)
	case row.BiteReady > 0:
		row.BiteVerdict = fmt.Sprintf("会咬 %d 种", row.BiteReady)
	case row.BiteFallback > 0:
		row.BiteVerdict = fmt.Sprintf("只能保底，%d 种都吃得很勉强", row.BiteFallback)
	default:
		row.BiteVerdict = "热门饵全被规则挡掉"
	}
}

// handleBaitPrefs 鱼饵偏好总览：每条鱼一行，含规则表条目与饵效证据。
//
// 查询参数：
//
//	q     按 fish_key / 中文名模糊匹配
//	loc   只看某个水域出现过的鱼
//	limit 默认 400，最多 2000
func (s *Server) handleBaitPrefs(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	loc := strings.TrimSpace(r.URL.Query().Get("loc"))
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 2000 {
		limit = 400
	}
	like := "%" + q + "%"

	// 1) 鱼清单 + 中文名（优先 wiki 名，退回 loc_fish.name_zh）
	rows, err := s.db.SQL.QueryContext(r.Context(), `
		SELECT x.fish_key, COALESCE(x.name_zh, x.fish_key)
		FROM (
			SELECT w.fish_key,
			       COALESCE(MAX(NULLIF(w.fish_zh,'')), MAX(NULLIF(l.name_zh,'')), w.fish_key) AS name_zh
			FROM loc_wiki_fish w
			LEFT JOIN loc_fish l ON l.fish_key = w.fish_key
			WHERE (? = '' OR w.fish_key LIKE ? OR w.fish_zh LIKE ?)
			  AND (? = '' OR w.loc = ?)
			GROUP BY w.fish_key
		) x
		ORDER BY x.fish_key
		LIMIT ?`, q, like, like, loc, loc, limit)
	if err != nil {
		httpError(w, 500, err.Error())
		return
	}
	type fishEntry struct{ key, name string }
	fish := make([]fishEntry, 0, 256)
	nameByKey := map[string]string{}
	for rows.Next() {
		var k, n string
		if err := rows.Scan(&k, &n); err != nil {
			rows.Close()
			httpError(w, 500, err.Error())
			return
		}
		fish = append(fish, fishEntry{k, n})
		nameByKey[k] = n
	}
	rows.Close()

	// 2) 饵效证据：一次性取全部，再在内存里按鱼聚合（3 万行，够快且只读）。
	evRows, err := s.db.SQL.QueryContext(r.Context(), `
		SELECT fish_key, bait_id, tier, evidence
		FROM companion_bait_fish
		ORDER BY fish_key, tier DESC, evidence DESC`)
	if err != nil {
		httpError(w, 500, err.Error())
		return
	}
	type agg struct {
		tier3, tier2, total int
		baits               []baitPrefEvidence
	}
	byFish := map[string]*agg{}
	for evRows.Next() {
		var fk, bait string
		var tier, ev int
		if err := evRows.Scan(&fk, &bait, &tier, &ev); err != nil {
			evRows.Close()
			httpError(w, 500, err.Error())
			return
		}
		a := byFish[fk]
		if a == nil {
			a = &agg{}
			byFish[fk] = a
		}
		a.total++
		switch tier {
		case 3:
			a.tier3++
		case 2:
			a.tier2++
		}
		if len(a.baits) < 8 {
			a.baits = append(a.baits, baitPrefEvidence{BaitID: bait, Tier: tier, Evidence: ev})
		}
	}
	evRows.Close()

	// 2.5) 官方饵料占比：rf4records_catch 聚合，口径与 gameserver.loadRf4Share 一致
	// （按鱼算分母，单饵样本量低于 bait_share_min_count 的不计入）。
	shareByFish := map[string][]baitPrefShare{}
	minCount := s.baitShareMinCount(r.Context())
	if shRows, serr := s.db.SQL.QueryContext(r.Context(), `
		SELECT fish_key, bait_id, cnt, tot FROM (
			SELECT fish_key, bait_id, count(*) AS cnt,
			       sum(count(*)) OVER (PARTITION BY fish_key) AS tot
			FROM rf4records_catch WHERE fish_key <> '' AND bait_id <> ''
			GROUP BY fish_key, bait_id
		) WHERE cnt >= ?`, minCount); serr == nil {
		type pair struct {
			fk, bait string
			pct      float64
			cnt      int
		}
		all := map[string][]pair{}
		for shRows.Next() {
			var fk, bait string
			var cnt, tot int
			if shRows.Scan(&fk, &bait, &cnt, &tot) == nil && tot > 0 {
				all[fk] = append(all[fk], pair{fk, bait, 100 * float64(cnt) / float64(tot), cnt})
			}
		}
		shRows.Close()
		for fk, list := range all {
			sort.Slice(list, func(i, j int) bool { return list[i].pct > list[j].pct })
			if len(list) > 8 {
				list = list[:8]
			}
			for _, p := range list {
				shareByFish[fk] = append(shareByFish[fk], baitPrefShare{BaitID: p.bait, Pct: math.Round(p.pct*10) / 10, Count: p.cnt})
			}
		}
	}

	// 3) 水域归属（可选，用于筛选后的展示）
	locsByFish := map[string][]string{}
	if locRows, err := s.db.SQL.QueryContext(r.Context(),
		`SELECT fish_key, loc FROM loc_wiki_fish WHERE loc <> '' ORDER BY loc`); err == nil {
		for locRows.Next() {
			var fk, l string
			if locRows.Scan(&fk, &l) == nil {
				if len(locsByFish[fk]) < 6 {
					locsByFish[fk] = append(locsByFish[fk], l)
				}
			}
		}
		locRows.Close()
	}

	// 4) 后台覆盖：有覆盖时以覆盖为准，并把轮换状态一起带出去。
	overrides := map[string]store.FishBaitOverride{}
	if list, oerr := s.db.ListFishBaitOverrides(r.Context()); oerr == nil {
		for _, o := range list {
			overrides[o.FishZh] = o
		}
	}
	// 3.5) 鱼口画像：fish_ecology（判鱼 G 层同源）+ fish_grade_lines（档位体重）
	ecoByFish := map[string][2]any{}
	if ecoRows, eerr := s.db.SQL.QueryContext(r.Context(),
		`SELECT fish_key, diet_json, layer_min, layer_pref, layer_max FROM fish_ecology`); eerr == nil {
		for ecoRows.Next() {
			var fk, dj string
			var lmin, lpref, lmax float64
			if ecoRows.Scan(&fk, &dj, &lmin, &lpref, &lmax) == nil {
				ecoByFish[fk] = [2]any{dj, [3]float64{lmin, lpref, lmax}}
			}
		}
		ecoRows.Close()
	}
	gradeByFish := map[string][2]int64{}
	if gRows, gerr := s.db.SQL.QueryContext(r.Context(),
		`SELECT fish_key, qualified_g, trophy_g FROM fish_grade_lines`); gerr == nil {
		for gRows.Next() {
			var fk string
			var q, t int64
			if gRows.Scan(&fk, &q, &t) == nil {
				gradeByFish[fk] = [2]int64{q, t}
			}
		}
		gRows.Close()
	}

	metas := loadBaitMeta(r.Context(), s.db)

	now := time.Now()
	out := make([]baitPrefRow, 0, len(fish))
	for _, f := range fish {
		row := baitPrefRow{FishKey: f.key, NameZh: f.name, Locs: locsByFish[f.key], Source: "default"}
		if e, ok := ecoByFish[f.key]; ok {
			diet := map[string]float64{}
			_ = json.Unmarshal([]byte(e[0].(string)), &diet)
			row.Diet = diet
			row.Layer = e[1].([3]float64)
			row.LureOK = diet["lure"] >= 0.30
		}
		if g, ok := gradeByFish[f.key]; ok {
			row.QualG, row.TrophG = g[0], g[1]
		}
		if info, ok := gameserver.ExportedBaitRules(f.name); ok {
			row.HasRule = true
			row.Allow = info.Allow
			row.Deny = info.Deny
			row.BottomOh = info.BottomOnly
			row.Cond = info.Condition
		}
		if o, ok := overrides[f.name]; ok {
			row.Source, row.Enabled = "override", o.Enabled
			row.HasRule = true
			row.Allow, row.Deny = o.Allow, o.Deny
			row.BottomOh, row.Cond = o.BottomOnly, o.Condition
			row.Groups, row.RotateSec, row.RotateMode = o.Groups, o.RotateSec, o.RotateMode
			row.Rotation = rotationView(o, now)
		}
		if a := byFish[f.key]; a != nil {
			row.Tier3, row.Tier2, row.Total = a.tier3, a.tier2, a.total
			row.Baits = a.baits
		}
		row.Official = shareByFish[f.key]
		applyBiteVerdict(&row, metas)
		// 严格模式判定：只有“能落到至少一款饵”才算还在咬钩池里。
		// ① 覆盖行被停用（enabled=0）时该鱼不参与；② 规则允许列表为空
		// （整条被清理成空）且没有证据，也不会再出现；③ 有证据（tier>0）
		// 或允许列表非空才算 InStrict。
		if row.Source == "override" && !row.Enabled {
			row.InStrict = false
		} else {
			row.InStrict = len(row.Allow) > 0 || row.Total > 0
		}
		out = append(out, row)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].FishKey < out[j].FishKey })

	writeJSON(w, map[string]any{
		"total":         len(out),
		"rule_count":    gameserver.ExportedBaitRuleCount(),
		"rows":          out,
		"fish_key_ok":   len(nameByKey),
		"rules_enabled": s.baitRulesEnabled(r.Context()),
		"now":           now.Unix(),
	})
}
