package gameserver

import (
	"math"
	"testing"

	"rf4server/internal/store"
)

// 真实库里的渔轮快照只有 13 个槽：guid / optguid / byte / u32×3 / u64×2 / u32 /
// val×2 / u32 / u16 —— 正好是线格式尾部 13 个（前面 15 个装配槽由 materializeInto
// 运行时物化、不落库）。耐久槽 idx=20 因此不在快照里。
//
// 这种「快照不含此槽」不是写入失败：耐久由 item.durability 列承载，
// 4/5 重建以列为准（见 persistItemDurability 的注释）。把它当失败，
// 14/4 结算会整条丢弃 —— 客户端弹「发生内部错误」，鱼被判定跑掉。
const sparseReelFieldsJSON = `[{"f":"guid","g":"81e3e0d97583cf36f23d97a86c7b9186"},{"f":"optguid","v":"b0c505be2c850041bdb413313407ad81"},{"f":"byte","v":4},{"f":"u32","v":657},{"f":"u32","v":1065353216},{"f":"u32","v":1065352959},{"f":"u64","v":0},{"f":"u64","v":0},{"f":"u32","v":17920},{"f":"val","v":{"t":"null"}},{"f":"val","v":{"t":"null"}},{"f":"u32","v":0},{"f":"u16","v":0}]`

func TestSparseReelSnapshotKeepsCatchSettlement(t *testing.T) {
	row := &store.Item{
		GUID:        "b0c505be2c850041bdb413313407ad81",
		UID:         1000001,
		ItemID:      "spin_5140_sbtn",
		NumericID:   13630,
		WireClass:   "dgjinoebdop",
		TypeID:      149,
		Durability:  0.9999756424208925,
		Workmanship: 1,
		FieldsJSON:  sparseReelFieldsJSON,
	}

	fields, err := FieldsFromJSON(row.FieldsJSON)
	if err != nil {
		t.Fatalf("解析真实快照失败: %v", err)
	}
	if len(fields) != 13 {
		t.Fatalf("真实快照应为 13 槽，实得 %d", len(fields))
	}
	if idx := fieldIdxByCS(row.WireClass, csDurability); idx != 20 {
		t.Fatalf("渔轮耐久槽应为 idx=20，实得 %d", idx)
	}

	// 20.893kg 哲罗鱼：回复量盖过磨损，耐久被钳回 1.0，与当前值差 2.4e-5。
	want := growthDurabilityAfterCatch(row.Durability, wearPerCatch["default"], 20.893, true)
	if !durabilityChanged(row.Durability, want) {
		t.Fatalf("这条用例必须触发一次写入（before=%v want=%v）", row.Durability, want)
	}

	cw, err := catchWearForItem(row, "default", 20.893, true, 6, 0)
	if err != nil {
		t.Fatalf("快照不含耐久槽不该让整条结算失败: %v", err)
	}
	if cw == nil {
		t.Fatal("应返回磨损快照")
	}
	if math.Abs(cw.durability-want) > 1e-9 {
		t.Fatalf("耐久应为 %v，实得 %v", want, cw.durability)
	}
	if cw.fields != row.FieldsJSON {
		t.Fatalf("快照没有耐久槽时不该改写字段，原样落库即可")
	}
}
