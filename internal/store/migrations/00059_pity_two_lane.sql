-- 本地服新增：奖杯 / 超稀有两条独立保底轨道。
-- 与原版 pity 分开计数，避免互相覆盖：
--   trophy_pity：每钓一条普通鱼 +1；出 3 星或 4 星归零。
--   super_pity ：每钓一条普通鱼 +1；出 3 星 +2；出 4 星归零。
-- 用权重放大倍数做爬坡，不改动名册与昼夜门。
ALTER TABLE fish_hunt_state ADD COLUMN trophy_pity INTEGER NOT NULL DEFAULT 0;
ALTER TABLE fish_hunt_state ADD COLUMN super_pity INTEGER NOT NULL DEFAULT 0;
