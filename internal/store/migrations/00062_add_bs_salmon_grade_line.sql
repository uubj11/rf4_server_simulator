-- 补录隆头鳟（Black Sea Salmon bs_salmon）的三色标重量线。
-- 00027 建表时（russiabots 2026-10-26 全表 252 条）该鱼尚未进入物种表，
-- russiabots 亦无此鱼条目；2026-11-15 静态库补录 bs_salmon 分布后，
-- TestFishGradeLinesCoverEverySpecies 要求每个鱼种都有三色标线。
--
-- 数值依据：奖杯线 14000g、蓝冠线 20000g 来自官方口径（rf4-stat 鱼种页
-- bs_salmon 及其 справочник 逐项一致，2026-11-15 玩家实测统计 2692 尾）；
-- 达标线 russiabots/RF4.Help 均无该鱼数据，参照同奖杯/蓝冠线的鲑科
-- l_salmon（600/14000/20000，00027）取 600。
INSERT OR IGNORE INTO fish_grade_lines(fish_key, qualified_g, trophy_g, blue_trophy_g, source, checked_at)
VALUES ('bs_salmon', 600, 14000, 20000, 'manual/rf4-stat+l_salmon参照', '2026-11-15');
