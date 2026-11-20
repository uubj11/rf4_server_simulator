package gameserver

import (
	"strconv"
	"testing"
	"time"

	"rf4server/internal/logging"
	"rf4server/internal/proto"
	"rf4server/internal/wiregen"
)

// 期望取自客户端 4.0.25034 的 5007，而非中文任务文案的概括。
func TestQualificationMarineReferenceConditions(t *testing.T) {
	cases := []struct {
		id   int64
		ev   qualificationEvent
		want bool
	}{
		{8202, qualificationEvent{RigType: 52, Species: "atl_cod"}, true},
		{8202, qualificationEvent{RigType: 50, Species: "atl_cod"}, false},
		{8302, qualificationEvent{RigType: 58, Species: "turbot", Grade: 3}, true},
		{8302, qualificationEvent{RigType: 58, Species: "atl_cod", Grade: 3}, false},
		{8402, qualificationEvent{RigType: 50, Species: "p_zubatka", Grade: 3}, true},
		{8403, qualificationEvent{RigType: 57, Species: "sebastes_n", Grade: 3}, true},
		{8403, qualificationEvent{RigType: 57, Species: "sebastes_m", Grade: 3}, true},
		{8601, qualificationEvent{RigType: 50, Species: "halibut_b", Grade: 3}, true},
		{8603, qualificationEvent{RigType: 50, Rarity: 1, Grade: 2}, false},
		{8603, qualificationEvent{RigType: 50, Rarity: 1, Grade: 3}, true},
	}
	for _, tc := range cases {
		for _, rule := range qualificationRules {
			if rule.ID != tc.id {
				continue
			}
			tc.ev.LocLevel, tc.ev.CatchGUID = evLocNorthSea, "reference-fish"
			if got := rule.Counted(tc.ev) != ""; got != tc.want {
				t.Errorf("rule %d event %+v: counted=%v want=%v", tc.id, tc.ev, got, tc.want)
			}
		}
	}
}

func TestQualificationCarpNineBottomHairRigs(t *testing.T) {
	s, db := newDetachTestServer(t)
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}
	sess := newProbeSess(s)
	ctx := &Ctx{Sess: sess, Log: logging.Discard()}
	catch := func(rig int64) {
		t.Helper()
		s.evaluateQualifications(ctx, qualificationEvent{
			Kind: qevFishLanded, CatchGUID: "hair-" + strconv.FormatInt(rig, 10),
			Species: "carp", RigType: rig, Grade: 3,
		})
	}
	catch(30) // 浮钓发丝不在任务的九种底部钓组中。
	for i, rig := range []int64{24, 25, 26, 27, 28, 29, 16, 32, 34} {
		catch(rig)
		catch(rig)
		states, err := db.QualificationStates(t.Context(), sess.Userid)
		if err != nil {
			t.Fatal(err)
		}
		st := states[7502]
		if len(st.Counted) != i+1 || (st.CompletedTS > 0) != (i == 8) {
			t.Fatalf("rig %d: got %d/9 complete=%v", rig, len(st.Counted), st.CompletedTS > 0)
		}
	}
}

func TestQualificationEvent12301ExactBiteWindow(t *testing.T) {
	var rule qualificationRuleDef
	for _, r := range qualificationRules {
		if r.ID == 12301 {
			rule = r
		}
	}
	ev := qualificationEvent{CatchGUID: "night-fish", LocLevel: evLocSevDon, WeightG: 3001, IsNight: true}
	if rule.Counted(ev) != "" {
		t.Fatal("legacy snapshot without precise bite time must not use settlement time")
	}
	for _, hour := range []int64{19, 22, 23, 0, 3, 4, 5} {
		ev.BiteUnix = worldDayEpochUnix + 3600 + hour*realSecPerGameHour
		if got, want := rule.Counted(ev) != "", hour >= 23 || hour < 4; got != want {
			t.Errorf("hour %d: counted=%v want=%v", hour, got, want)
		}
	}
	copy := rolledFishFromStore(rolledFishToStore(&RolledFish{BiteUnix: ev.BiteUnix}))
	if copy.BiteUnix != ev.BiteUnix {
		t.Fatal("precise bite time lost on persistence roundtrip")
	}
}

func TestQualificationEventScopeStopsInactiveProgress(t *testing.T) {
	for _, tc := range []struct {
		name          string
		on            bool
		branches      string
		expired, want bool
	}{
		{"off", false, "19", false, false},
		{"other-branch", true, "9", false, false},
		{"expired", true, "19", true, false},
		{"active", true, "19", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := eventTestServerBranches(t, false, tc.on, tc.branches)
			if err := s.DB.Migrate(); err != nil {
				t.Fatal(err)
			}
			end := time.Now().Unix() + 3600
			if tc.expired {
				end = time.Now().Unix() - 1
			}
			s.tunMu.Lock()
			s.tunCache[eventEndKey] = float64(end)
			s.tunMu.Unlock()
			sess := newProbeSess(s)
			ev := qualificationEvent{Kind: qevFishLanded, CatchGUID: "event-scope", Species: "gudgeon", WeightG: 80, LocLevel: evLocVyunok}
			changed := s.evaluateQualifications(&Ctx{Sess: sess, Log: logging.Discard()}, ev)
			if got := changed[19002].CompletedTS > 0; got != tc.want {
				t.Fatalf("completed=%v want=%v", got, tc.want)
			}
			if len(changed[1001].Counted) != 1 {
				t.Fatal("event switch must not stop permanent qualifications")
			}
		})
	}
}

