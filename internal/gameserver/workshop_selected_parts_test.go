package gameserver

import (
	"math"
	"testing"

	"rf4server/internal/logging"
	"rf4server/internal/proto"
)

func TestWorkshopRepairOnlySelectedReelPart(t *testing.T) {
	for _, cat := range []int64{reelPartCatGear, reelPartCatWasher, reelPartCatBearing} {
		t.Run(map[int64]string{reelPartCatGear: "gear", reelPartCatWasher: "washer", reelPartCatBearing: "bearing"}[cat], func(t *testing.T) {
			srv, db := newStaticRigTestServer(t)
			reel, _ := seedWornWorkshopReel(t, srv, db, .8, .2, .5, .35)
			ctx := t.Context()
			if cat == reelPartCatWasher {
				fields := snapshotFields(t, reel.FieldsJSON)
				washer, _ := snapshotPart(t, srv, fields, reelPartCatWasher)
				setReelChildDurability(washer, 1) // 镜像完好，但实际制动力仅剩 20%。
				reel.FieldsJSON = snapshotJSON(t, fields)
				if err := db.UpdateItemFields(ctx, reel.GUID, reel.FieldsJSON, reel.Qty, reel.Durability, reel.Workmanship, reel.RigGUID); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := db.EnsureProfile(ctx, 1000001); err != nil {
				t.Fatal(err)
			}
			if err := db.UpdateBalance(ctx, 1000001, 10000, 0, 0); err != nil {
				t.Fatal(err)
			}
			before := snapshotFields(t, reel.FieldsJSON)
			_, def := snapshotPart(t, srv, before, cat)
			cost := map[int64]int64{reelPartCatGear: 100, reelPartCatWasher: 400, reelPartCatBearing: 250}[cat]
			c := &Ctx{Sess: newProbeSess(srv), Main: 21, Sub: 3, Tree: snapshotWorkshopRequestPart(reel.GUID, uint32(cat), def.ItemID, 0), Log: logging.Discard()}
			if _, ok := hWorkshopOrder(c); !ok {
				t.Fatal("选中部件修理失败")
			}
			fresh := mustItem(t, db, reel.GUID)
			for _, part := range srv.repairPartsOf(ctx, fresh) {
				if part.Cat == uint32(cat) && math.Abs(part.Cond-1) > 1e-6 {
					t.Fatal("所选件的实际完好度未恢复")
				}
			}
			after := snapshotFields(t, fresh.FieldsJSON)
			for _, other := range []int64{reelPartCatGear, reelPartCatWasher, reelPartCatBearing} {
				oldPart, _ := snapshotPart(t, srv, before, other)
				newPart, _ := snapshotPart(t, srv, after, other)
				if other != cat {
					if snapshotJSON(t, oldPart.Fields) != snapshotJSON(t, newPart.Fields) {
						t.Fatalf("未选择的部件 %d 被修改", other)
					}
				} else if math.Abs(reelChildDurability(newPart)-1) > 1e-6 {
					t.Fatal("所选部件未修好")
				}
			}
			if fresh.Durability >= 1 {
				t.Fatal("仍有磨损部件时，本体不能直接回满")
			}
			p, err := db.GetProfile(ctx, 1000001)
			if err != nil {
				t.Fatal(err)
			}
			if p.Silver != 10000-cost {
				t.Fatalf("扣费=%d，期望 %d", 10000-p.Silver, cost)
			}
			// 再提交已修好的部件，不能修复或收费覆盖剩余磨损。
			if _, ok := hWorkshopOrder(c); !ok {
				t.Fatal("重复修理应返回当前装备")
			}
			again := mustItem(t, db, reel.GUID)
			if again.FieldsJSON != fresh.FieldsJSON {
				t.Fatal("重复修理改变装备")
			}
			p, err = db.GetProfile(ctx, 1000001)
			if err != nil || p.Silver != 10000-cost {
				t.Fatal("重复修理额外扣费")
			}
			// 诊断仍应给未选部件各自的修理费。
			body, ok := hWorkshopDiag(&Ctx{Sess: newProbeSess(srv), Tree: proto.Args524(proto.Guid(reel.GUID)), Log: logging.Discard()})
			if !ok {
				t.Fatal("诊断失败")
			}
			diag, _, err := proto.DecodeValue(body, 0, srv.Table())
			if err != nil {
				t.Fatal(err)
			}
			for _, other := range []int64{reelPartCatGear, reelPartCatWasher, reelPartCatBearing} {
				part, _ := snapshotPart(t, srv, after, other)
				guid := part.Fields[fieldIdxByCS(part.Class, csGUID)].Hex
				want := map[int64]uint64{reelPartCatGear: 100, reelPartCatWasher: 400, reelPartCatBearing: 250}[other]
				if other == cat {
					want = 0
				}
				found := false
				for _, entry := range diag.Fields[1].Val.Items {
					if entry.Fields[0].Hex == guid {
						found = true
						if got := entry.Fields[3].Val.Fields[0].U64(); got != want {
							t.Fatalf("部件 %d 报价=%d want=%d", other, got, want)
						}
					}
				}
				if !found {
					t.Fatal("诊断缺少部件")
				}
			}
		})
	}
}
