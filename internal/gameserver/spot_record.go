package gameserver

import (
	"context"
	"fmt"
	"math"
)

// spotRecordMaxCoord 是落点坐标的合理上限。客户端上报的是每图小地图坐标
// （实测最大 668，挪威海），留一档余量，防异常值污染点位表。
const spotRecordMaxCoord = 5000.0

// spotCoordKey 把抛竿落点压成 spot_fish 认的"横:纵"点位串。
// 协议 olieodeoaof 的 bopigefkkom / ceicchfhpld 就是小地图坐标（实测
// bopigefkkom [-141.744, 281.084]、ceicchfhpld [-60.5645, 425.429]），
// 与 spot_fish 既有数据同为整数格，四舍五入即可对齐。
func spotCoordKey(x, z float64) string {
	return fmt.Sprintf("%d:%d", int64(math.Round(x)), int64(math.Round(z)))
}

// recordCatchSpot 把一次真实入护的（地图·点位·鱼种）累加进 spot_fish，
// 让钓点数据随玩家自己的渔获积累。拿不到有效落点、地图名未登记或开关
// 关闭时静默跳过——钓点只是提示，任何异常都不该影响入护主流程。
func (s *Server) recordCatchSpot(ctx context.Context, locLevel string, x, z float64, hasPos bool, fishKey string) {
	if s == nil || s.DB == nil || !hasPos || locLevel == "" || fishKey == "" {
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
	key := spotCoordKey(x, z)
	if _, err := s.DB.SQL.ExecContext(ctx, `
		INSERT INTO spot_fish(loc, spot, fish_key, catches) VALUES(?,?,?,1)
		ON CONFLICT(loc, spot, fish_key) DO UPDATE SET catches = catches + 1`,
		locName, key, fishKey); err != nil {
		s.log.Warn("钓点记录失败", "loc", locLevel, "spot", key, "fish", fishKey, "err", err)
	}
}
