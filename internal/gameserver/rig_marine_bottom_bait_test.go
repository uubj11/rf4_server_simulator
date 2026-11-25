package gameserver

import (
	"database/sql"
	"testing"

	"rf4server/internal/proto"
)

func TestMarineBottomBaitSlots(t *testing.T) {
	for _, rt := range []int64{55, 56} {
		t.Run(rigNameForType(rt), func(t *testing.T) { testMarineBottomBaitSlots(t, rt) })
	}
}

func testMarineBottomBaitSlots(t *testing.T, rt int64) {
	// 4.0.25034：17/2 上报钩 250/251 后，天然饵依次上报 350/351。

	s, db := newStaticRigTestServer(t)
	ctx := t.Context()
	const uid = 1000001
	rod, err := s.CreateItem(ctx, uid, "boatr_10222_80_100", "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	if !s.rigSetType(ctx, uid, rod.GUID, rt) {
		t.Fatal("rig type rejected")
	}
	mountArmHook(t, s, rod.GUID, 250)
	mountArmHook(t, s, rod.GUID, 251)
	check := func(first, second string) {
		t.Helper()
		row, err := db.GetItemByGUID(ctx, rod.GUID)
		if err != nil {
			t.Fatal(err)
		}
		stored := rigStoredF5(t, row)
		want := map[int64]string{}
		if first != "" {
			want[350] = first
		}
		if second != "" {
			want[351] = second
		}
		for _, f5 := range []*proto.Value{stored, s.liveRig153(ctx, row, stored)} {
			comps := rigTestCategoryComps(t, f5, catBait)
			if len(comps) != len(want) {
				t.Fatalf("f5 baits = %+v, want %v", comps, want)
			}
			for _, c := range comps {
				if want[c.Extra] != c.GUID || c.Ordinal != c.Extra-350 {
					t.Fatalf("f5 bait = %+v, want %v", c, want)
				}
			}
		}
		rows, err := db.ListRigComponents(ctx, rod.GUID)
		if err != nil {
			t.Fatal(err)
		}
		n := 0
		for _, r := range rows {
			if r.Slot == catBait {
				n++
				if want[r.Extra] != r.GUID || r.Ordinal != r.Extra-350 {
					t.Fatalf("projection = %+v, want %v", r, want)
				}
			}
		}
		if n != len(want) {
			t.Fatalf("projection has %d baits, want %d", n, len(want))
		}
	}
	source, first := baitTestMount(t, s, rod.GUID, "fb_bf_saida", 4, 350)
	check(first, "")
	mounted := s.rigSetComponent(ctx, uid, rod.GUID, 0, source, 351, rigSetOpts{ParentType: familyRod, EnforceCategory: true, SplitStack: true})
	second := mounted.MountedGUID
	if mounted.Rejected || second == "" || second == first {
		t.Fatal("same stack did not yield a separate second bait")
	}
	check(first, second)
	_, second = baitTestMount(t, s, rod.GUID, "fb_squid", 4, 351)
	s = newFishingRecoveryServer(t, db)
	check(first, second)
	if _, err := s.consumeBaitEvent(ctx, uid, rod.GUID, "first-bite", []string{first}, false); err != nil {
		t.Fatal(err)
	}
	check("", second)
	if _, err := s.reloadBaitForCast(ctx, uid, rod.GUID); err != nil {
		t.Fatal(err)
	}
	first = s.storedRigComponents(ctx, rod.GUID)[rigCompKey(catBait, 0)].GUID
	if first == "" {
		t.Fatal("first bait was not replenished")
	}
	check(first, second)
	if r := s.rigRemoveComponentInstance(ctx, uid, rod.GUID, catBait, "", 351); r == nil || r.CompGUID != second {
		t.Fatalf("detach second: %+v", r)
	}
	check(first, "")
}

