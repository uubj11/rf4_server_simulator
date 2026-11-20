package gameserver

import (
	"math"
	"strconv"
	"testing"

	"rf4server/internal/logging"
	"rf4server/internal/proto"
)

// 复现玩家的真实装配：主线在竿的独立字段中，不在 f5 钓组组件内。
func TestQualificationMainLineFromRodField(t *testing.T) {
	s, db := newRigTestServer(t)
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}
	sess := newProbeSess(s)
	rod, err := s.CreateItem(t.Context(), sess.Userid, "tele_10178_500", "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	line, err := s.CreateItem(t.Context(), sess.Userid, "mono_15256", "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	runRodSet(t, s, sess, rod.GUID, line.GUID, 0)
	row := mustItem(t, db, rod.GUID)
	mainGUID := rigStoredGUID(t, row, csLine)
	if mainGUID == "" || len(rigCompKeysOfCategory(s.storedRigComponents(t.Context(), rod.GUID), catLine)) != 0 {
		t.Fatal("fixture must have a dedicated main-line reference and no main line in f5")
	}
	// 投影不是装配真源；删除它仍应能按竿实例读取已装主线。
	if _, err := db.SQL.Exec(`DELETE FROM rig_component WHERE rig_guid=? AND slot=11`, rod.GUID); err != nil {
		t.Fatal(err)
	}
	fish := &RolledFish{
		FishGUID: newFishGUID(), GearGUID: rod.GUID, Species: "dreissena",
		WeightG: 936, Grade: 4, LocLevel: evLocSevDon,
	}
	ev := s.qualificationEventForCatch(t.Context(), fish, 1)
	if ev.MainLine != 1 || math.Abs(ev.LineStrengthKG-1.4) > 1e-6 {
		t.Fatalf("mounted nylon/1.4kg was not read: material=%d strength=%g", ev.MainLine, ev.LineStrengthKG)
	}
	ctx := &Ctx{Sess: sess, Log: logging.Discard()}
	changed := s.evaluateQualifications(ctx, ev)
	if st := changed[4502]; len(st.Counted) != 1 || st.CompletedTS == 0 {
		t.Fatalf("real UL telescopic/nylon/1.4kg/936g blue catch must complete 4502: %+v", st)
	}
	// 拆掉真实主线后，即使派生表残留旧索引，也不能继续使用旧线条件。
	fields, err := FieldsFromJSON(row.FieldsJSON)
	if err != nil {
		t.Fatal(err)
	}
	fields[fieldIdxByCS(row.WireClass, csLine)] = proto.Field{Op: "OPTGUID"}
	s.saveItemFields(t.Context(), row, fields)
	if _, err := db.SQL.Exec(`INSERT INTO rig_component(rig_guid,slot,ordinal,guid,extra) VALUES(?,11,0,?,0)`, rod.GUID, mainGUID); err != nil {
		t.Fatal(err)
	}
	ev = s.qualificationEventForCatch(t.Context(), fish, 1)
	if ev.MainLine != 0 || ev.LineStrengthKG != 0 {
		t.Fatalf("detached main line leaked through stale projection: %+v", ev)
	}
}

// 所有活动线强度任务都经真实装配取值，并验证各自上限的两侧。
func TestQualificationMountedLineActivityLimits(t *testing.T) {
	for _, tc := range []struct {
		id    int64
		limit float64
		loc   string
		rod   string
		rig   int64
	}{
		{14402, 3, evLocSevDon, "tele_10178_500", 1},
		{14602, 12, evLocKuori, "spin_10158_M60UL", 5},
		{14802, 1.5, evLocSevDon, "tele_10178_500", 1},
		{16102, 2, evLocSevDon, "tele_10178_500", 1},
		{18402, 3, evLocSevDon, "tele_10178_500", 1},
		{18602, 12, evLocKuori, "spin_10158_M60UL", 5},
		{18802, 1.5, evLocSevDon, "tele_10178_500", 1},
		{19401, 5, evLocKuori, "spin_10158_M60UL", 5},
		{20103, 2, evLocSevDon, "tele_10178_500", 1},
	} {
		t.Run(strconv.FormatInt(tc.id, 10), func(t *testing.T) {
			s, db := newRigTestServer(t)
			if err := db.Migrate(); err != nil {
				t.Fatal(err)
			}
			sess := newProbeSess(s)
			ctx := &Ctx{Sess: sess, Log: logging.Discard()}
			rod, err := s.CreateItem(t.Context(), sess.Userid, tc.rod, "backpack", 1)
			if err != nil {
				t.Fatal(err)
			}
			line, err := s.CreateItem(t.Context(), sess.Userid, "mono_15256", "backpack", 1)
			if err != nil {
				t.Fatal(err)
			}
			runRodSet(t, s, sess, rod.GUID, line.GUID, 0)
			for _, kg := range []float64{tc.limit + 0.01, tc.limit} {
				if _, err := db.SQL.Exec(`UPDATE item_def SET attrs_json=json_set(attrs_json,'$.kngaaefkpka',?) WHERE numeric_id=?`, proto.F32Bits(float32(kg)), line.NumericID); err != nil {
					t.Fatal(err)
				}
				fish := &RolledFish{
					FishGUID: newFishGUID(), GearGUID: rod.GUID, Species: "perch",
					WeightG: 6000, Grade: 3, LocLevel: tc.loc,
				}
				ev := s.qualificationEventForCatch(t.Context(), fish, tc.rig)
				if math.Abs(ev.LineStrengthKG-kg) > 1e-5 {
					t.Fatalf("line source: got %g, want %g", ev.LineStrengthKG, kg)
				}
				s.evaluateQualifications(ctx, ev)
				after, err := db.QualificationStates(t.Context(), sess.Userid)
				if err != nil {
					t.Fatal(err)
				}
				got := len(after[tc.id].Counted)
				want := 0
				if kg == tc.limit {
					want = 1
				}
				if got != want {
					t.Fatalf("task %d at %gkg: added %d, want %d", tc.id, kg, got, want)
				}
			}
		})
	}
}
