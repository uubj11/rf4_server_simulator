package gameserver

// 本地服新增：跑鱼/嘬饵时的装备建议。
//
// 判定口径直接复用服务端已有的物理模型，不另造规则：
//   - 钩：hookGapeG(spec) = hookWeightCapG(钩长, 钩号) * gearOverloadFactor，
//     鱼重超过这个入口上限就会高频"嘬饵/鱼未上钩离场"（见 fish_hunt.go 的
//     hookSolidHold：solid = .96 - .45*(r-.8)，r = 鱼重/入口上限）。
//   - 竿：spec.RodPowerKg（item_def 的最大承力，单位 kg）。
//   - 线：spec.LineStrengthKg（kg）。
//
// 建议优先从玩家背包里挑能扛住这条鱼的实物，挑不到才提示去商店买。

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
)

// gearAdviceThrottle 同一根竿 20 秒内最多一条建议，避免连嘬时刷屏。
var gearAdviceThrottle sync.Map

const gearAdviceInterval = 20 * time.Second

// notifyGearAdvice 在鱼没咬住/跑掉之后推送一条换装建议。
func (s *Server) notifyGearAdvice(sess *Session, rodGUID string, f *RolledFish) {
	if s == nil || sess == nil || f == nil || f.WeightG <= 0 {
		return
	}
	if s.tunableF("gear_advice", 1) == 0 {
		return
	}
	key := fmt.Sprintf("%d:%s", sess.Userid, rodGUID)
	if last, ok := gearAdviceThrottle.Load(key); ok {
		if t, ok2 := last.(time.Time); ok2 && time.Since(t) < gearAdviceInterval {
			return
		}
	}
	gearAdviceThrottle.Store(key, time.Now())

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	text := s.gearAdviceText(ctx, sess.Userid, rodGUID, f)
	if text == "" {
		// 装备都够用（或背包没有可换的）时不推，避免无意义刷屏；留日志便于排查“为什么没反馈”。
		s.log.Info("装备建议：本次无需建议", "userid", sess.Userid, "rod", shortGUID(rodGUID), "weight_g", f.WeightG)
		return
	}
	sess.pushSystemChat(text)
	s.log.Info("装备建议已推送", "userid", sess.Userid, "rod", shortGUID(rodGUID), "weight_g", f.WeightG, "chars", len([]rune(text)))
}

