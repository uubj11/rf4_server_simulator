package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

// 并发回归：连接池只有 1 条物理连接时，任何长事务都会把读请求堵在池里，
// 读方只能等 ctx 超时报「context deadline exceeded」——这正是本地服转圈刷屏
// 「请稍等…」的机制（2026-11-18 10:40–10:41 两次 deadline 实测）。
// 期望：写事务未提交期间，另一条读请求仍能在超时前拿到连接。
func TestConcurrentReadNotBlockedByOpenWriteTx(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "pool.db"))
	if err != nil {
		t.Fatalf("打开测试库: %v", err)
	}
	defer db.SQL.Close()
	if err := db.Migrate(); err != nil {
		t.Fatalf("迁移: %v", err)
	}
	if _, err := db.SQL.Exec(`CREATE TABLE IF NOT EXISTS pool_probe(n INTEGER NOT NULL)`); err != nil {
		t.Fatalf("建表: %v", err)
	}

	tx, err := db.SQL.Begin()
	if err != nil {
		t.Fatalf("开事务: %v", err)
	}
	if _, err := tx.Exec(`INSERT INTO pool_probe(n) VALUES(1)`); err != nil {
		tx.Rollback()
		t.Fatalf("写事务首条语句: %v", err)
	}
	defer tx.Rollback() // 故意不提交，模拟「意外长事务」

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var n int
	if err := db.SQL.QueryRowContext(ctx, `SELECT COUNT(*) FROM pool_probe`).Scan(&n); err != nil {
		t.Fatalf("读请求被未提交的写事务堵住（连接池只有 1 条连接）: %v", err)
	}
	t.Logf("并发读成功，count=%d", n)
}