// 旧档已保留两份不同实例，但两条 f2 都是 350。按旧数组次序恢复，不改玩家库。
func TestMarineBottomLegacyDuplicateBaitIndex(t *testing.T) {
	for _, rt := range []int64{55, 56} {
		t.Run(rigNameForType(rt), func(t *testing.T) { testMarineBottomLegacyDuplicateBaitIndex(t, rt) })
	}
}

func testMarineBottomLegacyDuplicateBaitIndex(t *testing.T, rt int64) {
	const first = "12345678901234567890123456789011"
	const second = "12345678901234567890123456789012"
	old := buildRig153(rigNameForType(rt), rt, nil, 100)
	old.Fields[2].Val.Items = []*proto.Value{
		rigCompObj(catBait, rigComp{GUID: first, Extra: 350}),
		rigCompObj(catBait, rigComp{GUID: second, Extra: 350}),
	}
	rebuilt := buildRig153Like(old, rigNameForType(rt), rt, rigCompsOf(old), 100, 0)
	items := rebuilt.Fields[2].Val.Items
	if len(items) != 2 || items[0].Fields[1].GuidHex() != first || items[0].Fields[2].U32() != 350 ||
		items[1].Fields[1].GuidHex() != second || items[1].Fields[2].U32() != 351 {
		t.Fatalf("旧档未恢复成两个独立饵位: %+v", items)
	}
}

func TestMarineSwitchUnloadsBaits(t *testing.T) {
	for _, pair := range [][2]int64{{55, 56}, {56, 55}} {
		from, to := pair[0], pair[1]
		t.Run(rigNameForType(from), func(t *testing.T) {
			ctx := t.Context()
			s, db := newStaticRigTestServer(t)
			const uid = 1000001
			rod, err := s.CreateItem(ctx, uid, "boatr_10222_80_100", "backpack", 1)
			if err != nil {
				t.Fatal(err)
			}
			if !s.rigSetType(ctx, uid, rod.GUID, from) {
				t.Fatal("set initial rig")
			}
			mountArmHook(t, s, rod.GUID, 250)
			mountArmHook(t, s, rod.GUID, 251)
			src, first := baitTestMount(t, s, rod.GUID, "fb_squid", 8, 350)
			second := s.rigSetComponent(ctx, uid, rod.GUID, 0, src, 351, rigSetOpts{ParentType: familyRod, EnforceCategory: true, SplitStack: true}).MountedGUID
			if second == "" || second == first {
				t.Fatal("missing distinct second bait")
			}
			// 第一钩还剩两份：从来源栈再移一份上钩，卸下时应退回两份。
			firstRow, err := db.GetItemByGUID(ctx, first)
			if err != nil {
				t.Fatal(err)
			}
			sourceRow, err := db.GetItemByGUID(ctx, src)
			if err != nil {
				t.Fatal(err)
			}
			s.setStackCount(ctx, firstRow, 2)
			s.setStackCount(ctx, sourceRow, sourceRow.Qty-1)
			if !s.rigSetType(ctx, uid, rod.GUID, from) || len(rigCompKeysOfCategory(s.storedRigComponents(ctx, rod.GUID), catBait)) != 2 {
				t.Fatal("reselecting the same rig unloaded bait")
			}
			// 旧钓组未完成的补饵也不能跨到新钓组。保留其他竿设置。
			for _, key := range []string{"rebait:1", "refill:" + rod.GUID + ":27:350", "unrelated"} {
				if err := db.UpsertRodSetting(ctx, rod.GUID, key, "1"); err != nil {
					t.Fatal(err)
				}
			}
			var released []*rigReleased
			if !s.rigSetType(ctx, uid, rod.GUID, to, &released) {
				t.Fatal("switch failed")
			}
			restarted := newFishingRecoveryServer(t, db)
			if got := rigCompKeysOfCategory(restarted.storedRigComponents(ctx, rod.GUID), catBait); len(got) != 0 {
				t.Fatalf("切换后 f5 仍有 %d 份饵", len(got))
			}
			rows, err := db.ListRigComponents(ctx, rod.GUID)
			if err != nil {
				t.Fatal(err)
			}
			for _, r := range rows {
				if r.Slot == catBait {
					t.Fatal("projection retained bait")
				}
			}
			stack, err := db.GetItemByGUID(ctx, src)
			if err != nil || stack.Qty != 8 {
				t.Fatalf("refund: %+v %v", stack, err)
			}
			if len(released) != 4 || released[2].GUID != first || released[3].GUID != second || released[3].SourceTree == nil {
				t.Fatalf("refund notification: %+v", released)
			}
			snapshot := s.buildRigSnapshotForParent(ctx, rod.GUID, "", "", released)
			deleted := snapshot.Fields[2].Val
			if deleted == nil || len(deleted.Items) != 2 || deleted.Items[0].Hex != first || deleted.Items[1].Hex != second {
				t.Fatal("switch response did not remove both mounted instances")
			}
			if q, ok := itemStackQtyValue(stack.WireClass, released[3].SourceTree.Fields); !ok || q != 8 {
				t.Fatalf("source stack refresh quantity = %d, want 8", q)
			}
			for _, key := range []string{"rebait:1", "refill:" + rod.GUID + ":27:350", "bait_hook:" + first, "bait_hook:" + second} {
				if _, err := db.GetRodSetting(ctx, rod.GUID, key); err != sql.ErrNoRows {
					t.Fatalf("%s was not cleared: %v", key, err)
				}
			}
			if v, _ := db.GetRodSetting(ctx, rod.GUID, "unrelated"); v != "1" {
				t.Fatal("unrelated setting removed")
			}
		})
	}
}

