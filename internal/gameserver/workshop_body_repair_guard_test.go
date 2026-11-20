package gameserver

import (
	"testing"

	"rf4server/internal/logging"
	"rf4server/internal/proto"
	"rf4server/internal/wiregen"
)

func TestWorkshopReelBodyRepairRejectsWornInternalParts(t *testing.T) {
	handlers := []struct {
		name string
		fn   Handler
	}{
		{"preview", hWorkshopPreview},
		{"confirm", hWorkshopOrder},
	}
	cases := []struct {
		name        string
		includePart bool
	}{
		{"body_only", false},
		{"body_with_gear", true},
	}
	for _, hc := range handlers {
		for _, tc := range cases {
			t.Run(hc.name+"/"+tc.name, func(t *testing.T) {
				srv, db := newStaticRigTestServer(t)
				reel, gearID := seedWornWorkshopReel(t, srv, db, .8, .2, .5, .35)
				ctx := t.Context()
				if _, err := db.EnsureProfile(ctx, 1000001); err != nil {
					t.Fatal(err)
				}
				if err := db.UpdateBalance(ctx, 1000001, 100000, 0, 0); err != nil {
					t.Fatal(err)
				}
				orders := []*proto.Value{
					wiregen.MustMakeObj("gcllbdkgkna", map[int]*proto.Value{
						0: proto.U32(uint32(gearCatReel)),
						1: proto.StrVal(reel.ItemID),
					}),
				}
				if tc.includePart {
					orders = append(orders, wiregen.MustMakeObj("gcllbdkgkna", map[int]*proto.Value{
						0: proto.U32(uint32(reelPartCatGear)),
						1: proto.StrVal(gearID),
					}))
				}
				request := proto.Args524(proto.StrVal("workshop_01"), wiregen.MustMakeObj("nfmkdcbnkhp", map[int]*proto.Value{
					0: proto.Guid(reel.GUID),
					1: proto.Args(proto.S("503"), orders...),
					2: proto.U32(0),
					3: proto.U64(0),
				}))
				c := &Ctx{Sess: newProbeSess(srv), Main: 21, Sub: 4, Tree: request, Log: logging.Discard()}
				if _, ok := hc.fn(c); ok {
					t.Fatal("worn internal parts must prevent body-only repair")
				}
				fresh := mustItem(t, db, reel.GUID)
				if fresh.FieldsJSON != reel.FieldsJSON || fresh.Durability != reel.Durability {
					t.Fatal("rejected order changed equipment")
				}
				profile, err := db.GetProfile(ctx, 1000001)
				if err != nil || profile.Silver != 100000 {
					t.Fatalf("rejected order changed balance: profile=%v err=%v", profile, err)
				}
			})
		}
	}
}
