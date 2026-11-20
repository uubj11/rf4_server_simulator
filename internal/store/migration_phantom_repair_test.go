package store

import (
	"path/filepath"
	"testing"
)

// 幽灵迁移登记：schema_migrations 里有行、但对象并不存在（历史上合并反复回退、
// 迁移按文件名改号后可能出现）。Migrate 必须识别并删掉这些行重新重放，
// 否则表/列永久缺失，运行期静默降级。
func TestMigrateRepairsPhantomRegistrations(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "phantom.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.SQL.Close()
	if err := db.Migrate(); err != nil {
		t.Fatalf("首次迁移: %v", err)
	}
	for _, tbl := range []string{"fish_hunt_pity", "growth_set_state", "skill_cast_effect_def"} {
		if !tableExists(t, db, tbl) {
			t.Fatalf("首次迁移后缺表 %s", tbl)
		}
	}
	// 制造幽灵：登记行还在，表被删（模拟 SQL 从没跑过/被回退）。
	for _, tbl := range []string{"fish_hunt_pity", "growth_set_state", "skill_cast_effect_def"} {
		if _, err := db.SQL.Exec(`DROP TABLE ` + tbl); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Migrate(); err != nil {
		t.Fatalf("修复后迁移: %v", err)
	}
	for _, tbl := range []string{"fish_hunt_pity", "growth_set_state", "skill_cast_effect_def"} {
		if !tableExists(t, db, tbl) {
			t.Fatalf("幽灵行未修复，缺表 %s", tbl)
		}
	}
	// 再跑一次必须完全幂等。
	if err := db.Migrate(); err != nil {
		t.Fatalf("二次迁移不幂等: %v", err)
	}
}

func tableExists(t *testing.T, db *DB, name string) bool {
	t.Helper()
	var n int
	if err := db.SQL.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, name).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n > 0
}

// 重复登记：库里同时留着一行「旧编号+旧文件名」和一行「新编号+新文件名」
// （历史合并回退叠加改号后真实出现），旧号重映射会撞 UNIQUE。Migrate 必须容忍。
func TestMigrateToleratesDuplicateRenumberedRows(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "dup.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.SQL.Close()
	if err := db.Migrate(); err != nil {
		t.Fatalf("首次迁移: %v", err)
	}
	// 让 60 空出来，再把 78 那行改成「旧编号 60 + 旧文件名」，形成重复。
	if _, err := db.SQL.Exec(`UPDATE schema_migrations SET version=9000060 WHERE version=60`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL.Exec(`UPDATE schema_migrations SET version=60, name='migrations/00060_pity_lane_aggregate.sql'
		WHERE name='migrations/00078_pity_lane_aggregate.sql'`); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(); err != nil {
		t.Fatalf("重复登记下迁移失败: %v", err)
	}
	if !tableExists(t, db, "fish_hunt_pity") {
		t.Fatal("fish_hunt_pity 丢失")
	}
	var dup int
	if err := db.SQL.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE name='migrations/00078_pity_lane_aggregate.sql'`).Scan(&dup); err != nil {
		t.Fatal(err)
	}
	if dup != 1 {
		t.Fatalf("00078 登记行应只剩 1 行，实得 %d", dup)
	}
}
