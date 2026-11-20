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
