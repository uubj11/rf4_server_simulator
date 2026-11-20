-- 给玩家自钓点位表补一列「海图区号」。
--
-- 原因：挪威海的攻略点位不是世界坐标，而是海图区号（"215"、"120:3"），
-- 判鱼侧 loadFishSpotPreference 就是拿区号和当前落点比对。玩家自钓记录存的是
-- 世界坐标，读的时候无法直接跟区号比，所以在入护时就用 sonarZoneForPoint
-- 把落点换算成区号一起存下来（换算只由静态海图资源决定，同一坐标结果稳定）。
--
-- 内陆湖没有海图，sonarZoneForPoint 返回 false，这列留空串，走半径命中。
-- 幂等：ALTER 语句重复执行会报 duplicate column，用 PRAGMA 无法判断，
-- 因此迁移由 schema_migrations 保证只跑一次。
ALTER TABLE spot_catch_player ADD COLUMN spot TEXT NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS idx_spot_catch_player_zone ON spot_catch_player(uid, loc, spot);
