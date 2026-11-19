package gameserver

import (
	"math"
	"testing"
)

// TestSpotCoordKey 锁“落点坐标 → 点位串”的格式与取整口径（日志/排障展示用）。
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

// TestSpotHUDKey 锁“世界坐标 → 游戏地图 HUD 读数”的换算：
// （世界坐标 − 场景原点）/ 5，按最近偶数取整，与客户端显示口径一致。
func TestSpotHUDKey(t *testing.T) {
	cases := []struct {
		level    string
		x, z     float64
		want     string
	}{
		{"level_001_lake", 286, 419, "57:84"},           // 原点 0：[0,600) 图，落点 286/5=57.2、419/5=83.8
		{"level_004_torfyanoe_ozero", 149, 304, "30:61"}, // 原点 0：[0,400) 图
		{"level_018_moskitnoe", 59.4, 51.4, "37:36"},     // 原点 -128：(59.4+128)/5=37.48
		{"level_019_american_pond", 194, 314, "64:88"},   // 原点 -127.4：(194+127.4)/5=64.28
		{"level_999_unknown", 59, 51, ""},                // 未登记标定：不臆造
	}
	for _, c := range cases {
		if got := spotHUDKey(c.level, c.x, c.z); got != c.want {
			t.Errorf("spotHUDKey(%s,%v,%v) = %q, want %q", c.level, c.x, c.z, got, c.want)
		}
	}
}

// TestRecordCatchSpot 锁“入护后把落点累加进玩家自钓点位表”的行为与全部跳过条件。
//
// 关键：这里用 openStaticTestDB 起库——静态库只读 ATTACH + authorizer 全开，
// 与生产接线一致。旧实现写的是只读的静态表 spot_fish，在这个接线下恒被拒绝
// （SQLITE_AUTH 23 / authorization denied），而老的用例用的是不带静态库的库，
// 所以线上每条鱼刷 WARN 却一直是绿的。
func TestRecordCatchSpot(t *testing.T) {
	db := openStaticTestDB(t)
	s := newLocationTestServer(t, db)
	ctx := t.Context()
	// loc_alias 本身也是只读静态表，这里直接用静态库里的既有映射：
	// level_018_moskitnoe → 铜湖。

	const uid = int64(1000001)
	rows := func() int {
		var n int
		if err := db.SQL.QueryRow(`SELECT COUNT(*) FROM spot_catch_player`).Scan(&n); err != nil {
			t.Fatalf("读 spot_catch_player 行数失败: %v", err)
		}
		return n
	}
	catches := func(loc string, x, z float64, fish string) int {
		var n int
		if err := db.SQL.QueryRow(`SELECT COALESCE(SUM(catches),0) FROM spot_catch_player
			WHERE uid=? AND loc=? AND x=? AND z=? AND fish_key=?`, uid, loc, math.Round(x), math.Round(z), fish).Scan(&n); err != nil {
			t.Fatalf("读 catches 失败: %v", err)
		}
		return n
	}

	// A1：首次入护落一行，slug 映射成中文图名，坐标取整落格。
	s.recordCatchSpot(ctx, uid, "level_018_moskitnoe", 59.4, 51.4, true, "perch")
	if got := rows(); got != 1 {
		t.Fatalf("首次记录后 spot_catch_player 行数 = %d, want 1（写静态表会在这里被 authorizer 拒绝）", got)
	}
	if got := catches("铜湖", 59.4, 51.4, "perch"); got != 1 {
		t.Fatalf("首次记录 catches = %d, want 1", got)
	}
	// HUD 坐标同时落库：（59.4+128)/5=37.48→37、(51.4+128)/5=35.88→36。
	var hud string
	if err := db.SQL.QueryRow(`SELECT hud FROM spot_catch_player WHERE uid=? AND loc='铜湖'`, uid).Scan(&hud); err != nil {
		t.Fatalf("读 hud 失败: %v", err)
	}
	if hud != "37:36" {
		t.Fatalf("hud = %q, want %q", hud, "37:36")
	}

	// A2：同图同点位同鱼种再入护，累加而不是新增行。
	s.recordCatchSpot(ctx, uid, "level_018_moskitnoe", 59.4, 51.4, true, "perch")
	if got := catches("铜湖", 59.4, 51.4, "perch"); got != 2 {
		t.Fatalf("第二次记录 catches = %d, want 2", got)
	}
	if got := rows(); got != 1 {
		t.Fatalf("累加后仍应只有 1 行, got %d", got)
	}

	// A3/A4：以下情形一律不写。
	for name, tc := range map[string]struct {
		uid  int64
		loc  string
		x, z float64
		has  bool
		fish string
	}{
		"无玩家":   {0, "level_018_moskitnoe", 59.4, 51.4, true, "perch"},
		"无落点坐标": {uid, "level_018_moskitnoe", 59.4, 51.4, false, "perch"},
		"零坐标":   {uid, "level_018_moskitnoe", 0, 0, true, "perch"},
		"负坐标":   {uid, "level_018_moskitnoe", -5, 51, true, "perch"},
		"超上限":   {uid, "level_018_moskitnoe", 5001, 51, true, "perch"},
		"空鱼种":   {uid, "level_018_moskitnoe", 59.4, 51.4, true, ""},
		"未登记地图": {uid, "level_999_unknown", 59.4, 51.4, true, "perch"},
		"空地图":   {uid, "", 59.4, 51.4, true, "perch"},
	} {
		before := rows()
		u := tc.uid
		if u == 0 && name != "无玩家" {
			u = uid
		}
		s.recordCatchSpot(ctx, u, tc.loc, tc.x, tc.z, tc.has, tc.fish)
		if got := rows(); got != before {
			t.Errorf("%s：spot_catch_player 行数 %d → %d，期望不变", name, before, got)
		}
	}

	// A5：开关置 0 后不再写。
	if err := db.TunableSet(ctx, "spot_record", "0", "test"); err != nil {
		t.Fatalf("写开关失败: %v", err)
	}
	s.execAdminCmd(&AdminCommand{Op: "reload_tunables"})
	before := rows()
	s.recordCatchSpot(ctx, uid, "level_018_moskitnoe", 70.2, 60.3, true, "perch")
	if got := rows(); got != before {
		t.Errorf("开关关闭后 spot_catch_player 行数 %d → %d，期望不变", before, got)
	}
}

