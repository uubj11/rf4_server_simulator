-- 玩家自己的渔获点位统计（运行期可写，独立于只读的攻略表 spot_fish）。
--
-- 背景：原实现把入护落点 upsert 进 spot_fish，而 spot_fish 属于 store.StaticTables
-- （运行时只读，读到的是挂在静态库上的同名 TEMP VIEW），staticWriteAuthorizer 对它的
-- DML 一律 AuthorizerDeny，所以每次都返回 SQLITE_AUTH(23)：
--   authorization denied: not authorized
-- 结果「钓点随玩家渔获积累」这条功能自上线起从未生效，每条鱼刷一条 WARN。
--
-- 为什么不直接放开 spot_fish：攻略点位是地图格网口径（样例 67:108、92:65，取值多在
-- 7~206），而客户端上报的是世界坐标（实测 153~601），两套坐标系不能混进同一列，
-- 否则宝典的「钓点参考」会被写脏。所以玩家数据单独落表，只用在同一坐标系的场景：
-- 判鱼时按当前落点半径内的自钓记录，给该鱼种做权重加成。
--
-- 幂等：IF NOT EXISTS，重复执行安全。
CREATE TABLE IF NOT EXISTS spot_catch_player (
  uid        INTEGER NOT NULL,
  loc        TEXT    NOT NULL,
  x          REAL    NOT NULL,
  z          REAL    NOT NULL,
  fish_key   TEXT    NOT NULL,
  catches    INTEGER NOT NULL DEFAULT 0,
  updated_ts REAL    NOT NULL DEFAULT 0,
  PRIMARY KEY (uid, loc, x, z, fish_key)
);

CREATE INDEX IF NOT EXISTS idx_spot_catch_player_uid_loc ON spot_catch_player(uid, loc);
