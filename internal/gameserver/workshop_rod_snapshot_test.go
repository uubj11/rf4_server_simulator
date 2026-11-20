package gameserver

import (
	"testing"

	"rf4server/internal/logging"
	"rf4server/internal/proto"
)

func TestWorkshopRodReplacementSurvivesSnapshot(t *testing.T) {
	srv, db := newStaticRigTestServer(t)
	ctx := t.Context()
	rod, err := srv.CreateItem(ctx, 1000001, "carp_10140_13_4_50", "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	// 竿身有磨损，新买的手柄仍应保持完整耐久。
	if err := db.UpdateItemFields(ctx, rod.GUID, rod.FieldsJSON, rod.Qty, .5, rod.Workmanship, rod.RigGUID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.EnsureProfile(ctx, 1000001); err != nil {
		t.Fatal(err)
	}
	if err := db.UpdateBalance(ctx, 1000001, 100000, 0, 0); err != nil {
		t.Fatal(err)
	}
	c := &Ctx{Sess: newProbeSess(srv), Main: 21, Sub: 3,
		Tree: snapshotWorkshopRequestPart(rod.GUID, 4, "hand_kgf_sage_pick", 0), Log: logging.Discard()}
	body, ok := hWorkshopOrder(c)
	if !ok {
		t.Fatal("换手柄失败")
	}
	fresh := mustItem(t, db, rod.GUID)
	persisted := snapshotFields(t, fresh.FieldsJSON)
	installed := persisted[1].Val
	wantGUID := installed.Fields[fieldIdxByCS(installed.Class, csGUID)].Hex
	if got := srv.childSystemID(ctx, installed); got != "hand_kgf_sage_pick" {
		t.Fatalf("落库手柄=%s", got)
	}
	reply, _, err := proto.DecodeValue(body, 0, srv.Table())
	if err != nil {
		t.Fatal(err)
	}
	reloaded, err := srv.ItemToTree(ctx, fresh)
	if err != nil {
		t.Fatal(err)
	}
	for name, tree := range map[string]*proto.Value{"reply": reply, "reload": reloaded} {
		child := tree.Fields[1].Val
		if got := reelChildDurability(child); got != 1 {
			t.Fatalf("%s 新手柄耐久=%v，期望 1", name, got)
		}
		if got := srv.childSystemID(ctx, child); got != "hand_kgf_sage_pick" {
			t.Fatalf("%s 恢复了出厂手柄: %s", name, got)
		}
		if got := child.Fields[fieldIdxByCS(child.Class, csGUID)].Hex; got != wantGUID {
			t.Fatalf("%s 部件 GUID 与诊断不一致", name)
		}
		ci := fieldIdxByCS(child.Class, csCreated)
		if child.Fields[ci].U64() == 0 || child.Fields[ci].U64() != installed.Fields[ci].U64() {
			t.Fatalf("%s 手柄版本未保留", name)
		}
	}
	for name, rig := range map[string]*proto.Value{"persisted": persisted[5].Val, "reply": reply.Fields[5].Val, "reload": reloaded.Fields[5].Val} {
		count := 0
		for _, comp := range rigCompsOf(rig) {
			if comp.Category == 4 {
				count++
				if comp.GUID != wantGUID {
					t.Fatalf("%s 手柄索引仍指向旧件", name)
				}
			}
		}
		if count != 1 {
			t.Fatalf("%s 手柄索引数=%d", name, count)
		}
	}
}