// TestLoadPlayerSpotPreference 锁“按当前落点半径命中玩家自钓记录”的口径：
// 半径内加权、半径外不计、开关关闭即不生效。
func TestLoadPlayerSpotPreference(t *testing.T) {
	db := openStaticTestDB(t)
	s := newLocationTestServer(t, db)
	ctx := t.Context()

	seed := `INSERT INTO spot_catch_player(uid, loc, x, z, fish_key, spot, catches, updated_ts) VALUES
		(1000001,'铜湖',59,51,'perch','',9,0),
		(1000001,'铜湖',260,300,'bream','',16,0),
		(1000002,'铜湖',59,51,'zander','',25,0),
		(1000001,'挪威海',41,112,'atl_cod','215',25,0),
		(1000001,'挪威海',41,112,'atl_haddock','',9,0),
		(1000001,'挪威海',600,400,'atl_cod','120',4,0)`
	if _, err := db.SQL.Exec(seed); err != nil {
		t.Fatalf("准备自钓点位失败: %v", err)
	}

	c := &fishRollContext{uid: 1000001, hasPos: true, x: 60, z: 52, runtime: &fishRollRuntime{}}
	pref, ok := s.loadPlayerSpotPreference(ctx, c, "铜湖")
	if !ok {
		t.Fatalf("半径内应有自钓记录")
	}
	if got, want := pref["perch"], math.Sqrt(9); math.Abs(got-want) > 1e-9 {
		t.Errorf("perch 权重 = %v, want %v", got, want)
	}
	if _, has := pref["bream"]; has {
		t.Errorf("半径外的记录不该计入: %v", pref)
	}
	if _, has := pref["zander"]; has {
		t.Errorf("别的玩家的记录不该计入: %v", pref)
	}

	// 海图地图：按入护时换算好的海图区号命中，与攻略侧同口径；且不叠半径（不重复计数）。
	sea := &fishRollContext{uid: 1000001, hasPos: true, x: 41, z: 112,
		runtime: &fishRollRuntime{chartKnown: true, zoneKnown: true, zone: 215}}
	seaPref, ok := s.loadPlayerSpotPreference(ctx, sea, "挪威海")
	if !ok {
		t.Fatalf("215 号区应有自钓记录")
	}
	if got, want := seaPref["atl_cod"], math.Sqrt(25); math.Abs(got-want) > 1e-9 {
		t.Errorf("atl_cod 区号权重 = %v, want %v（同一坐标的 120 号区不该并进来）", got, want)
	}
	if _, has := seaPref["atl_haddock"]; has {
		t.Errorf("区号为空、只靠半径命中的记录不该计进海图口径: %v", seaPref)
	}

	// 半径收紧到 1 格：同一点位（相距 ~1.4 格）不再命中。
	if err := db.TunableSet(ctx, tunableSpotPlayerRadius, "0.5", "test"); err != nil {
		t.Fatal(err)
	}
	s.execAdminCmd(&AdminCommand{Op: "reload_tunables"})
	if pref, ok := s.loadPlayerSpotPreference(ctx, c, "铜湖"); ok {
		t.Errorf("半径 0.5 时不该命中，got %v", pref)
	}
	if err := db.TunableSet(ctx, tunableSpotPlayerRadius, "20", "test"); err != nil {
		t.Fatal(err)
	}

	// 开关关闭：一律不生效。
	if err := db.TunableSet(ctx, tunableSpotPlayerBias, "0", "test"); err != nil {
		t.Fatal(err)
	}
	s.execAdminCmd(&AdminCommand{Op: "reload_tunables"})
	if pref, ok := s.loadPlayerSpotPreference(ctx, c, "铜湖"); ok {
		t.Errorf("开关关闭时不该命中，got %v", pref)
	}
}

