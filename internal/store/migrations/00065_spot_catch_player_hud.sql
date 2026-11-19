-- 给玩家自钓点位表补一列「HUD 地图坐标」（游戏里显示的两三位数格网）。
--
-- 原因：客户端上报的落点是「场景相对世界坐标」，而攻略库和游戏地图界面用的是
-- HUD 坐标，换算口径是（世界坐标 − 场景原点）/ 5 再按最近偶数取整
-- （见 docs/fish-query-coordinates.md 与 fish_query_coordinates.go）。
-- 入护时按该图原点换算好一起存，宝典里就能把玩家点位与攻略点位放进同一套坐标，
-- 不需要玩家自己去比对或手测。
--
-- 未登记标定的地图留空串，读取端回退到原始世界坐标。
ALTER TABLE spot_catch_player ADD COLUMN hud TEXT NOT NULL DEFAULT '';
