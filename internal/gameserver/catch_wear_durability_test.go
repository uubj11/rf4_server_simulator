package gameserver

import (
	"math"
	"testing"

	"rf4server/internal/proto"
)

// 成长套装件满耐久时钓小鱼：磨损 0.0012 被回复 0.0027 盖过、再钳回 1.0，
// 算出来与当前值完全相同。setWireFloat 对「值没变」返回 false，
// 若把它当成写入失败，14/4 结算就会整个丢弃，客户端弹「发生内部错误」。
func TestFullDurabilityGrowthCatchKeepsSettlement(t *testing.T) {
	after := growthDurabilityAfterCatch(1.0, wearPerCatch["hook"], 1.335, true)
	if after != 1.0 {
		t.Fatalf("满耐久成长件应保持 1.0，得到 %v", after)
	}
	if durabilityChanged(1.0, after) {
		t.Fatalf("1.0 → %v 不该算变化", after)
	}
	if !durabilityChanged(0.9, 0.89) {
		t.Fatalf("真实磨损必须被判为变化")
	}

	// 复核底层语义：值相同就返回 false，这条路径不能当失败判据。
	var idx int = fieldIdxByCS("dgjinoebdop", csDurability)
	if idx < 0 {
		t.Skip("该类的耐久字段不在线格式里")
	}
	fields := make([]proto.Field, 64)
	switch fieldOpAt("dgjinoebdop", idx) {
	case "U32":
		// 耐久在这类线格式里是 float32 位模式存 U32
		fields[idx].Num = ptrU64(uint64(math.Float32bits(0.75)))
	case "FLOAT", "DOUBLE":
		fields[idx].Float = 0.75
	default:
		t.Skipf("耐久字段 op 不可写: %s", fieldOpAt("dgjinoebdop", idx))
	}
	if setWireFloat(fields, "dgjinoebdop", idx, 0.75) {
		t.Fatalf("写入相同值不该返回 true")
	}
	if !setWireFloat(fields, "dgjinoebdop", idx, 0.5) {
		t.Fatalf("写入不同值必须返回 true")
	}
}