func TestQualificationEventAlternativesDoNotMix(t *testing.T) {
	s := eventTestServerBranches(t, false, true, "20")
	if err := s.DB.Migrate(); err != nil {
		t.Fatal(err)
	}
	sess := newProbeSess(s)
	ctx := &Ctx{Sess: sess, Log: logging.Discard()}
	catch := func(guid, species string, grade, rarity int64) {
		t.Helper()
		s.evaluateQualifications(ctx, qualificationEvent{
			Kind: qevFishLanded, CatchGUID: guid, Species: species,
			Grade: grade, Rarity: rarity, RodPower: "UL",
		})
	}
	catch("trophy-1", "ordinary", 3, 0)
	catch("rare-1", "rare-a", 2, 1)
	catch("rare-repeat", "rare-a", 2, 1)
	states, err := s.DB.QualificationStates(t.Context(), sess.Userid)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{20602, 20603} {
		if len(states[id].Counted) != 1 || states[id].CompletedTS != 0 {
			t.Fatalf("rule %d must not combine one trophy with one rare species: %+v", id, states[id])
		}
	}
	// 下次事件从持久化证据恢复：第二种罕见鱼无需达到星。
	catch("rare-2", "rare-b", 1, 2)
	states, err = s.DB.QualificationStates(t.Context(), sess.Userid)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{20602, 20603} {
		if len(states[id].Counted) != 2 || states[id].CompletedTS == 0 {
			t.Fatalf("rule %d must complete on two distinct rare species: %+v", id, states[id])
		}
	}
	if changed := s.evaluateQualifications(ctx, qualificationEvent{Kind: qevFishLanded, Species: "rare-b", CatchGUID: "rare-2", Rarity: 2, RodPower: "UL"}); changed[20602].RuleID != 0 {
		t.Fatal("completed alternative must not emit repeated progress")
	}
}

// 走完整 14/4，证明额外鱼不仅落库，还把最终资格计数放进客户端响应。
func TestQualificationMarineExtrasSettleAndDeliverProgress(t *testing.T) {
	s, db := newStaticRigTestServer(t)
	defer db.Close()
	sess := newProbeSess(s)
	uid := sess.Userid
	if _, err := db.EnsureProfile(t.Context(), uid); err != nil {
		t.Fatal(err)
	}
	rod, err := s.CreateItem(t.Context(), uid, "boatr_10222_SBH80_100", "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	if !s.rigSetType(t.Context(), uid, rod.GUID, 59) {
		t.Fatal("marine rig setup failed")
	}
	fish := make([]*RolledFish, 3)
	for i := range fish {
		fish[i] = &RolledFish{
			FishGUID: qualificationCountedGUID("multi-qualification-" + strconv.Itoa(i)),
			Species:  "atl_cod", Grade: 3, WeightG: 10000,
			LocLevel: evLocNorthSea, BaitsKnown: true,
		}
	}
	fish[0].GearGUID = rod.GUID
	st := s.selectFishingRod(uid, rod.GUID)
	st.mu.Lock()
	st.GearGuid, st.RigType, st.Hooked = rod.GUID, 59, true
	st.Pending, st.Extra = fish[0], fish[1:]
	st.mu.Unlock()
	body, ok := s.fishingEndSettle(&Ctx{Sess: sess, Main: 14, Sub: 4, CallID: 1, ExpectResponse: true, Log: logging.Discard()})
	if !ok {
		t.Fatal("14/4 not handled")
	}
	root, _, err := proto.DecodeValue(body, 0, wiregen.Table())
	if err != nil {
		t.Fatal(err)
	}
	states, err := db.QualificationStates(t.Context(), uid)
	if err != nil {
		t.Fatal(err)
	}
	if len(states[8502].Counted) != 3 || states[8502].CompletedTS == 0 {
		t.Fatalf("three same-species fish must complete 8502: %+v", states[8502])
	}
	assertResponse := func(root *proto.Value) {
		t.Helper()
		for _, counter := range root.Field(3).Val.Field(2).Val.Items {
			id := counter.Field(0)
			if id.U32() == 8502 {
				if len(counter.Field(1).Val.Items) != 3 || *counter.Field(2).Num == 0 {
					t.Fatal("14/4 must deliver final 3/3 progress, not main-fish-only 1/3")
				}
				return
			}
		}
		t.Fatal("14/4 response missing 8502 counter")
	}
	assertResponse(root)
	body, ok = s.fishingEndSettle(&Ctx{Sess: sess, Main: 14, Sub: 4, CallID: 2, ExpectResponse: true, Log: logging.Discard()})
	if !ok {
		t.Fatal("14/4 retry not handled")
	}
	root, _, err = proto.DecodeValue(body, 0, wiregen.Table())
	if err != nil {
		t.Fatal(err)
	}
	assertResponse(root)
	states, err = db.QualificationStates(t.Context(), uid)
	if err != nil || len(states[8502].Counted) != 3 {
		t.Fatalf("14/4 retry changed counted fish: %+v err=%v", states[8502], err)
	}
}
