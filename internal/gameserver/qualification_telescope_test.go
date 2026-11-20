package gameserver

import (
	"context"
	"strconv"
	"testing"

	"rf4server/internal/logging"
	"rf4server/internal/proto"
	"rf4server/internal/wiregen"
)

func TestQualificationTelescopicRuleMetadata(t *testing.T) {
	for _, id := range []uint64{4303, 4602} {
		body, err := proto.EncodeValue(qualificationRule(id), wiregen.Table())
		if err != nil {
			t.Fatal(err)
		}
		rule, _, err := proto.DecodeValue(body, 0, wiregen.Table())
		if err != nil {
			t.Fatal(err)
		}
		if got := rule.Fields[5].Val.Items; len(got) != 1 || got[0].U32Val() != 3 {
			t.Fatalf("%d 应限定伸缩竿 [3]，实得 %v", id, got)
		}
		if id == 4303 {
			if got := rule.Fields[6].Val.Items; len(got) != 1 || got[0].U32Val() != 1 {
				t.Fatalf("4303 硬度应为 [1]，实得 %v", got)
			}
		}
		if id == 4602 && *rule.Fields[4].Num != 0 {
			t.Fatalf("4602 静态规则不应额外限定钓组类别，实得 %d", *rule.Fields[4].Num)
		}
	}
}

func TestQualificationFloatBranchRejectsOtherRigs(t *testing.T) {
	srv, db := newDetachTestServer(t)
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}
	sess := newProbeSess(srv)
	ctx := &Ctx{Sess: sess, Log: logging.Discard()}
	// 路亚和海洋钓组都不能推进手竿任务。
	for _, rod := range []int64{1, 2, 3, 4, 5, 9, 10, 11, 12, 15, 16, 0} {
		rig := int64(6)
		if rod == 15 || rod == 16 {
			rig = 50
		}
		ev := qualificationEvent{
			Kind: qevFishLanded, CatchGUID: "non-tele-" + strconv.FormatInt(rod, 10),
			Species: "meagre", WeightG: 7000, Grade: 3, XP: 7000,
			RodType: rod, RigType: rig, LocLevel: evLocNorthSea,
			Bait: "worm_01", BaitCat: 27, BaitClass: 2,
			RodTestMax: 5, MainLine: 1, LineStrengthKG: 1.5, IsNight: true,
		}
		changed := srv.evaluateQualifications(ctx, ev)
		for id := range changed {
			if id/1000 == 4 {
				t.Fatalf("rod %d advanced telescopic task %d", rod, id)
			}
		}
	}
	// 真实手竿仍计数；超轻手竿按测试上限识别，无须伪造 UL 硬度尾缀。
	ev := qualificationEvent{
		Kind: qevFishLanded, CatchGUID: "tele-catch", Species: "dace",
		WeightG: 600, Grade: 3, RodType: 3, RigType: 1,
		RodTestMax: 5, MainLine: 1, LineStrengthKG: 1.5, LocLevel: evLocBelaya,
	}
	changed := srv.evaluateQualifications(ctx, ev)
	for _, id := range []int64{4001, 4303, 4502, 4602} {
		if len(changed[id].Counted) != 1 {
			t.Fatalf("telescopic catch must advance task %d", id)
		}
	}
	for id := range changed {
		if id/1000 == 8 {
			t.Fatalf("freshwater telescopic catch advanced marine task %d", id)
		}
	}
	srv.evaluateQualifications(ctx, ev)
	states, err := db.QualificationStates(context.Background(), sess.Userid)
	if err != nil {
		t.Fatal(err)
	}
	if len(states[4602].Counted) != 1 {
		t.Fatal("replayed catch must not increase telescopic progress")
	}
}

func TestQualificationFloatBranchRequiresTelescopicRod(t *testing.T) {
	for _, tc := range []struct{ rod, rig int64 }{{3, 1}, {4, 2}, {5, 4}, {0, 1}} {
		t.Run(strconv.FormatInt(tc.rod, 10), func(t *testing.T) {
			srv, db := newDetachTestServer(t)
			if err := db.Migrate(); err != nil {
				t.Fatal(err)
			}
			sess := newProbeSess(srv)
			ctx := &Ctx{Sess: sess, Log: logging.Discard()}
			ev := qualificationEvent{
				Kind: qevFishLanded, CatchGUID: "float-rod", Species: "dace",
				WeightG: 500, Grade: 3, RodType: tc.rod, RigType: tc.rig,
				RodTestMax: 5, MainLine: 1, LineStrengthKG: 1.5, LocLevel: evLocBelaya,
			}
			changed := srv.evaluateQualifications(ctx, ev)
			for _, id := range []int64{4001, 4602} {
				if got := len(changed[id].Counted); (got == 1) != (tc.rod == 3) {
					t.Fatalf("task %d requires rod type 3, rig=%d rod=%d count=%d", id, tc.rig, tc.rod, got)
				}
			}
			for _, id := range []int64{4303, 4502} {
				_, advanced := changed[id]
				if advanced != (tc.rod == 3) {
					t.Fatalf("task %d explicitly requires telescopic rod, rod=%d advanced=%v", id, tc.rod, advanced)
				}
			}
		})
	}
}

func TestQualificationFloatTaskTextConditions(t *testing.T) {
	var lineRule, xpRule qualificationRuleDef
	for _, rule := range qualificationRules {
		switch rule.ID {
		case 4502:
			lineRule = rule
		case 4402:
			xpRule = rule
		}
	}
	ev := qualificationEvent{
		CatchGUID: "line-limit", Species: "perch", WeightG: 500, Grade: 3,
		RodType: 3, RigType: 1, RodTestMax: 5, MainLine: 1,
	}
	for _, tc := range []struct {
		kg   float64
		want bool
	}{{0, false}, {1.5, true}, {1.5001, false}} {
		ev.LineStrengthKG = tc.kg
		if got := lineRule.Counted(ev) != ""; got != tc.want {
			t.Fatalf("4502 line strength %g: counted=%v, want %v", tc.kg, got, tc.want)
		}
	}
	ev.XP, ev.BaseXP = 14000, 6999
	if xpRule.Counted(ev) != "" {
		t.Fatal("4402 must not use bonus XP to reach the base XP threshold")
	}
	ev.BaseXP = 7000
	if xpRule.Counted(ev) == "" {
		t.Fatal("4402 must count exactly 7000 base XP")
	}
}

// 永久手竿分支共 21 条，每条的 5007 显示与服务端过滤都必须限定伸缩竿。
func TestQualificationAllFloatTasksRequireTelescopicRod(t *testing.T) {
	count := 0
	for _, rule := range qualificationRules {
		if rule.ID/1000 != 4 {
			continue
		}
		count++
		types := qualificationRodTypesFor(rule.ID)
		if len(types) != 1 || types[0] != rodTypeTelescope {
			t.Fatalf("task %d rod filter = %v, want [3]", rule.ID, types)
		}
		body, err := proto.EncodeValue(qualificationRule(uint64(rule.ID)), wiregen.Table())
		if err != nil {
			t.Fatal(err)
		}
		wireRule, _, err := proto.DecodeValue(body, 0, wiregen.Table())
		if err != nil {
			t.Fatal(err)
		}
		got := wireRule.Fields[5].Val.Items
		if len(got) != 1 || got[0].U32Val() != rodTypeTelescope {
			t.Fatalf("task %d shown rod filter = %v, want [3]", rule.ID, got)
		}
	}
	if count != 21 {
		t.Fatalf("float branch task count = %d, want 21", count)
	}
}