// gearAdviceText 生成"钩太小 / 竿太轻 / 线太细"的诊断文本；无需建议时返回空串。
func (s *Server) gearAdviceText(ctx context.Context, uid int64, rodGUID string, f *RolledFish) string {
	if s == nil || s.DB == nil || f == nil || f.WeightG <= 0 {
		return ""
	}
	ids := s.rigGearIDs(ctx, uid, rodGUID)
	spec := s.loadGearSpecForRig(ctx, ids, rodGUID)
	w := float64(f.WeightG)
	var tips []string

	if spec.HookPresent {
		gape := hookGapeG(spec)
		switch {
		case gape > 0 && w > gape*0.9:
			if name, capG := s.bestBackpackHook(ctx, uid, f.WeightG); name != "" {
				tips = append(tips, fmt.Sprintf("钩小 %.0fg＜鱼%.0fg\n→ 换「%s」%.0fg\n　钓%.0f~%.0fg",
					gape, w, name, capG, capG*0.25, capG*0.9))
			} else if nm, cp, silver := s.pickShopHook(ctx, f.WeightG); nm != "" {
				tips = append(tips, fmt.Sprintf("钩小 %.0fg＜鱼%.0fg\n→ 买「%s」\n　%.0fg %d银 钓%.0f~%.0fg",
					gape, w, nm, cp, silver, cp*0.25, cp*0.9))
			} else {
				tips = append(tips, fmt.Sprintf("钩小 %.0fg＜鱼%.0fg\n→ 背包无更大钩", gape, w))
			}
		case gape > 0 && w < gape*0.25:
			// 鱼远小于钩口：小鱼嘴小，咬不住大号钩，表现就是“啄两下就走、漂不动”。
			// 背包没有更小的钩时也给一支商店在售的，玩家照着型号去买就行。
			if name, capG := s.bestBackpackHook(ctx, uid, f.WeightG); name != "" && capG < gape*0.75 {
				tips = append(tips, fmt.Sprintf("钩偏大 鱼%.0fg＜钩%.0fg\n→ 换「%s」%.0fg\n　钓%.0f~%.0fg",
					w, gape, name, capG, capG*0.25, capG*0.9))
			} else if nm, cp, silver := s.pickShopHook(ctx, f.WeightG); nm != "" {
				tips = append(tips, fmt.Sprintf("钩偏大 鱼%.0fg＜钩%.0fg\n→ 买「%s」\n　%.0fg %d银 钓%.0f~%.0fg",
					w, gape, nm, cp, silver, cp*0.25, cp*0.9))
			} else {
				tips = append(tips, fmt.Sprintf("钩偏大 鱼%.0fg＜钩%.0fg\n→ 换更小号钩（商店有）", w, gape))
			}
		}
	}

	if rodG := spec.RodPowerKg * 1000; rodG > 0 && w > rodG {
		if name, _ := s.bestBackpackRod(ctx, uid, f.WeightG); name != "" {
			tips = append(tips, fmt.Sprintf("竿软 %.0fg＜鱼%.0fg\n→ 换「%s」", rodG, w, name))
		} else {
			tips = append(tips, fmt.Sprintf("竿软 %.0fg＜鱼%.0fg\n→ 背包无更硬竿", rodG, w))
		}
	}

	if lineG := spec.LineStrengthKg * 1000; lineG > 0 && w > lineG*0.9 {
		tips = append(tips, fmt.Sprintf("线细 %.0fg＜鱼%.0fg\n→ 换更粗主线", lineG, w))
	}

	// 最多报两条：钩、竿优先，线只在前面都没问题时才补，避免整块太长。
	if len(tips) > 2 {
		tips = tips[:2]
	}
	if len(tips) == 0 {
		return ""
	}
	// 标出是哪一根竿：多竿同在水里时，数值本来就是按触发这尾鱼的那根竿算的，
	// 但文案必须让玩家知道该动哪根。hookedRodLabel 优先给“快捷栏N号竿”，
	// 没有绑定快捷键时回退竿名 + guid 短码。
	lines := make([]string, 0, len(tips)+1)
	lines = append(lines, fmt.Sprintf("【装备】%s", s.hookedRodLabel(uid, rodGUID)))
	lines = append(lines, tips...)
	return strings.Join(lines, "\n")
}

// bestBackpackHook 返回背包里"能入口 weightG 克鱼"的最小钩及其入口上限。
func (s *Server) bestBackpackHook(ctx context.Context, uid, weightG int64) (string, float64) {
	items, err := s.DB.ListItemsByContainer(ctx, uid, "backpack")
	if err != nil {
		return "", 0
	}
	name, bestCap := "", 0.0
	for _, it := range items {
		def, derr := s.DB.GetItemDef(ctx, it.NumericID)
		if derr != nil || def == nil || def.Category != 12 {
			continue
		}
		capG := hookWeightCapG(itemDefF32(def, gearAttrHookLen), itemDefInt(def, gearAttrHookSize)) * gearOverloadFactor
		if capG < float64(weightG) {
			continue
		}
		if name == "" || capG < bestCap {
			if zh := s.itemZhName(ctx, def.NumericID); zh != "" {
				name, bestCap = zh, capG
			}
		}
	}
	return name, bestCap
}

