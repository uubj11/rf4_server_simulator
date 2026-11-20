package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// FishZone 一条区域固定鱼种名册。kind 决定几何：
//   - grid_rect：内陆 40m 世界网格矩形（gx=floor(x/40)，含端点）；
//   - grid_cells：内陆 40m 网格组合（地图刷选，任意形状）；
//   - circle：圆形钓点区（中心世界坐标 cx/cz + 半径米），坑/窄口等小面积米级精度；
//   - sea_zone：挪威海声呐海图区号（与客户端水域编号同源）。
//
// mode 决定判鱼语义：exclusive 只出名单内鱼种（∩ 地图名册）；
// boost 名单内乘 boost、名单外乘 keep_others（沿用海图观测语义）。
type FishZone struct {
	ID         int64      `json:"id"`
	Loc        string     `json:"loc"`
	Name       string     `json:"name"`
	Kind       string     `json:"kind"`
	SeaZone    int64      `json:"sea_zone"`
	SeaZones   []int64    `json:"sea_zones,omitempty"` // 海图区号集合（图上框选）；空=旧单区号 sea_zone
	GX0        int64      `json:"gx0"`
	GZ0        int64      `json:"gz0"`
	GX1        int64      `json:"gx1"`
	GZ1        int64      `json:"gz1"`
	Cells      [][2]int64 `json:"cells,omitempty"`
	Cx         float64    `json:"cx,omitempty"`
	Cz         float64    `json:"cz,omitempty"`
	Radius     float64    `json:"radius,omitempty"`
	Mode       string     `json:"mode"`
	Boost      float64    `json:"boost"`
	KeepOthers float64    `json:"keep_others"`
	// boostPresent 仅记录 JSON 是否显式提供了 boost；省略时保存按旧兼容语义落 1，
	// 显式 0 则必须交给 Validate 拒绝，不能借“缺省”掩盖非法乘数。
	boostPresent bool
	Enabled      bool           `json:"enabled"`
	UpdatedBy    string         `json:"updated_by"`
	UpdatedTs    float64        `json:"updated_ts"`
	Fish         []FishZoneFish `json:"fish"`
}

// FishZoneFish 名册内一行鱼种：weight 是区内的相对权重乘数（默认 1）。
type FishZoneFish struct {
	FishKey string  `json:"fish_key"`
	Weight  float64 `json:"weight"`
	Enabled bool    `json:"enabled"`
}

func (z FishZone) Validate() error {
	if strings.TrimSpace(z.Loc) == "" {
		return fmt.Errorf("水域不能为空")
	}
	switch z.Kind {
	case "sea_zone":
		if len(z.SeaZones) > 0 {
			if len(z.SeaZones) > 300 {
				return fmt.Errorf("海图区号最多 300 个")
			}
			for _, n := range z.SeaZones {
				if n <= 0 {
					return fmt.Errorf("海图区号必须是正整数")
				}
			}
		} else if z.SeaZone <= 0 {
			return fmt.Errorf("海图区号必须是正整数")
		}
	case "grid_rect":
		if z.GX0 > z.GX1 || z.GZ0 > z.GZ1 {
			return fmt.Errorf("网格矩形坐标颠倒：需要 gx0<=gx1、gz0<=gz1")
		}
	case "grid_cells":
		if len(z.Cells) == 0 {
			return fmt.Errorf("格子区域至少要涂一个 40m 格子")
		}
		if len(z.Cells) > 2048 {
			return fmt.Errorf("格子区域最多 2048 格")
		}
	case "circle":
		if z.Radius < 2 || z.Radius > 500 {
			return fmt.Errorf("圆形区域半径需在 2~500 米")
		}
	default:
		return fmt.Errorf("区域类型只能是 grid_rect、grid_cells、circle 或 sea_zone")
	}
	switch z.Mode {
	case "exclusive", "boost":
	default:
		return fmt.Errorf("模式只能是 exclusive 或 boost")
	}
	// 乘数离谱的增益区等于变相关区/爆权重：与 UI 输入约束同口径。
	if z.Boost < 0.1 || z.Boost > 100 {
		return fmt.Errorf("名单内乘数需在 0.1~100")
	}
	if z.KeepOthers < 0 || z.KeepOthers > 1 {
		return fmt.Errorf("名单外保留系数需在 0~1")
	}
	return nil
}

