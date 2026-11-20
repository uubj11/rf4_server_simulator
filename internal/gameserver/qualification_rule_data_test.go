package gameserver

import (
	"encoding/hex"
	"reflect"
	"testing"

	"rf4server/internal/proto"
	"rf4server/internal/wiregen"
)

func TestPermanentQualificationWireRulesComplete(t *testing.T) {
	if len(qualificationRuleWireByID) != 168 {
		t.Fatalf("永久资格静态规则数=%d，期望 168", len(qualificationRuleWireByID))
	}
	seen := make(map[uint64]bool, 168)
	for branch := uint64(1); branch <= qualificationBranches; branch++ {
		for stage := uint64(1); stage <= qualificationStages; stage++ {
			for task := uint64(1); task <= qualificationTasks; task++ {
				id := qualificationRuleID(branch, stage, task)
				if _, ok := qualificationRuleWireByID[id]; !ok {
					t.Fatalf("缺少永久资格规则 %d", id)
				}
				seen[id] = true
			}
		}
	}
	for id := range qualificationRuleWireByID {
		if !seen[id] {
			t.Fatalf("静态表含有阶梯外规则 %d", id)
		}
	}
}

func TestPermanentQualificationWireRulesRoundTrip(t *testing.T) {
	for id, want := range qualificationRuleWireByID {
		body, err := proto.EncodeValue(qualificationRule(id), wiregen.Table())
		if err != nil {
			t.Fatalf("规则 %d 编码失败: %v", id, err)
		}
		got, end, err := proto.DecodeValue(body, 0, wiregen.Table())
		if err != nil {
			t.Fatalf("规则 %d 解码失败: %v", id, err)
		}
		if end != len(body) || got.TypeID != 5007 || len(got.Fields) != 28 {
			t.Fatalf("规则 %d 形状错误: end=%d/%d type=%d fields=%d", id, end, len(body), got.TypeID, len(got.Fields))
		}

		wantScalars := map[int]uint64{
			0: id, 1: uint64(want.F1), 2: uint64(want.F2), 3: uint64(want.F3), 4: uint64(want.F4),
			14: uint64(want.F14), 15: uint64(want.F15), 18: want.F18,
			20: uint64(want.F20), 21: uint64(want.F21), 23: uint64(want.F23), 24: uint64(want.F24),
			25: uint64(want.F25), 26: uint64(want.F26), 27: uint64(want.F27),
		}
		for idx, value := range wantScalars {
			if got.Fields[idx].Num == nil || *got.Fields[idx].Num != value {
				t.Fatalf("规则 %d 字段 %d=%v，期望 %d", id, idx, got.Fields[idx].Num, value)
			}
		}

		wantEnums := map[int][]uint32{
			5: want.F5, 6: want.F6, 7: want.F7, 8: want.F8, 9: want.F9,
			10: want.F10, 11: want.F11, 12: want.F12, 13: want.F13, 22: want.F22,
		}
		for idx, values := range wantEnums {
			field := got.Fields[idx].Val
			if field == nil || field.Kind != proto.KindU32Arr {
				t.Fatalf("规则 %d 字段 %d 应为非 null u32 数组，实得 %v", id, idx, field)
			}
			actual := make([]uint32, 0, len(field.Items))
			for _, item := range field.Items {
				actual = append(actual, uint32(item.Num))
			}
			if len(values) == 0 {
				values = []uint32{}
			}
			if !reflect.DeepEqual(actual, values) {
				t.Fatalf("规则 %d 字段 %d=%v，期望 %v", id, idx, actual, values)
			}
		}

		wantStrings := map[int][]string{16: want.F16, 17: want.F17, 19: want.F19}
		for idx, values := range wantStrings {
			field := got.Fields[idx].Val
			if field == nil || field.Kind != proto.KindArray {
				t.Fatalf("规则 %d 字段 %d 应为非 null 字符串数组，实得 %v", id, idx, field)
			}
			actual := make([]string, 0, len(field.Items))
			for _, item := range field.Items {
				raw, err := hex.DecodeString(item.Hex)
				if err != nil {
					t.Fatalf("规则 %d 字段 %d 字符串解码失败: %v", id, idx, err)
				}
				actual = append(actual, string(raw))
			}
			if len(values) == 0 {
				values = []string{}
			}
			if !reflect.DeepEqual(actual, values) {
				t.Fatalf("规则 %d 字段 %d=%v，期望 %v", id, idx, actual, values)
			}
		}
	}
}

func TestPermanentQualificationOfficialTargetAnchors(t *testing.T) {
	checks := map[uint64]struct {
		target uint32
		rigs   []uint32
	}{
		6502: {target: 5, rigs: []uint32{12, 13, 14, 15, 17}},
		7502: {target: 9, rigs: []uint32{24, 25, 26, 27, 28, 29, 16, 32, 34}},
	}
	for id, check := range checks {
		rule := qualificationRuleWireByID[id]
		if rule.F3 != check.target || !reflect.DeepEqual(rule.F9, check.rigs) {
			t.Fatalf("规则 %d 目标/钓组=%d/%v，期望 %d/%v", id, rule.F3, rule.F9, check.target, check.rigs)
		}
	}
	ghost := qualificationRuleWireByID[7401]
	if ghost.F3 != 5 || !reflect.DeepEqual(ghost.F16, []string{"n_carp_ghost", "m_carp_ghost", "l_carp_ghost", "carp_ghost", "r_carp_ghost"}) {
		t.Fatalf("规则 7401 目标/鱼种=%d/%v", ghost.F3, ghost.F16)
	}
}
