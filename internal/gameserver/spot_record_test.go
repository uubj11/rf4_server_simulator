package gameserver

import "testing"

// TestSpotCoordKey 锁“落点坐标 → 宝典点位串”的格式与取整口径。
func TestSpotCoordKey(t *testing.T) {
	cases := []struct {
		x, z float64
		want string
	}{
		{59.4, 51.4, "59:51"},
		{59.5, 51.5, "60:52"}, // math.Round 半数向远离零取整
		{266, 295, "266:295"},
		{-0.4, 0.6, "0:1"},
		{668.2, 475.4, "668:475"},
	}
	for _, c := range cases {
		if got := spotCoordKey(c.x, c.z); got != c.want {
			t.Errorf("spotCoordKey(%v,%v) = %q, want %q", c.x, c.z, got, c.want)
		}
	}
}

// TestRecordCatchSpot 锁“入护后把坐标累加进 spot_fish”的行为与全部跳过条件。
func TestRecordCatchSpot(t *testing.T) {
	s, db := seedBaitWeightTest(t)
	ctx := t.Context()
	if _, err := db.SQL.Exec(`INSERT INTO loc_alias(alias, loc, note) VALUES('level_018_moskitnoe','铜湖','spot-record test')`); err != nil {
		t.Fatalf("准备 loc_alias 失败: %v", err)
	}

	rows := func() int {
		var n int
		if err := db.SQL.QueryRow(`SELECT COUNT(*) FROM spot_fish`).Scan(&n); err != nil {
			t.Fatalf("读 spot_fish 行数失败: %v", err)
		}
		return n
	}
	catches := func(loc, spot, fish string) int {
		var n int
		if err := db.SQL.QueryRow(`SELECT COALESCE(SUM(catches),0) FROM spot_fish
			WHERE loc=? AND spot=? AND fish_key=?`, loc, spot, fish).Scan(&n); err != nil {
			t.Fatalf("读 catches 失败: %v", err)
		}
		return n
	}

	// A2：首次入护落一行，slug 映射成中文图名，坐标压成 "横:纵"。
	s.recordCatchSpot(ctx, "level_018_moskitnoe", 59.4, 51.4, true, "perch")
	if got := rows(); got != 1 {
		t.Fatalf("首次记录后 spot_fish 行数 = %d, want 1", got)
	}
	if got := catches("铜湖", "59:51", "perch"); got != 1 {
		t.Fatalf("首次记录 catches = %d, want 1", got)
	}

	// A2：同图同点位同鱼种再入护，累加而不是新增行。
	s.recordCatchSpot(ctx, "level_018_moskitnoe", 59.4, 51.4, true, "perch")
	if got := catches("铜湖", "59:51", "perch"); got != 2 {
		t.Fatalf("第二次记录 catches = %d, want 2", got)
	}
	if got := rows(); got != 1 {
		t.Fatalf("累加后仍应只有 1 行, got %d", got)
	}

	// A3/A4：以下情形一律不写。
	for name, tc := range map[string]struct {
		loc  string
		x, z float64
		has  bool
		fish string
	}{
		"无落点坐标": {"level_018_moskitnoe", 59.4, 51.4, false, "perch"},
		"零坐标":   {"level_018_moskitnoe", 0, 0, true, "perch"},
		"负坐标":   {"level_018_moskitnoe", -5, 51, true, "perch"},
		"超上限":   {"level_018_moskitnoe", 5001, 51, true, "perch"},
		"空鱼种":   {"level_018_moskitnoe", 59.4, 51.4, true, ""},
		"未登记地图": {"level_999_unknown", 59.4, 51.4, true, "perch"},
		"空地图":   {"", 59.4, 51.4, true, "perch"},
	} {
		before := rows()
		s.recordCatchSpot(ctx, tc.loc, tc.x, tc.z, tc.has, tc.fish)
		if got := rows(); got != before {
			t.Errorf("%s：spot_fish 行数 %d → %d，期望不变", name, before, got)
		}
	}

	// A5：开关置 0 后不再写。
	if err := db.TunableSet(ctx, "spot_record", "0", "test"); err != nil {
		t.Fatalf("写开关失败: %v", err)
	}
	s.execAdminCmd(&AdminCommand{Op: "reload_tunables"})
	before := rows()
	s.recordCatchSpot(ctx, "level_018_moskitnoe", 70.2, 60.3, true, "perch")
	if got := rows(); got != before {
		t.Errorf("开关关闭后 spot_fish 行数 %d → %d，期望不变", before, got)
	}
}
