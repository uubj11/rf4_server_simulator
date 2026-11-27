package gameserver

import (
	"math"
	"testing"

	"rf4server/internal/proto"
	"rf4server/internal/store"
	"rf4server/internal/wiregen"
)

// 耐久有两份存储：item.durability 列 与 fields_json 里的耐久槽。
// 有路径只写列（如 wearRigForCatch 的"更新该列即可同步字段"），于是一段时间后
// 两者会差出零点几个千分位 —— 实测 b0c505be 列 0.999948、字段 idx20 已是 1.000000。
//
// 此时钓一条 3.319kg 的鱼，成长件耐久算回来钳到 1.0：列确实变了（0.999948→1.0），
// 而字段本来就是 1.0，setWireFloat 对"值相同"返回 false —— 若拿它当失败，
// 整条 14/4 结算又被丢掉，客户端弹内部错误、鱼判跑掉。
//
// 判据必须是"槽存在且 op 可写"，而不是"列变了所以字段也必须变"。
func TestDriftedDurabilityColumnKeepsCatchSettlement(t *testing.T) {
	idx := fieldIdxByCS("dgjinoebdop", csDurability)
	if idx < 0 {
		t.Skip("该类没有耐久槽")
	}
	if op := fieldOpAt("dgjinoebdop", idx); op != "U32" {
		t.Skipf("耐久槽 op 非 U32: %s", op)
	}

	fields := make([]proto.Field, len(wiregen.ClassOps["dgjinoebdop"]))
	fields[idx].Num = ptrU64(uint64(math.Float32bits(1.0)))
	fj, err := FieldsToJSON(fields)
	if err != nil {
		t.Fatalf("构造快照失败: %v", err)
	}

	row := &store.Item{
		GUID:       "b0c505be2c850041bdb413313407ad81",
		UID:        1000001,
		ItemID:     "spin_5140_sbtn",
		WireClass:  "dgjinoebdop",
		TypeID:     149,
		Durability: 0.999948, // 列落后于字段
		FieldsJSON: fj,
	}
	if len(fields) <= idx {
		t.Fatalf("构造的快照长度 %d 不含耐久槽 %d", len(fields), idx)
	}

	want := growthDurabilityAfterCatch(row.Durability, wearPerCatch["default"], 3.319, true)
	if math.Abs(want-1.0) > 1e-9 {
		t.Fatalf("这条用例应把耐久回复到 1.0，实得 %v", want)
	}
	if !durabilityChanged(row.Durability, want) {
		t.Fatalf("列确实变了，用例前提不成立")
	}

	cw, err := catchWearForItem(row, "default", 3.319, true, false, 6, 0)
	if err != nil {
		t.Fatalf("列与字段漂移不该让整条结算失败: %v", err)
	}
	if cw == nil {
		t.Fatal("应返回磨损快照")
	}
	if math.Abs(cw.durability-want) > 1e-9 {
		t.Fatalf("耐久应为 %v，实得 %v", want, cw.durability)
	}
}
