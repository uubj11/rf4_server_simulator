-- 昼夜曲线：四段权重（夜 0-5/20-24、晨 5-9、昼 9-16、昏 16-20），空串=只用 diel_mask。
ALTER TABLE fish_ecology ADD COLUMN diel_curve TEXT NOT NULL DEFAULT '';