// UnmarshalJSON 保留 boost 字段是否出现的信息。普通 float64 无法区分
// “没传”与“显式 0”，而保存合同要求前者走兼容缺省、后者按越界拒绝。
func (z *FishZone) UnmarshalJSON(data []byte) error {
	type fishZoneAlias FishZone
	var decoded fishZoneAlias
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	var probe struct {
		Boost *float64 `json:"boost"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return err
	}
	*z = FishZone(decoded)
	z.boostPresent = probe.Boost != nil
	return nil
}

func (d *DB) ListFishZones(ctx context.Context) ([]FishZone, error) {
	zones := map[int64]*FishZone{}
	rows, err := d.SQL.QueryContext(ctx, `
		SELECT id, loc, name, kind, COALESCE(sea_zone,0), gx0, gz0, gx1, gz1,
		       mode, boost, keep_others, enabled, updated_by, updated_ts, COALESCE(cells_json,''),
		       cx, cz, radius, COALESCE(sea_zones_json,'')
		FROM fish_zone ORDER BY id`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var z FishZone
		var enabled int
		var cellsJSON string
		var cx, cz, radius sql.NullFloat64
		var seaSetJSON string
		if err := rows.Scan(&z.ID, &z.Loc, &z.Name, &z.Kind, &z.SeaZone,
			&z.GX0, &z.GZ0, &z.GX1, &z.GZ1, &z.Mode, &z.Boost, &z.KeepOthers,
			&enabled, &z.UpdatedBy, &z.UpdatedTs, &cellsJSON, &cx, &cz, &radius, &seaSetJSON); err != nil {
			rows.Close()
			return nil, err
		}
		if z.Kind == "circle" {
			z.Cx, z.Cz, z.Radius = cx.Float64, cz.Float64, radius.Float64
		}
		z.Enabled = enabled != 0
		if cellsJSON != "" && z.Kind == "grid_cells" {
			_ = json.Unmarshal([]byte(cellsJSON), &z.Cells)
		}
		if z.Kind == "sea_zone" {
			if seaSetJSON != "" {
				_ = json.Unmarshal([]byte(seaSetJSON), &z.SeaZones)
			}
			if len(z.SeaZones) == 0 && z.SeaZone > 0 {
				z.SeaZones = []int64{z.SeaZone}
			}
		}
		z.Fish = []FishZoneFish{}
		zones[z.ID] = &z
	}
	rows.Close()
	fishRows, err := d.SQL.QueryContext(ctx, `
		SELECT zone_id, fish_key, weight, enabled FROM fish_zone_fish ORDER BY fish_key`)
	if err != nil {
		return nil, err
	}
	defer fishRows.Close()
	for fishRows.Next() {
		var zoneID int64
		var f FishZoneFish
		var enabled int
		if err := fishRows.Scan(&zoneID, &f.FishKey, &f.Weight, &enabled); err != nil {
			return nil, err
		}
		if z, ok := zones[zoneID]; ok {
			f.Enabled = enabled != 0
			z.Fish = append(z.Fish, f)
		}
	}
	out := make([]FishZone, 0, len(zones))
	for _, z := range zones {
		out = append(out, *z)
	}
	return out, nil
}

// SaveFishZone 整行保存（含名册）；ID 为 0 时新增。
func (d *DB) SaveFishZone(ctx context.Context, z *FishZone, updatedBy string) error {
	// 唯一的缺省归一：boost 字段省略（JSON 不带，落 0）按「无倍增」落 1。
	// 其余越界值一律由 Validate 显式拒绝，不做静默钳制——keep_others=0 是
	// 合同内的合法值（名单外全清），1.5 这类越界必须报错而不是悄悄收 1
	//（2026-11-16 PR #510 评审 P2：与后台 handler 的兜底改写二选一，统一为报错）。
	if z.Boost == 0 && !z.boostPresent {
		z.Boost = 1
	}
	if err := z.Validate(); err != nil {
		return err
	}
	now := float64(time.Now().UnixNano()) / 1e9
	cellsJSON := ""
	if z.Kind == "grid_cells" && len(z.Cells) > 0 {
		if b, err := json.Marshal(z.Cells); err == nil {
			cellsJSON = string(b)
		}
	}
	seaSetJSON := ""
	if z.Kind == "sea_zone" {
		if len(z.SeaZones) == 0 && z.SeaZone > 0 {
			z.SeaZones = []int64{z.SeaZone}
		}
		if len(z.SeaZones) > 0 {
			if b, err := json.Marshal(z.SeaZones); err == nil {
				seaSetJSON = string(b)
			}
			z.SeaZone = z.SeaZones[0]
		}
	}
	tx, err := d.SQL.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if z.ID <= 0 {
		res, err := tx.ExecContext(ctx, `
			INSERT INTO fish_zone(loc,name,kind,sea_zone,gx0,gz0,gx1,gz1,mode,boost,keep_others,enabled,updated_by,updated_ts,cells_json,cx,cz,radius,sea_zones_json)
			VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			z.Loc, z.Name, z.Kind, nullableSeaZone(*z), z.GX0, z.GZ0, z.GX1, z.GZ1,
			z.Mode, z.Boost, z.KeepOthers, boolInt(z.Enabled), updatedBy, now, cellsJSON, nullableCircle(*z, "cx"), nullableCircle(*z, "cz"), nullableCircle(*z, "r"), seaSetJSON)
		if err != nil {
			return err
		}
		z.ID, _ = res.LastInsertId()
	} else {
		if _, err := tx.ExecContext(ctx, `
			UPDATE fish_zone SET loc=?,name=?,kind=?,sea_zone=?,gx0=?,gz0=?,gx1=?,gz1=?,
				mode=?,boost=?,keep_others=?,enabled=?,updated_by=?,updated_ts=?,cells_json=?,cx=?,cz=?,radius=?,sea_zones_json=?
			WHERE id=?`,
			z.Loc, z.Name, z.Kind, nullableSeaZone(*z), z.GX0, z.GZ0, z.GX1, z.GZ1,
			z.Mode, z.Boost, z.KeepOthers, boolInt(z.Enabled), updatedBy, now, cellsJSON,
			nullableCircle(*z, "cx"), nullableCircle(*z, "cz"), nullableCircle(*z, "r"), seaSetJSON, z.ID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM fish_zone_fish WHERE zone_id=?`, z.ID); err != nil {
			return err
		}
	}
	for _, f := range z.Fish {
		if strings.TrimSpace(f.FishKey) == "" {
			continue
		}
		if f.Weight <= 0 {
			f.Weight = 1
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO fish_zone_fish(zone_id,fish_key,weight,enabled) VALUES(?,?,?,?)
			ON CONFLICT(zone_id,fish_key) DO UPDATE SET weight=excluded.weight, enabled=excluded.enabled`,
			z.ID, f.FishKey, f.Weight, boolInt(f.Enabled)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func nullableSeaZone(z FishZone) any {
	if z.Kind != "sea_zone" {
		return nil
	}
	return z.SeaZone
}

// nullableCircle 圆形区三列只在 kind=circle 时写值，其它类型写 NULL 保持语义干净。
func nullableCircle(z FishZone, col string) any {
	if z.Kind != "circle" {
		return nil
	}
	switch col {
	case "cx":
		return z.Cx
	case "cz":
		return z.Cz
	}
	return z.Radius
}

func (d *DB) DeleteFishZone(ctx context.Context, id int64) error {
	if id <= 0 {
		return fmt.Errorf("区域 ID 无效")
	}
	tx, err := d.SQL.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `DELETE FROM fish_zone_fish WHERE zone_id=?`, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM fish_zone WHERE id=?`, id); err != nil {
		return err
	}
	return tx.Commit()
}
