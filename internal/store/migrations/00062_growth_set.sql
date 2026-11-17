-- 成长套装：等级存在「套装」上，单件只记归属与角色。
--
-- growth_set_state：一个玩家一套一个等级，套装内所有件共用同一等级；
-- growth_item：哪些物品实例属于哪一套、在该套里扮演什么角色（rod/reel/line/hook/leader/swivel/float）。
-- 单独建表而不给 item 加列，避免改动物品主表带来的迁移风险。
-- 幂等：IF NOT EXISTS，重复执行安全。

CREATE TABLE IF NOT EXISTS growth_set_state (
  uid        INTEGER NOT NULL,
  set_key    TEXT    NOT NULL,
  level      INTEGER NOT NULL DEFAULT 0,
  xp         INTEGER NOT NULL DEFAULT 0,
  updated_ts INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (uid, set_key)
);

CREATE TABLE IF NOT EXISTS growth_item (
  guid       TEXT PRIMARY KEY,
  uid        INTEGER NOT NULL,
  set_key    TEXT    NOT NULL,
  role       TEXT    NOT NULL,
  created_ts INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_growth_item_uid ON growth_item(uid, set_key);
