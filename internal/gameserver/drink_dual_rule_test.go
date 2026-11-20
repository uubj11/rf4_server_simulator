package gameserver

import "testing"

// 本服口径：热红酒既是酒（经验加成）又是热饮（舒适度恢复），两套规则必须同时命中。
// 上游 0.9.65 合并时改成 `if !isDrink` 只走酒规则，暖身效果丢失，此用例防回退。
func TestHotWineUsesBothDrinkAndHotDrinkRules(t *testing.T) {
	if _, isDrink := drinkRuleForItem("thermos_gwein"); !isDrink {
		t.Fatal("热红酒必须命中酒类规则（经验加成）")
	}
	kind, isConsumable := consumableRestoreForItem("thermos_gwein")
	if !isConsumable || kind != "hot_wine" {
		t.Fatalf("热红酒必须同时命中热饮恢复规则，实得 kind=%q ok=%v", kind, isConsumable)
	}
}
