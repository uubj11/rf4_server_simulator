package gameserver

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"math"
	"strings"
)

// fishing_bait_rules_strict.json 是后台 fish_bait_override 中 254 条精确
// allow 清单的导出。只在 bait_exact_only=1 时启用；默认规则表仍按
// fishing_bait_rules.txt 走，避免改变上游已有行为。
//
//go:embed fishing_bait_rules_strict.json
var fishBaitRuleStrictJSON []byte

type strictFishBaitRuleJSON struct {
	Fish      string   `json:"fish"`
	Allow     []string `json:"allow"`
	Deny      []string `json:"deny"`
	Bottom    []string `json:"bottom"`
	Condition string   `json:"condition"`
}

var fishBaitRulesStrict = parseStrictFishBaitRules()

func parseStrictFishBaitRules() map[string]fishBaitRule {
	var rows []strictFishBaitRuleJSON
	if err := json.Unmarshal(fishBaitRuleStrictJSON, &rows); err != nil {
		panic("fishing_bait_rules_strict.json: " + err.Error())
	}
	out := make(map[string]fishBaitRule, len(rows))
	for _, row := range rows {
		name := strings.TrimSpace(row.Fish)
		allow := cleanStrictRuleTerms(row.Allow)
		if name == "" || len(allow) == 0 {
			continue
		}
		r := fishBaitRule{
			allow:      allow,
			deny:       cleanStrictRuleTerms(row.Deny),
			bottomOnly: cleanStrictRuleTerms(row.Bottom),
			condition:  strings.TrimSpace(row.Condition),
		}
		r.deriveConditionFlags()
		out[ruleName(name)] = r
	}
	return out
}

func cleanStrictRuleTerms(in []string) []string {
	out := make([]string, 0, len(in))
	for _, v := range in {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	return out
}

// lookupStrictFishBaitRule 与 lookupFishBaitRule 使用同一套鱼名归一化和
// “去/加结尾鱼”回退，但只查严格清单，不会把 fishing_bait_rules.txt 的
// 宽泛规则带进 strict 模式。
func lookupStrictFishBaitRule(fishZh string) (fishBaitRule, bool) {
	key := ruleName(fishZh)
	if r, ok := fishBaitRulesStrict[key]; ok {
		return r, true
	}
	if strings.HasSuffix(key, "鱼") {
		if r, ok := fishBaitRulesStrict[strings.TrimSuffix(key, "鱼")]; ok {
			return r, true
		}
	}
	if r, ok := fishBaitRulesStrict[key+"鱼"]; ok {
		return r, true
	}
	return fishBaitRule{}, false
}

// strictBaitScore 是绝对严格模式的判饵：
//   - 没有 allow 清单的鱼不咬；
//   - 组合饵必须每一项都在 allow 清单里，任何一项不在，整组不咬；
//   - deny / bottomOnly 仍然逐项生效；
//   - 不再看 companion 证据、rf4 实测占比或同食性杂口兜底。
func strictBaitScore(r fishBaitRule, baits []ruleBait, c fishRollContext, loc, key string, period int64) (float64, bool) {
	if len(r.allow) == 0 || len(baits) == 0 {
		return 0, false
	}
	for _, b := range baits {
		if !b.matchesAny(r.allow) {
			return 0, false
		}
	}
	for _, b := range baits {
		if baitBlockedByRule(r, c, b) {
			return 0, false
		}
	}
	best := 0.0
	preferred := 0
	for _, b := range baits {
		variation := fishVariation(fmt.Sprintf("%s/%s/%s/%d", loc, key, b.id, period))
		best = math.Max(best, 1.0+0.5*variation)
		preferred++
	}
	if best <= 0 {
		return 0, false
	}
	if preferred > 1 {
		best *= 1 + math.Min(0.15, 0.05*float64(preferred-1))
	}
	return best, true
}
