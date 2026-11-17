package store

import (
	"context"
	"database/sql"
	"time"
)

// GrowthItem 记录一件物品实例属于哪套成长套装、在套内扮演什么角色。
// 归属存在这张表而不是物品主表，玩家的等级则挂在 set_key 上，套装内所有件共用。
type GrowthItem struct {
	GUID   string
	UID    int64
	SetKey string
	Role   string
}

// GrowthSetState 是玩家某套成长套装的等级与累计经验。
type GrowthSetState struct {
	UID    int64
	SetKey string
	Level  int64
	XP     int64
}

// GrowthItemInsert 幂等登记一件套装归属，重复写入同一 guid 不覆盖。
func (d *DB) GrowthItemInsert(ctx context.Context, it GrowthItem) error {
	return d.GrowthItemInsertTx(ctx, nil, it)
}

// GrowthItemInsertTx 在给定事务（可为 nil）内登记套装归属。
func (d *DB) GrowthItemInsertTx(ctx context.Context, tx *sql.Tx, it GrowthItem) error {
	if d == nil || it.GUID == "" || it.SetKey == "" || it.Role == "" {
		return nil
	}
	const q = `INSERT INTO growth_item(guid,uid,set_key,role,created_ts) VALUES(?,?,?,?,?)
		ON CONFLICT (guid) DO NOTHING`
	ts := float64(time.Now().Unix())
	if tx != nil {
		_, err := tx.ExecContext(ctx, q, it.GUID, it.UID, it.SetKey, it.Role, ts)
		return err
	}
	_, err := d.SQL.ExecContext(ctx, q, it.GUID, it.UID, it.SetKey, it.Role, ts)
	return err
}

// GrowthItemByGUID 取单件归属；没登记返回 nil, nil。
func (d *DB) GrowthItemByGUID(ctx context.Context, guid string) (*GrowthItem, error) {
	row := d.SQL.QueryRowContext(ctx, `SELECT guid,uid,set_key,role FROM growth_item WHERE guid=?`, guid)
	it := &GrowthItem{}
	if err := row.Scan(&it.GUID, &it.UID, &it.SetKey, &it.Role); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return it, nil
}

// GrowthItemRoles 返回该玩家该套的「角色 → 归属」，同一角色取最早登记的一件。
func (d *DB) GrowthItemRoles(ctx context.Context, uid int64, setKey string) (map[string]GrowthItem, error) {
	rows, err := d.SQL.QueryContext(ctx,
		`SELECT guid,uid,set_key,role FROM growth_item WHERE uid=? AND set_key=? ORDER BY created_ts ASC`, uid, setKey)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]GrowthItem{}
	for rows.Next() {
		var it GrowthItem
		if err := rows.Scan(&it.GUID, &it.UID, &it.SetKey, &it.Role); err != nil {
			return nil, err
		}
		if _, ok := out[it.Role]; !ok {
			out[it.Role] = it
		}
	}
	return out, rows.Err()
}

// GrowthSetKeysOwned 返回该玩家已持有的全部套装 set_key，一人一套的判定入口。
func (d *DB) GrowthSetKeysOwned(ctx context.Context, uid int64) (map[string]bool, error) {
	rows, err := d.SQL.QueryContext(ctx, `SELECT DISTINCT set_key FROM growth_item WHERE uid=?`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, err
		}
		out[key] = true
	}
	return out, rows.Err()
}

// GrowthSetStateGet 读某套等级；无记录返回 level=0。
func (d *DB) GrowthSetStateGet(ctx context.Context, uid int64, setKey string) (GrowthSetState, error) {
	st := GrowthSetState{UID: uid, SetKey: setKey}
	err := d.SQL.QueryRowContext(ctx,
		`SELECT level,xp FROM growth_set_state WHERE uid=? AND set_key=?`, uid, setKey).
		Scan(&st.Level, &st.XP)
	if err == sql.ErrNoRows {
		return st, nil
	}
	return st, err
}

// GrowthSetStateUpsert 写等级与经验；事务可选。
func (d *DB) GrowthSetStateUpsert(ctx context.Context, tx *sql.Tx, st GrowthSetState) error {
	if st.SetKey == "" {
		return nil
	}
	const q = `INSERT INTO growth_set_state(uid,set_key,level,xp,updated_ts) VALUES(?,?,?,?,?)
		ON CONFLICT (uid,set_key) DO UPDATE SET level=excluded.level, xp=excluded.xp, updated_ts=excluded.updated_ts`
	ts := float64(time.Now().Unix())
	if tx != nil {
		_, err := tx.ExecContext(ctx, q, st.UID, st.SetKey, st.Level, st.XP, ts)
		return err
	}
	_, err := d.SQL.ExecContext(ctx, q, st.UID, st.SetKey, st.Level, st.XP, ts)
	return err
}

// GrowthSetStateEnsureTx 只在缺行时建档（level=0），已有进度原样保留。
// 购买套装时用它把「已持有」落到库里，重复购买由调用方前置拦截。
func (d *DB) GrowthSetStateEnsureTx(ctx context.Context, tx *sql.Tx, uid int64, setKey string) error {
	if d == nil || setKey == "" {
		return nil
	}
	const q = `INSERT INTO growth_set_state(uid,set_key,level,xp,updated_ts) VALUES(?,?,0,0,?)
		ON CONFLICT (uid,set_key) DO NOTHING`
	ts := float64(time.Now().Unix())
	if tx != nil {
		_, err := tx.ExecContext(ctx, q, uid, setKey, ts)
		return err
	}
	_, err := d.SQL.ExecContext(ctx, q, uid, setKey, ts)
	return err
}