// pickShopHook 按目标鱼重挑一支“能覆盖这尾鱼、商店在售、价格最低”的钩。
// 覆盖判据与服务端实际手感一致：入口上限 G 满足 0.25G <= 鱼重 <= 0.9G 时，
// hookSolidHold 的 solid 恒为 0.96，也就是“稳钓区间” = 0.25G ~ 0.9G。
// 返回：中文名、入口上限（克）。
func (s *Server) pickShopHook(ctx context.Context, weightG int64) (name string, capG float64, silver int64) {
	if s == nil || s.DB == nil || weightG <= 0 {
		return "", 0, 0
	}
	rows, err := s.DB.SQL.QueryContext(ctx, `
		SELECT COALESCE(NULLIF(c.name_zh,''), c.name), d.attrs_json, COALESCE(s.silver, 0)
		FROM item_def d
		JOIN item_catalog c ON c.numeric_id = d.numeric_id
		JOIN shop_item s ON s.item_id = c.item_id
		WHERE d.category = 12 AND COALESCE(s.stock, 0) > 0 AND COALESCE(s.silver, 0) > 0`)
	if err != nil {
		return "", 0, 0
	}
	defer rows.Close()
	lo := float64(weightG) * 1.11 // 鱼重 <= 0.9G：至少能扛住，别报“钩小”
	hi := float64(weightG) * 6.0  // 鱼重 >= 0.17G：再小就只剩嘬饵，放宽以免小鱼无货可荐
	bestPrice := int64(0)
	var fbName string
	var fbG float64
	var fbSilver int64
	for rows.Next() {
		var nm, attrs string
		var sil int64
		if rows.Scan(&nm, &attrs, &sil) != nil || nm == "" {
			continue
		}
		g := hookGapeFromAttrsJSON(attrs)
		if g <= 0 {
			continue
		}
		if g < lo || g > hi {
			// 备选：在售钩里入口最小的那支，保证小鱼也有一支可买。
			if fbName == "" || g < fbG {
				fbName, fbG, fbSilver = nm, g, sil
			}
			continue
		}
		if name == "" || sil < bestPrice {
			name, capG, silver, bestPrice = nm, g, sil, sil
		}
	}
	if name == "" && fbName != "" {
		name, capG, silver = fbName, fbG, fbSilver
	}
	return name, capG, silver
}

// hookGapeFromAttrsJSON 直接从 item_def.attrs_json 算钩入口上限，
// 免得为遍历整张钩目录而对每一行都调一次 GetItemDef。
func hookGapeFromAttrsJSON(raw string) float64 {
	var m map[string]any
	if json.Unmarshal([]byte(raw), &m) != nil || m == nil {
		return 0
	}
	ln, _ := decodeU32F32(m[gearAttrHookLen])
	var size int64
	switch n := m[gearAttrHookSize].(type) {
	case float64:
		size = int64(n)
	case int64:
		size = n
	}
	return hookWeightCapG(ln, size) * gearOverloadFactor
}

// bestBackpackRod 返回背包里能扛住 weightG 克鱼的最小竿及其承力（克）。
func (s *Server) bestBackpackRod(ctx context.Context, uid, weightG int64) (string, float64) {
	items, err := s.DB.ListItemsByContainer(ctx, uid, "backpack")
	if err != nil {
		return "", 0
	}
	name, bestCap := "", 0.0
	for _, it := range items {
		def, derr := s.DB.GetItemDef(ctx, it.NumericID)
		if derr != nil || def == nil || def.Category != 3 {
			continue
		}
		capG := rodPowerKg(def) * 1000
		if capG < float64(weightG)*1.2 {
			continue
		}
		if name == "" || capG < bestCap {
			if zh := s.itemZhName(ctx, def.NumericID); zh != "" {
				name, bestCap = zh, capG
			}
		}
	}
	return name, bestCap
}

// itemZhName 取物品中文名，取不到回退英文名。
func (s *Server) itemZhName(ctx context.Context, numericID int64) string {
	if s == nil || s.DB == nil || numericID == 0 {
		return ""
	}
	var zh, en string
	err := s.DB.SQL.QueryRowContext(ctx,
		`SELECT COALESCE(name_zh,''), COALESCE(name,'') FROM item_catalog WHERE numeric_id=?`,
		numericID).Scan(&zh, &en)
	if err != nil {
		return ""
	}
	if strings.TrimSpace(zh) != "" {
		return zh
	}
	return en
}