func TestRigSwitchViaThirdTypeKeepsRodEquipment(t *testing.T) {
	s, db := newStaticRigTestServer(t)
	ctx := t.Context()
	const uid = 1000001
	rod, err := s.CreateItem(ctx, uid, "boatr_10222_80_100", "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	if !s.rigSetType(ctx, uid, rod.GUID, 55) {
		t.Fatal("initial rig")
	}
	_, reel := baitTestMount(t, s, rod.GUID, "bclp_5004", 1, 0)
	_, line := baitTestMount(t, s, rod.GUID, "braid_15001", 1, 0)
	_, shock := baitTestMount(t, s, rod.GUID, "leader_crafted_shock_200", 1, 175)
	for _, to := range []int64{58, 56, 57, 55} {
		_, leader := baitTestMount(t, s, rod.GUID, "mar_leader_40842", 1, 150)
		hook := mountArmHook(t, s, rod.GUID, 250)
		_, bait := baitTestMount(t, s, rod.GUID, "fb_squid", 4, 350)
		if !s.rigSetType(ctx, uid, rod.GUID, to) {
			t.Fatalf("switch to %d", to)
		}
		rows, err := db.ListRigComponents(ctx, rod.GUID)
		if err != nil {
			t.Fatal(err)
		}
		seen := map[string]bool{}
		for _, r := range rows {
			seen[r.GUID] = true
		}
		if !seen[shock] {
			t.Fatalf("切到 %d 后震子线丢失", to)
		}
		stored := rigTestCategoryComps(t, rigStoredF5(t, mustItem(t, db, rod.GUID)), catLead)
		if len(stored) != 1 || stored[0].GUID != shock || stored[0].Extra != 175 {
			t.Fatalf("切到 %d 后前导槽不符: %+v", to, stored)
		}
		if !seen[line] || !seen[reel] || seen[leader] || seen[hook] || seen[bait] {
			t.Fatalf("switch to %d: bad attachments %+v", to, rows)
		}
		for _, g := range []string{leader, hook} {
			r := mustItem(t, db, g)
			if r.Container != "backpack" || r.RigGUID != "" {
				t.Fatalf("not returned: %+v", r)
			}
		}
	}
}
