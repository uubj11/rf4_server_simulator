package gameserver

import (
	"strings"
	"testing"
)

// TestHookUpgradeAcceptable 锁"换大钩是否真的有用"的判据：
// 鱼必须落进新钩的稳钓带（入口上限 >= 鱼重/0.9），且入口上限严格大于当前钩。
func TestHookUpgradeAcceptable(t *testing.T) {
	cases := []struct {
		name    string
		capG    float64
		fishG   float64
		current float64
		want    bool
	}{
		{"同规格钩（换完照样跑鱼）", 10800, 10530, 10800, false},
		{"鱼超出稳钓上限", 10800, 10530, 0, false},
		{"刚好达标且更大", 11700, 10530, 10800, true},
		{"够大够稳", 12000, 10530, 10800, true},
		{"比当前钩还小", 10500, 9000, 10800, false},
		{"非法输入", 0, 10530, 10800, false},
	}
	for _, c := range cases {
		if got := hookUpgradeAcceptable(c.capG, c.fishG, c.current); got != c.want {
			t.Errorf("%s: hookUpgradeAcceptable(%v,%v,%v) = %v, want %v", c.name, c.capG, c.fishG, c.current, got, c.want)
		}
	}
}

// TestGearAdviceTipText 锁提示文案的口径：比较值必须是"稳钓上限"(0.9*标称)，
// 标称单独附注。上一版写成 "钩小 10.8kg＜鱼10.53kg"、"线细 11.42kg＜鱼10.53kg"，
// 这两个不等式本身就是假的（10.8 > 10.53、11.42 > 10.53）。
func TestGearAdviceTipText(t *testing.T) {
	const (
		gape  = 10800.0 // 钩标称 10.8kg，稳钓上限 9.72kg
		lineG = 11420.0 // 线标称 11.42kg，稳钓上限 10.28kg
		fish  = 10530.0
	)

	hook := hookSmallTip(gape, fish, "WG Hook S2", 12000)
	for _, want := range []string{"稳钓9.72kg≤鱼10.53kg", "这枚钩标称10.8kg", "换「WG Hook S2」12kg", "钓3kg~10.8kg"} {
		if !strings.Contains(hook, want) {
			t.Errorf("钩提示缺少 %q，实际:\n%s", want, hook)
		}
	}
	if strings.Contains(hook, "10.8kg＜鱼10.53kg") {
		t.Errorf("钩提示仍用标称做比较，式子为假:\n%s", hook)
	}

	shop := hookSmallTipShop(gape, fish, "WG Hook S2", 12000, 1500)
	if !strings.Contains(shop, "稳钓9.72kg≤鱼10.53kg") || !strings.Contains(shop, "→ 买「WG Hook S2」") {
		t.Errorf("商店钩提示不符合口径:\n%s", shop)
	}

	none := hookSmallNoUpgradeTip(gape, fish)
	if !strings.Contains(none, "稳钓9.72kg≤鱼10.53kg") || !strings.Contains(none, "背包与商店都没有更大号的钩") {
		t.Errorf("无升级钩提示不符合口径:\n%s", none)
	}

	line := lineThinTip(lineG, fish)
	for _, want := range []string{"线偏细 稳钓10.28kg≤鱼10.53kg", "这盘线标称11.42kg", "→ 换更粗主线"} {
		if !strings.Contains(line, want) {
			t.Errorf("线提示缺少 %q，实际:\n%s", want, line)
		}
	}
	if strings.Contains(line, "11.42kg＜鱼10.53kg") {
		t.Errorf("线提示仍用标称做比较，式子为假:\n%s", line)
	}
}
