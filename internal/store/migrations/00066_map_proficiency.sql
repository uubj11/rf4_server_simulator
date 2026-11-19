-- 每张地图的钓鱼熟练度（本地 mod，0.9.58）。
--
-- 口径：按 (玩家, 地图) 累计「钓到过」的尾数，尾数跨过 map_prof_steps 的某一档
-- 就升一级；等级本身只由尾数派生，不单独存储，避免两处账本对不上。
--
-- key 用 level_xxx 这种地图键（f.LocLevel / statLoc 都是这一套），与 catch_log.loc_level
-- 同源；中文地图名只在播报时经 loc_alias 查回来。
CREATE TABLE IF NOT EXISTS map_proficiency (
  uid INTEGER NOT NULL,
  loc TEXT NOT NULL,
  catches INTEGER NOT NULL DEFAULT 0,
  last_ts REAL NOT NULL DEFAULT 0,
  PRIMARY KEY (uid, loc)
);

CREATE INDEX IF NOT EXISTS idx_map_proficiency_uid ON map_proficiency(uid, loc);
