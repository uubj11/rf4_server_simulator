-- 出鱼权重的「基础值」：rf4info 周档统计得出的静态基线。
--
-- fish_weight_override 只有一个值列，后台人工改权重会把它覆盖掉，之后就再也看不出
-- 「周档统计认为这条鱼该占多少」。这里把统计结果单独存一份，后台「基础权重」列显示它，
-- 「实际权重」列显示覆盖值；人工覆盖不会破坏基线。
--
-- 数据来源与生成方式同 00071（rf4info 2026-W32…W46 十五个周档，社区渔获 1.0 / 点位 0.5 /
-- 官方周榜 1.0 加权，每图归一为 100），见 specs/changes/fish-weight-official/spec.md。
CREATE TABLE IF NOT EXISTS fish_weight_base(
    loc        TEXT    NOT NULL,
    fish_key   TEXT    NOT NULL,
    weight     REAL    NOT NULL,
    updated_ts INTEGER NOT NULL DEFAULT 0,
    updated_by TEXT    NOT NULL DEFAULT '',
    PRIMARY KEY(loc, fish_key)
);

-- 首次建立基线：此时 fish_weight_override 刚被 00071 重置为周档统计值。
INSERT OR REPLACE INTO fish_weight_base(loc, fish_key, weight, updated_ts, updated_by)
SELECT loc, fish_key, weight, updated_ts, COALESCE(updated_by, '')
  FROM fish_weight_override
 WHERE updated_by = 'rf4info';
