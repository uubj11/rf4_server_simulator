package gameserver

import (
	"context"
	"testing"
)

// 客户端鱼名与规则表长期存在同物异写，差一个码位就整条规则失效：
// 「大西洋鲑」vs「大西洋鲑鱼」、「泥鳅鱼」vs「泥鳅」、
// 「钓鮟鱇」vs「钓𩽾𩾌」（异体字）。lookupFishBaitRule 必须把它们并起来。
func TestLookupFishBaitRuleToleratesNameVariants(t *testing.T) {
	pairs := [][2]string{
		{"大西洋鲑", "大西洋鲑鱼"},
		{"泥鳅鱼", "泥鳅"},
		{"钓鮟鱇", "钓𩽾𩾌"},
	}
	for _, p := range pairs {
		a, okA := lookupFishBaitRule(p[0])
		b, okB := lookupFishBaitRule(p[1])
		if !okA || !okB {
			t.Fatalf("%s/%s：命中最少一边失败 ok=%v/%v", p[0], p[1], okA, okB)
		}
		if len(a.allow) != len(b.allow) || len(a.allow) == 0 {
			t.Fatalf("%s/%s：允许列表不一致 %v vs %v", p[0], p[1], a.allow, b.allow)
		}
	}
	if _, ok := lookupFishBaitRule("完全不存在的鱼"); ok {
		t.Fatal("不存在的鱼不应命中规则")
	}
	if _, ok := lookupFishBaitRule(""); ok {
		t.Fatal("空名不应命中规则")
	}
}

// resolveBaitRule 是判鱼链路的入口。它必须和 lookupFishBaitRule 用同一套
// 归一：库里同一鱼种可能有两个写法（loc_wiki_fish 记「泥鳅鱼」、loc_fish 记
// 「泥鳅」），loadFishNames 取到哪一个取决于 UNION 的行序，所以谁先被读到
// 就会决定这条鱼有没有规则——这正是 TestBiteGateDietDualTicket 偶发的根因：
// 泥鳅鱼拿不到规则 → 只剩单票 → 0.05 权重 → 64 次摇不到。
func TestResolveBaitRuleUsesNameVariants(t *testing.T) {
	s, _ := seedBaitRuleTest(t)
	for _, name := range []string{"泥鳅鱼", "泥鳅"} {
		r, _ := s.resolveBaitRule(context.Background(), name)
		if len(r.allow) == 0 {
			t.Fatalf("%s：resolveBaitRule 丢了规则（allow 为空）", name)
		}
	}
}