// TestSpotReferencesPlayerToggle 锁宝典钓点参考的两种口径：
// 默认只给攻略点位（不混坐标系）；打开开关后并列玩家自己的点位（客户端位置坐标），
// 且只取本人的记录。
func TestSpotReferencesPlayerToggle(t *testing.T) {
	// 这里要写 spot_fish，必须用未挂静态库的夹具（挂静态库时静态表运行期只读）。
	s, db := seedBaitWeightTest(t)
	ctx := t.Context()

	if _, err := db.SQL.Exec(`INSERT INTO spot_fish(loc,spot,fish_key,catches) VALUES('铜湖','59:51','perch',5)`); err != nil {
		t.Fatalf("准备攻略点位失败: %v", err)
	}
	seed := `INSERT INTO spot_catch_player(uid, loc, x, z, fish_key, spot, hud, catches, updated_ts) VALUES
		(1000001,'铜湖',268,408,'perch','','57:84',7,0),
		(1000001,'铜湖',300,500,'bream','','',6,0),
		(1000002,'铜湖',300,500,'perch','','60:100',99,0)`
	if _, err := db.SQL.Exec(seed); err != nil {
		t.Fatalf("准备自钓点位失败: %v", err)
	}

	// 默认：只有攻略点位。
	refs, err := s.spotReferences(ctx, "铜湖", "perch", 1000001)
	if err != nil {
		t.Fatalf("取参考失败: %v", err)
	}
	if len(refs) != 1 || refs[0].Coordinate != "59:51" || refs[0].Catches != 5 {
		t.Fatalf("默认应只给攻略点位，got %+v", refs)
	}

	// 打开开关：并列玩家自己的点位（按次数倒序），别人的记录不出现。
	if err := db.TunableSet(ctx, tunableSpotShowPlayerRefs, "1", "test"); err != nil {
		t.Fatal(err)
	}
	s.execAdminCmd(&AdminCommand{Op: "reload_tunables"})
	refs, err = s.spotReferences(ctx, "铜湖", "perch", 1000001)
	if err != nil {
		t.Fatalf("取参考失败: %v", err)
	}
	if len(refs) != 2 {
		t.Fatalf("应并列 2 条（攻略 + 自己），got %+v", refs)
	}
	// 玩家点位用 HUD 读数（与攻略同口径），不是原始世界坐标。
	if refs[0].Coordinate != "57:84" || refs[0].Catches != 7 {
		t.Errorf("首条应是自己的 HUD 点位 57:84，got %+v", refs[0])
	}
	for _, r := range refs {
		if r.Coordinate == "60:100" {
			t.Errorf("别的玩家点位不该出现: %+v", refs)
		}
	}
}
