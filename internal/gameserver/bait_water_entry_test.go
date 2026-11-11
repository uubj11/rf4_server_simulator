package gameserver

// 窝料/PVA 的消耗时机：抛竿只记录状态，14/7 首次出现“在水中”动画位才扣 1 个。
// 朝岸上抛竿没有入水位，不应扣除。

import (
	"testing"

	"rf4server/internal/logging"
	"rf4server/internal/proto"
)

func TestGroundbaitAndPvaConsumeOnWaterEntryOnly(t *testing.T) {
	for _, itemID := range []string{"feedcompleted", "pva_stick_s"} {
		t.Run(itemID, func(t *testing.T) {
			s, db := newRigTestServer(t)
			ctx := t.Context()
			const uid = int64(1000001)
			rod := baitTestRod(t, s)
			sess := newProbeSess(s)

			stack, err := s.CreateItem(ctx, uid, itemID, "backpack", 60)
			if err != nil {
				t.Fatal(err)
			}
			res := s.rigSetComponent(ctx, uid, rod, 0, stack.GUID, 0,
				rigSetOpts{ParentType: familyRod, EnforceCategory: true, SplitStack: true})
			if res.Rejected || res.MountedGUID == "" {
				t.Fatalf("挂载失败: %+v", res)
			}
			mounted := res.MountedGUID
			if row, err := db.GetItemByGUID(ctx, stack.GUID); err != nil || row == nil || row.Qty != 59 {
				t.Fatalf("挂载后来源堆应剩 59，实得 %+v err=%v", row, err)
			}

			// 抛竿完成：只进入等待入水状态，不应扣减。
			st := s.stateFor(uid)
			st.mu.Lock()
			st.GearGuid = rod
			st.mu.Unlock()
			if !s.scheduleBite(sess, st) {
				t.Fatal("排咬钩失败")
			}
			if row, _ := db.GetItemByGUID(ctx, mounted); row == nil {
				t.Fatal("抛竿阶段不得消耗窝料/PVA")
			}

			tick := func(anim uint64) {
				t.Helper()
				fields := make([]proto.Field, 7)
				fields[5] = proto.Field{Op: "U32", Num: ptrU64(anim)}
				fields[6] = proto.Field{Op: "STR", Str: strPtr("loc12_01")}
				if _, ok := hFishingStep(&Ctx{Sess: sess, Main: 14, Sub: 7,
					Tree:     proto.Args524(proto.Obj("lobemkloehp", 0, fields...)),
					DecodeOK: true, Log: logging.Discard()}); !ok {
					t.Fatal("14/7 未处理")
				}
			}
			// 未入水（空中/岸上）帧不扣。
			tick(0)
			if row, _ := db.GetItemByGUID(ctx, mounted); row == nil {
				t.Fatal("未入水时不得消耗窝料/PVA")
			}

			// 首次入水位出现：笼里这堆不再是“永远 1 份”，而是从背包整叠补到 20 份，
			// 之后每抛一次消耗笼里 1 份——先消耗本堆叠，再动背包。
			tick(0x400)
			if row, err := db.GetItemByGUID(ctx, mounted); err != nil || row == nil || row.Qty != 20 {
				t.Fatalf("自动补料后笼里应堆到 20 份，实得 %+v err=%v", row, err)
			}
			if row, err := db.GetItemByGUID(ctx, stack.GUID); err != nil || row == nil || row.Qty != 40 {
				t.Fatalf("补满 20 份应从背包扣 19，来源堆实得 %+v err=%v", row, err)
			}
			// 同一杆持续在水中不会重复补。
			tick(0x400)
			if row, err := db.GetItemByGUID(ctx, stack.GUID); err != nil || row == nil || row.Qty != 40 {
				t.Fatalf("同一杆只应补一次，来源堆实得 %+v err=%v", row, err)
			}
		})
	}
}
