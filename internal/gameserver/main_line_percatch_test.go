package gameserver

import (
	"context"
	"testing"

	"rf4server/internal/logging"
)

func TestCatchSettlementDoesNotWearMainLine(t *testing.T) {
	srv, db := newRigTestServer(t)
	ctx := context.Background()
	const uid = int64(1000001)
	rod, err := srv.CreateItem(ctx, uid, "feeder_10125_FD360", "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	line, err := srv.CreateItem(ctx, uid, "mono_40853", "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	sess := newProbeSess(srv)
	mountGearForTest(t, srv, sess, rod.GUID, line.GUID)
	rodBefore, err := db.GetItemByGUID(ctx, rod.GUID)
	if err != nil {
		t.Fatal(err)
	}
	lineBefore, err := db.GetItemByGUID(ctx, line.GUID)
	if err != nil {
		t.Fatal(err)
	}
	st := srv.stateFor(uid)
	st.mu.Lock()
	st.GearGuid = rod.GUID
	st.Pending = &RolledFish{FishGUID: newFishGUID(), Species: "perch", Grade: 2,
		WeightG: 642, LengthM: .28, LocLevel: "level_001_lake"}
	st.Hooked = true
	st.mu.Unlock()
	body, handled := srv.fishingEndSettle(&Ctx{Sess: sess, CallID: 1,
		Main: 14, Sub: 4, ExpectResponse: true, Log: logging.Discard()})
	if !handled || len(body) == 0 {
		t.Fatal("结算未正常返回")
	}
	rodAfter, err := db.GetItemByGUID(ctx, rod.GUID)
	if err != nil {
		t.Fatal(err)
	}
	lineAfter, err := db.GetItemByGUID(ctx, line.GUID)
	if err != nil {
		t.Fatal(err)
	}
	if lineAfter.Durability != lineBefore.Durability {
		t.Fatalf("主线不应按尾扣耐久: %v -> %v", lineBefore.Durability, lineAfter.Durability)
	}
	if rodAfter.Durability >= rodBefore.Durability {
		t.Fatalf("竿本体按尾磨损应保留: %v -> %v", rodBefore.Durability, rodAfter.Durability)
	}
}
