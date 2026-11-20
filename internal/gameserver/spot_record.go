package gameserver

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"time"
)

// spotRecordMaxCoord 是落点坐标的合理上限。客户端上报的是每图小地图坐标
// （实测最大 668，挪威海），留一档余量，防异常值污染点位表。
const spotRecordMaxCoord = 5000.0

// spotCoordKey 把抛竿落点压成"横:纵"点位串（只用于日志与排障展示）。
// 协议 olieodeoaof 的 bopigefkkom / ceicchfhpld 就是客户端自己的位置字段
// （位姿同步帧族同族字段，实测 bopigefkkom [-141.744, 281.084]、
// ceicchfhpld [-60.5645, 425.429]），与攻略库的地图格网不是同一套口径。
func spotCoordKey(x, z float64) string {
	return fmt.Sprintf("%d:%d", int64(math.Round(x)), int64(math.Round(z)))
}

// spotHUDKey 把客户端上报的场景相对世界坐标换算成游戏地图界面上显示的
// 两三位数 HUD 坐标，口径与客户端一致：（世界坐标 − 场景原点）/ 5，按最近偶数取整
// （见 docs/fish-query-coordinates.md、fishQueryMapFrame.project）。
// 未登记标定的地图返回空串，调用方回退到原始坐标。
func spotHUDKey(levelKey string, x, z float64) string {
	scene, ok := fishQueryRegionScenes[levelKey]
	if !ok || !validFishQueryCoordinate(scene.OriginX) || !validFishQueryCoordinate(scene.OriginZ) {
		return ""
	}
	frame := fishQueryMapFrame{originX: scene.OriginX, originZ: scene.OriginZ}
	mx, mz, ok := frame.project(x, z)
	if !ok {
		return ""
	}
	hx := int64(math.RoundToEven(mx))
	hz := int64(math.RoundToEven(mz))
	return fmt.Sprintf("%d:%d", hx, hz)
}

// 玩家自钓点位的三个 tunable：加权开关、半径（世界坐标单位）、宝典里是否并列显示。
const (
	tunableSpotPlayerBias     = "spot_player_bias"
	tunableSpotPlayerRadius   = "spot_player_radius"
	tunableSpotShowPlayerRefs = "spot_show_player_refs"
	defSpotPlayerRadius       = 20.0
)

// recordCatchSpot 把一次真实入护的（玩家·地图·落点·鱼种）累加进 spot_catch_player。
//
// 为什么不能写 spot_fish：spot_fish 属于 store.StaticTables，运行时只读挂载，
// staticWriteAuthorizer 对静态表的 DML 一律拒绝，写进去只会拿到
// SQLITE_AUTH(23) / "authorization denied"，功能不会生效（2026-11-18 前每条鱼
// 刷一条 WARN 即由此而来）。另外攻略点位是攻略作者的地图格网口径，与客户端
// 上报的位置坐标不是同一套，混在同列会写脏宝典的钓点参考，所以玩家数据单独落表。
//
// 拿不到有效落点、地图名未登记或开关关闭时静默跳过——钓点只是提示，
// 任何异常都不该影响入护主流程。
func (s *Server) recordCatchSpot(ctx context.Context, uid int64, locLevel string, x, z float64, hasPos bool, fishKey string) {
	if s == nil || s.DB == nil || uid <= 0 || !hasPos || locLevel == "" || fishKey == "" {
		return
	}
	if s.tunableF("spot_record", 1) == 0 {
		return
	}
	if math.IsNaN(x) || math.IsNaN(z) || math.IsInf(x, 0) || math.IsInf(z, 0) {
		return
	}
	if x <= 0 || z <= 0 || x > spotRecordMaxCoord || z > spotRecordMaxCoord {
		return
	}
	locName, err := s.DB.LocDisplayName(ctx, locLevel)
	if err != nil || locName == "" {
		return
	}
	// 取整落格，避免同一点位因浮点抖动拆成多行。
	gx := math.Round(x)
	gz := math.Round(z)
	// 挪威海没有"客户端位置坐标"口径的攻略点位，攻略用的是海图区号；入护时就换算好
	// 存下来，判鱼侧才能和攻略侧同口径比对。内陆湖没有海图，这里留空串。
	zone := ""
	levelKey := s.resolveRollLevelKey(ctx, locLevel)
	if sonarChartLevel(levelKey) {
		if n, ok := sonarZoneForPoint(levelKey, x, z); ok {
			zone = strconv.Itoa(n)
		}
	}
	// HUD 坐标：游戏地图界面上显示的那套读数（（世界 − 原点）/5 取最近偶数）。
	// 存下来后宝典可以把玩家点位与攻略点位放在同一套坐标上，玩家不用自己换算。
	hud := spotHUDKey(levelKey, x, z)
	if _, err := s.DB.SQL.ExecContext(ctx, `
		INSERT INTO spot_catch_player(uid, loc, x, z, fish_key, spot, hud, catches, updated_ts)
		VALUES(?,?,?,?,?,?,?,1,?)
		ON CONFLICT(uid, loc, x, z, fish_key) DO UPDATE SET
			catches = catches + 1,
			spot = CASE WHEN excluded.spot <> '' THEN excluded.spot ELSE spot END,
			hud  = CASE WHEN excluded.hud  <> '' THEN excluded.hud  ELSE hud  END,
			updated_ts = excluded.updated_ts`,
		uid, locName, gx, gz, fishKey, zone, hud, float64(time.Now().Unix())); err != nil {
		// 只留调试级痕迹：钓点加权是可选的锦上添花，不该在正常渔获流里刷 WARN。
		s.log.Debug("钓点自钓记录失败", "loc", locLevel, "spot", spotCoordKey(gx, gz), "fish", fishKey, "err", err)
	}
}

