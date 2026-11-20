package admin

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"rf4server/internal/store"
)

// 区域固定鱼种名册的后台读写。判鱼侧由 gameserver 的 fish_zone.go 消费，
// 保存后经管理队列推 reload_fish_zones 主动失效缓存（另有 30 秒 TTL 兜底）。

const maxFishZoneFish = 120

// handleFishZones GET：全量区域列表（含名册）。
func (s *Server) handleFishZones(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	zones, err := s.db.ListFishZones(ctx)
	if err != nil {
		httpError(w, 500, err.Error())
		return
	}
	writeJSON(w, map[string]any{"zones": zones})
}

// handleFishZoneSave POST：整行保存（id=0 新增）。body 即 store.FishZone。
func (s *Server) handleFishZoneSave(w http.ResponseWriter, r *http.Request) {
	var z store.FishZone
	if err := readJSON(r, &z); err != nil {
		httpError(w, 400, "参数错误")
		return
	}
	z.Loc = strings.TrimSpace(z.Loc)
	z.Name = strings.TrimSpace(z.Name)
	// 挪威海不做区域划分（2026-11-14 用户决定）：海图区号/坑名与判鱼信号易引起误解，
	// 硬拒绝保存，UI 同步隐藏入口。
	if z.Kind == "sea_zone" {
		httpError(w, 400, "挪威海不启用区域划分——按全图名册出鱼，无需配置")
		return
	}
	if len([]rune(z.Name)) > 60 {
		httpError(w, 400, "区域名过长")
		return
	}
	if len(z.Fish) > maxFishZoneFish {
		httpError(w, 400, "名册最多 "+strconv.Itoa(maxFishZoneFish)+" 个鱼种")
		return
	}
	// 网格矩形标准化：允许端点乱序输入，保存前翻正。
	if z.GX0 > z.GX1 {
		z.GX0, z.GX1 = z.GX1, z.GX0
	}
	if z.GZ0 > z.GZ1 {
		z.GZ0, z.GZ1 = z.GZ1, z.GZ0
	}
	// keep_others 不在此兜底改写（2026-11-16 PR #510 评审 P2）：boost 模式下
	// 0 是合同内的合法值（名单外全清），静默改 0.35 会让玩家配置失真；
	// 越界值由 store.Validate 显式拒绝并回 400 提示。
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	// 拼错鱼种会得到「永不命中的死区」且无任何报错提示，逐个核对全库名册。
	for _, f := range z.Fish {
		var n int
		if err := s.db.SQL.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM species_weight WHERE fish_key=?`, f.FishKey).Scan(&n); err != nil {
			httpError(w, 500, err.Error())
			return
		}
		if n == 0 {
			httpError(w, 400, "未知鱼种: "+f.FishKey)
			return
		}
	}
	if err := s.db.SaveFishZone(ctx, &z, adminOf(r)); err != nil {
		httpError(w, 400, err.Error())
		return
	}
	_ = s.db.AuditLogWrite(ctx, adminOf(r), "fish_zone_save", z.Loc,
		"zone="+strconv.FormatInt(z.ID, 10)+" kind="+z.Kind+" mode="+z.Mode+
			" fish="+strconv.Itoa(len(z.Fish))+" enabled="+strconv.FormatBool(z.Enabled))
	_ = s.db.QueuePush(ctx, mustJSON(map[string]any{"op": "reload_fish_zones"}))
	writeJSON(w, map[string]any{"ok": true, "id": z.ID})
}

// handleFishZoneDelete POST：body {"id":N}。
func (s *Server) handleFishZoneDelete(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID int64 `json:"id"`
	}
	if err := readJSON(r, &req); err != nil {
		httpError(w, 400, "参数错误")
		return
	}
	if req.ID <= 0 {
		httpError(w, 400, "区域 ID 无效")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	if err := s.db.DeleteFishZone(ctx, req.ID); err != nil {
		httpError(w, 500, err.Error())
		return
	}
	_ = s.db.AuditLogWrite(ctx, adminOf(r), "fish_zone_delete", strconv.FormatInt(req.ID, 10), "")
	_ = s.db.QueuePush(ctx, mustJSON(map[string]any{"op": "reload_fish_zones"}))
	writeJSON(w, map[string]any{"ok": true})
}
