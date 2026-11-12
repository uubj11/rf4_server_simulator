package gameserver

import "testing"

// 口径必须跟钓鱼宝典的 Db.Wt 一致：<1000g 用整数克，>=1000g 折 kg 且最多两位小数。
func TestFmtWeightG(t *testing.T) {
	cases := []struct {
		in   float64
		want string
	}{
		{0, "—"},
		{31, "31g"},
		{97, "97g"},
		{388, "388g"},
		{999, "999g"},
		{1000, "1kg"},
		{1080, "1.08kg"},
		{1234, "1.23kg"},
		{1500, "1.5kg"},
		{2000, "2kg"},
		{3500, "3.5kg"},
		{10800, "10.8kg"},
		{12000, "12kg"},
	}
	for _, c := range cases {
		if got := fmtWeightG(c.in); got != c.want {
			t.Errorf("fmtWeightG(%v) = %q，期望 %q", c.in, got, c.want)
		}
	}
}
