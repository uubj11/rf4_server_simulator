package gameserver

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"slices"

	"rf4server/internal/store"
)

func qualificationCatchRecord(key string, ev qualificationEvent) store.QualificationFish {
	return store.QualificationFish{
		GUID: qualificationCountedGUID(key), FishGUID: ev.CatchGUID,
		Species: ev.Species, NameZh: ev.NameZh, WeightG: ev.WeightG,
		LengthM: ev.LengthM, Grade: ev.Grade, RigType: ev.RigType, RodType: ev.RodType,
	}
}

// 两条路径的证据保存在同一结算事务的 kv 中，客户端只接收进度较大的一条。
// 例如一尾普通鱼种星鱼 + 一尾未达星的罕见鱼，仍为 1/2，不能混成 2/2。
func countQualificationAlternativesTx(ctx context.Context, tx *sql.Tx, uid int64, rule qualificationRuleDef, st store.QualificationState, ev qualificationEvent) ([]store.QualificationFish, bool, error) {
	primaryKey, alternateKey := rule.Counted(ev), rule.AlternateCounted(ev)
	if primaryKey == "" && alternateKey == "" {
		return nil, false, nil
	}
	var progress struct {
		Primary   []store.QualificationFish `json:"primary"`
		Alternate []store.QualificationFish `json:"alternate"`
	}
	key := fmt.Sprintf("qualification-alternatives:%d:%d", uid, rule.ID)
	var raw string
	err := tx.QueryRowContext(ctx, `SELECT v FROM kv WHERE k=?`, key).Scan(&raw)
	switch err {
	case nil:
		if err := json.Unmarshal([]byte(raw), &progress); err != nil {
			return nil, false, fmt.Errorf("资格备选进度解析失败 id=%d: %w", rule.ID, err)
		}
	case sql.ErrNoRows:
		// 旧版本只实现星鱼路径，保留其已有证据。
		progress.Primary = append([]store.QualificationFish(nil), st.Counted...)
	default:
		return nil, false, err
	}
	updated := false
	if primaryKey != "" && !qualificationHasGUID(progress.Primary, qualificationCountedGUID(primaryKey)) {
		progress.Primary = append(progress.Primary, qualificationCatchRecord(primaryKey, ev))
		updated = true
	}
	if alternateKey != "" {
		alternateKey = "rare-species:" + alternateKey
		if !qualificationHasGUID(progress.Alternate, qualificationCountedGUID(alternateKey)) {
			progress.Alternate = append(progress.Alternate, qualificationCatchRecord(alternateKey, ev))
			updated = true
		}
	}
	if !updated {
		return nil, false, nil
	}
	encoded, err := json.Marshal(progress)
	if err != nil {
		return nil, false, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO kv(k,v) VALUES(?,?) ON CONFLICT(k) DO UPDATE SET v=excluded.v`, key, string(encoded)); err != nil {
		return nil, false, err
	}
	visible := progress.Primary
	if len(progress.Alternate) > len(visible) {
		visible = progress.Alternate
	}
	return visible, !slices.Equal(st.Counted, visible), nil
}
