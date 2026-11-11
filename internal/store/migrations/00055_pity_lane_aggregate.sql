-- 本地服：双轨保底计数按 (uid, loc) 聚合，不再随抛竿落点(spot)重置。
-- 原实现把 trophy_pity/super_pity 放在 fish_hunt_state 的 (uid,loc,spot) 行里，
-- 玩家每换一个落点就新建一行、计数从 0 开始，20 条的周期永远走不完。
CREATE TABLE IF NOT EXISTS fish_hunt_pity(
    uid         INTEGER NOT NULL,
    loc         TEXT    NOT NULL,
    trophy_pity INTEGER NOT NULL DEFAULT 0,
    super_pity  INTEGER NOT NULL DEFAULT 0,
    updated_ts  REAL    NOT NULL DEFAULT 0,
    PRIMARY KEY(uid, loc)
);