// loadPlayerSpotPreference 读取"玩家自己在这个落点附近钓到过哪些鱼"。
//
// 与 loadFishSpotPreference 的区别：攻略点位只对挪威海的海图区号有效（内陆湖的
// 格网坐标换不成客户端落点）；而玩家自钓记录本身就是客户端位置坐标，所以
// 内陆湖同样可用——这正是原来"入护记录钓点"想达到的效果。
//
// 命中范围由两个口径共同决定：
//   - 挪威海：入护时已换算好的海图区号（spot 列）与当前落点区号比对，与攻略侧同口径；
//   - 内陆湖：按世界坐标半径（spot_player_radius）命中。
//
// 权重一律用 sqrt(次数)，与攻略侧同口径。
func (s *Server) loadPlayerSpotPreference(ctx context.Context, c *fishRollContext, statLoc string) (map[string]float64, bool) {
	if s == nil || s.DB == nil || c == nil || statLoc == "" || !c.hasPos || c.uid <= 0 {
		return nil, false
	}
	if s.tunableF(tunableSpotPlayerBias, 1) == 0 {
		return nil, false
	}
	out := map[string]float64{}
	if c.runtime != nil && c.runtime.chartKnown && c.runtime.zoneKnown {
		rows, err := s.DB.SQL.QueryContext(ctx, `
			SELECT fish_key, SUM(catches)
			FROM spot_catch_player
			WHERE uid=? AND loc=? AND spot=?
			GROUP BY fish_key`,
			c.uid, statLoc, strconv.Itoa(c.runtime.zone))
		if err == nil {
			for rows.Next() {
				var fish string
				var catches int64
				if rows.Scan(&fish, &catches) != nil || fish == "" || catches <= 0 {
					continue
				}
				out[fish] += math.Sqrt(float64(catches))
			}
			rows.Close()
		}
	}
	radius := s.tunableF(tunableSpotPlayerRadius, defSpotPlayerRadius)
	// 海图地图已经按区号命中过了，不再叠半径，避免同一批记录被计两遍。
	if radius > 0 && !(c.runtime != nil && c.runtime.chartKnown && c.runtime.zoneKnown) {
		rows, err := s.DB.SQL.QueryContext(ctx, `
			SELECT fish_key, SUM(catches)
			FROM spot_catch_player
			WHERE uid=? AND loc=?
			  AND ABS(x-?) <= ? AND ABS(z-?) <= ?
			GROUP BY fish_key`,
			c.uid, statLoc, c.x, radius, c.z, radius)
		if err == nil {
			for rows.Next() {
				var fish string
				var catches int64
				if rows.Scan(&fish, &catches) != nil || fish == "" || catches <= 0 {
					continue
				}
				out[fish] += math.Sqrt(float64(catches))
			}
			rows.Close()
		}
	}
	return out, len(out) > 0
}

// spotReferences 取某图某鱼的钓点参考。
//
// 默认只给攻略点位（spot_fish）。打开 spot_show_player_refs 后，把玩家自己在
// 该图该鱼的点位（客户端位置坐标口径）一并并列进来，用于"按游戏里显示的坐标
// 去找自己钓过的点"。两套坐标口径不同，所以必须由开关显式打开，不能默认混排。
func (s *Server) spotReferences(ctx context.Context, statLoc, fish string, uid int64) ([]FishQueryReference, error) {
	var (
		query string
		args  []any
	)
	if s.tunableF(tunableSpotShowPlayerRefs, 0) == 1 && uid > 0 {
		query = `
			SELECT spot, SUM(catches) FROM (
				SELECT spot, catches FROM spot_fish WHERE loc=? AND fish_key=?
				UNION ALL
				SELECT COALESCE(NULLIF(hud,''), printf('%d:%d', x, z)) AS spot, catches
				FROM spot_catch_player WHERE uid=? AND loc=? AND fish_key=?
			)
			GROUP BY spot ORDER BY SUM(catches) DESC, spot LIMIT 6`
		args = []any{statLoc, fish, uid, statLoc, fish}
	} else {
		query = `SELECT spot,SUM(catches) FROM spot_fish WHERE loc=? AND fish_key=?
			GROUP BY spot ORDER BY SUM(catches) DESC,spot LIMIT 6`
		args = []any{statLoc, fish}
	}
	rows, err := s.DB.SQL.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []FishQueryReference
	for rows.Next() {
		var r FishQueryReference
		if err := rows.Scan(&r.Coordinate, &r.Catches); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
