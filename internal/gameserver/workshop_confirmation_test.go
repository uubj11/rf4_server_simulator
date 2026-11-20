package gameserver

import (
	"testing"

	"rf4server/internal/logging"
	"rf4server/internal/proto"
)

// Exercise the registered commands: 21/3 previews the selection; 21/4 pays.
func TestWorkshopPreviewThenConfirm(t *testing.T) {
	srv, db := newStaticRigTestServer(t)
	reel, _ := seedWornWorkshopReel(t, srv, db, .8, .2, .5, .35)
	ctx := t.Context()
	if _, err := db.EnsureProfile(ctx, 1000001); err != nil {
		t.Fatal(err)
	}
	if err := db.UpdateBalance(ctx, 1000001, 100000, 0, 0); err != nil {
		t.Fatal(err)
	}
	d := NewDispatch(srv)
	registerEconMarket(d)
	h := newMailTestSession(t, srv, 71, 1000001)
	const replacement = "tama_reel_oil"
	price, _ := srv.partPrice(ctx, replacement, 10)
	var preview *proto.Value
	for range 2 {
		c := &Ctx{Sess: h.sess, Main: 21, Sub: 3, Tree: snapshotWorkshopRequestPart(reel.GUID, 10, replacement, 0), Log: logging.Discard()}
		body, ok := d.lookup(21, 3)(c)
		if !ok {
			t.Fatal("preview failed")
		}
		var err error
		preview, _, err = proto.DecodeValue(body, 0, srv.Table())
		if err != nil {
			t.Fatal(err)
		}
		_, def := snapshotPart(t, srv, preview.Fields, 10)
		if def.ItemID != replacement {
			t.Fatal("preview does not show selected oil")
		}
		fresh := mustItem(t, db, reel.GUID)
		p, _ := db.GetProfile(ctx, 1000001)
		if fresh.FieldsJSON != reel.FieldsJSON || fresh.Durability != reel.Durability || p.Silver != 100000 || len(c.afterResponse) != 0 {
			t.Fatal("preview changed persisted equipment, wallet, or scheduled pushes")
		}
	}
	c := &Ctx{Sess: h.sess, Main: 21, Sub: 4, Tree: snapshotWorkshopRequestPart(reel.GUID, 10, replacement, uint64(price)), Log: logging.Discard()}
	body, ok := d.lookup(21, 4)(c)
	if !ok {
		t.Fatal("confirmation failed")
	}
	confirmed, _, err := proto.DecodeValue(body, 0, srv.Table())
	if err != nil {
		t.Fatal(err)
	}
	version := fieldIdxByCS(confirmed.Class, csCreated)
	if confirmed.Fields[version].U64() <= preview.Fields[version].U64() {
		t.Fatal("confirmation is not newer than preview")
	}
	fresh := mustItem(t, db, reel.GUID)
	_, def := snapshotPart(t, srv, snapshotFields(t, fresh.FieldsJSON), 10)
	p, _ := db.GetProfile(ctx, 1000001)
	if def.ItemID != replacement || p.Silver != 100000-price {
		t.Fatal("confirmation did not persist replacement and charge once")
	}
	for _, fn := range c.afterResponse {
		fn()
	}
	push := nextMailTestPush(t, h, 4, 22)
	assertWorkshopPushMatchesResponse(t, srv, push.ArgItem(0), body)
	profile := nextMailTestPush(t, h, 2, 1).ArgItem(0)
	if profile.Fields[7].U64() != uint64(100000-price) {
		t.Fatal("profile push contains stale balance")
	}
	// Switching the same slot again reuses its GUID, so its version must advance.
	oldChild, _ := snapshotPart(t, srv, confirmed.Fields, 10)
	c.Tree = snapshotWorkshopRequestPart(reel.GUID, 10, "blg_reel_oil", 0)
	body, ok = hWorkshopOrder(c)
	if !ok {
		t.Fatal("second replacement failed")
	}
	second, _, err := proto.DecodeValue(body, 0, srv.Table())
	if err != nil {
		t.Fatal(err)
	}
	child, childDef := snapshotPart(t, srv, second.Fields, 10)
	ci := fieldIdxByCS(child.Class, csCreated)
	if childDef.ItemID != "blg_reel_oil" || child.Fields[ci].U64() <= oldChild.Fields[ci].U64() {
		t.Fatal("second replacement did not refresh child model and version")
	}
}

func assertWorkshopPushMatchesResponse(t *testing.T, srv *Server, pushed *proto.Value, body []byte) {
	t.Helper()
	response, _, err := proto.DecodeValue(body, 0, srv.Table())
	if err != nil {
		t.Fatal(err)
	}
	idx := fieldIdxByCS(response.Class, csCreated)
	if pushed.Fields[idx].U64() <= response.Fields[idx].U64() {
		t.Fatal("item refresh must be newer than confirmation")
	}
	pushed.Fields[idx].Num = response.Fields[idx].Num
	encoded, err := proto.EncodeValue(pushed, srv.Table())
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) != string(body) {
		t.Fatal("item refresh differs from confirmation apart from version")
	}
}

func TestWorkshopTinyWearRepairAndQuote(t *testing.T) {
	srv, db := newStaticRigTestServer(t)
	reel, partID := seedWornWorkshopReel(t, srv, db, .9995, .2, .5, .35)
	ctx := t.Context()
	if _, err := db.EnsureProfile(ctx, 1000001); err != nil {
		t.Fatal(err)
	}
	if err := db.UpdateBalance(ctx, 1000001, 10000, 0, 0); err != nil {
		t.Fatal(err)
	}
	for _, part := range srv.repairPartsOf(ctx, reel) {
		if part.Cat != uint32(reelPartCatGear) {
			continue
		}
		jobs := srv.partAlternatives(ctx, reel.ItemID, part)
		for _, job := range jobs {
			if *job.Fields[0].Str == partID && job.Fields[2].Val.Fields[0].U64() != 100 {
				t.Error("same-model order quote must respect the client's one-silver minimum")
			}
		}
	}
	// Actual client 21/4 sends 100 even when the server quotes one cent.
	c := &Ctx{Sess: newProbeSess(srv), Main: 21, Sub: 4, Tree: snapshotWorkshopRequestPart(reel.GUID, uint32(reelPartCatGear), partID, 100), Log: logging.Discard()}
	if _, ok := hWorkshopOrder(c); !ok {
		t.Fatal("tiny wear repair failed")
	}
	fresh := mustItem(t, db, reel.GUID)
	part, _ := snapshotPart(t, srv, snapshotFields(t, fresh.FieldsJSON), reelPartCatGear)
	if reelChildDurability(part) != 1 {
		t.Error("tiny wear was left unrepaired")
	}
	if part.Fields[fieldIdxByCS(part.Class, csCreated)].U64() == 0 {
		t.Error("repaired part version was not refreshed")
	}
	p, _ := db.GetProfile(ctx, 1000001)
	if p.Silver != 9900 {
		t.Errorf("balance=%d want 9900", p.Silver)
	}
	if _, ok := hWorkshopOrder(c); !ok {
		t.Fatal("repeat failed")
	}
	p, _ = db.GetProfile(ctx, 1000001)
	if p.Silver != 9900 {
		t.Fatal("repeat charged again")
	}
}
