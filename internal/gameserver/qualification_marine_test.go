package gameserver

import (
	"context"
	"strconv"
	"testing"

	"rf4server/internal/logging"
	"rf4server/internal/proto"
	"rf4server/internal/wiregen"
)

func TestQualificationMarineTreeOmitsUnspecifiedRigConditions(t *testing.T) {
	for _, stage := range []uint64{1, 2, 3, 4, 5, 6, 7} {
		body, err := proto.EncodeValue(qualificationTaskList(8, stage), wiregen.Table())
		if err != nil {
			t.Fatal(err)
		}
		tasks, _, err := proto.DecodeValue(body, 0, wiregen.Table())
		if err != nil {
			t.Fatal(err)
		}
		for i, task := range tasks.Items {
			id := qualificationRuleID(8, stage, uint64(i+1))
			rule := task.Field(1).Val.Items[0]
			switch id {
			case 8101, 8102, 8202, 8302, 8303, 8403, 8502:
				// 保留规则原有的明确钓组条件，独立显示表是否合入均适用。
				want, err := proto.EncodeValue(qualificationRule(id), wiregen.Table())
				if err != nil {
					t.Fatal(err)
				}
				got, err := proto.EncodeValue(rule, wiregen.Table())
				if err != nil {
					t.Fatal(err)
				}
				if string(got) != string(want) {
					t.Fatalf("%d explicit rig condition was changed", id)
				}
			default:
				rigs := rule.Field(9).Val
				if rigs == nil || rigs.Kind != proto.KindU32Arr || len(rigs.Items) != 0 {
					t.Fatalf("%d must not display an extra marine-rig condition", id)
				}
			}
		}
	}
}

// 海洋任务限定渔获地图；即使用海钓钓组，在淡水地图也不能推进。
func TestQualificationMarineMapFilters(t *testing.T) {
	cases := map[int64]qualificationEvent{
		8001: {Species: "atl_cod", RigType: 50},
		8002: {WeightG: 7000, RigType: 50},
		8003: {Bait: "pilker", RigType: 50},
		8101: {WeightG: 15000, RigType: 50},
		8102: {Species: "atl_cod", RigType: 50},
		8103: {Grade: 3, RigType: 50},
		8201: {WeightG: 35000, RigType: 50},
		8202: {Species: "atl_cod", RigType: 51},
		8203: {WeightG: 5000, Grade: 3, RigType: 50},
		8301: {WeightG: 50000, RigType: 50},
		8302: {Species: "sea_kambala", Grade: 3, RigType: 58},
		8303: {Species: "atl_cod", RigType: 55},
		8401: {Species: "atl_cod", DepthCM: 12000, RigType: 50},
		8402: {Species: "m_zubatka", Grade: 3, RigType: 50},
		8403: {Species: "red_fish", Grade: 3, RigType: 57},
		8501: {Species: "atl_cod", Grade: 3, RigType: 50},
		8502: {Species: "atl_cod", Grade: 3, RigType: 59},
		8503: {DepthCM: 12000, Grade: 3, RigType: 50},
		8601: {Species: "halibut_w", Grade: 3, RigType: 50},
		8602: {Species: "molva", Grade: 3, RigType: 50},
		8603: {Rarity: 1, Grade: 3, RigType: 50},
	}
	for _, rule := range qualificationRules {
		if rule.ID/1000 != 8 {
			continue
		}
		t.Run(strconv.FormatInt(rule.ID, 10), func(t *testing.T) {
			ev, ok := cases[rule.ID]
			if !ok {
				t.Fatal("missing marine task fixture")
			}
			ev.Kind, ev.CatchGUID = qevFishLanded, "marine-catch"
			for _, level := range []string{"", evLocCopper, evLocLadogaIs, evLocBelaya} {
				ev.LocLevel = level
				if key := rule.Counted(ev); key != "" {
					t.Errorf("map %q must not count, got %q", level, key)
				}
			}
			ev.LocLevel = evLocNorthSea
			if key := rule.Counted(ev); key == "" {
				t.Fatal("eligible catch in the Norwegian Sea must count")
			}
		})
	}
}

// 1-1/1-2/1-3 仅限定地图，不额外限制钓组；仍按原鱼种/鱼/饵去重。
func TestQualificationMarineFirstStageProgress(t *testing.T) {
	srv, db := newDetachTestServer(t)
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}
	sess := newProbeSess(srv)
	ctx := &Ctx{Sess: sess, Log: logging.Discard()}
	ev := qualificationEvent{
		Kind: qevFishLanded, CatchGUID: "marine-first", Species: "atl_cod",
		WeightG: 20179, Bait: "bait-a", RigType: 6, LocLevel: evLocCopper,
	}
	changed := srv.evaluateQualifications(ctx, ev)
	for _, id := range []int64{8001, 8002, 8003} {
		if _, ok := changed[id]; ok {
			t.Fatalf("freshwater catch advanced marine task %d", id)
		}
	}
	ev.LocLevel = evLocNorthSea
	changed = srv.evaluateQualifications(ctx, ev)
	for _, id := range []int64{8001, 8002, 8003} {
		if len(changed[id].Counted) != 1 {
			t.Fatalf("Norwegian Sea ordinary spinning catch must advance task %d", id)
		}
	}
	for _, id := range []int64{8101, 8102, 8202, 8302, 8303, 8403, 8502} {
		if _, ok := changed[id]; ok {
			t.Fatalf("ordinary spinning catch advanced rig-specific marine task %d", id)
		}
	}
	changed = srv.evaluateQualifications(ctx, ev)
	for _, id := range []int64{8001, 8002, 8003} {
		if _, ok := changed[id]; ok {
			t.Fatalf("replayed catch advanced task %d", id)
		}
	}
	ev.CatchGUID = "marine-second"
	srv.evaluateQualifications(ctx, ev) // 同鱼种、同饵的另一尾鱼。
	states, err := db.QualificationStates(context.Background(), sess.Userid)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{8001, 8003} {
		if len(states[id].Counted) != 1 {
			t.Fatalf("same species/bait must count once for task %d", id)
		}
	}
}

// 8502 原文为三条奖杯鱼，不要求三个鱼种；同一尾重放仍只能算一条。
func TestQualificationMarine8502CountsFishNotSpecies(t *testing.T) {
	srv, db := newDetachTestServer(t)
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}
	sess := newProbeSess(srv)
	ctx := &Ctx{Sess: sess, Log: logging.Discard()}
	for i := 0; i < 3; i++ {
		ev := qualificationEvent{
			Kind: qevFishLanded, CatchGUID: "dead-fish-" + strconv.Itoa(i),
			Species: "atl_cod", Grade: 3, RigType: 59, RodType: 16, LocLevel: evLocNorthSea,
		}
		changed := srv.evaluateQualifications(ctx, ev)
		if len(changed[8502].Counted) != i+1 {
			t.Fatalf("same-species catch %d must advance 8502", i+1)
		}
		if replay := srv.evaluateQualifications(ctx, ev); replay[8502].RuleID != 0 {
			t.Fatal("replayed catch must not advance 8502")
		}
	}
	states, err := db.QualificationStates(context.Background(), sess.Userid)
	if err != nil {
		t.Fatal(err)
	}
	if len(states[8502].Counted) != 3 || states[8502].CompletedTS == 0 {
		t.Fatal("three trophy catches of the same species must complete 8502")
	}
}
