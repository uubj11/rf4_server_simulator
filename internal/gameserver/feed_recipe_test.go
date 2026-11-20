package gameserver

import (
	"reflect"
	"testing"

	"rf4server/internal/proto"
	"rf4server/internal/store"
)

// 库存接口必须把旧存档的用料转换为非空配方数组，且读取不能改写库存。
func TestFeedRecipeLegacyInventory(t *testing.T) {
	s, db := newRigTestServer(t)
	ctx := t.Context()
	const uid = int64(1000001)
	for _, itemID := range []string{"feed_dry", "feedcompleted"} {
		t.Run(itemID, func(t *testing.T) {
			row, err := s.CreateItem(ctx, uid, itemID, "backpack", 60)
			if err != nil {
				t.Fatal(err)
			}
			gbRecordRecipe(t, s, row.GUID, []string{
				"mix_groundcrackers", "feedadds_cookies", "feedadds_biscuit", "cb_attr_a",
			})
			body, ok := hItemDetails(economyTestCtx(s, uid, proto.Args524(
				proto.U32(0), proto.GuidArr([]string{row.GUID}),
			)))
			if !ok {
				t.Fatal("库存请求未处理")
			}
			response := decodeTreeValue(t, s, body)
			if len(response.Items) != 1 || response.Items[0].TypeID != 120 {
				t.Fatalf("库存未返回成品窝料: %+v", response)
			}
			assertFeedIngredients(t, response.Items[0], [][]string{
				{"mix_groundcrackers"}, {"cb_attr_a"}, {"feedadds_cookies", "feedadds_biscuit"},
			})
			after, err := db.GetItemByGUID(ctx, row.GUID)
			if err != nil || after.FieldsJSON != row.FieldsJSON || after.Qty != 60 {
				t.Fatalf("读取配方改变了库存: %+v, %v", after, err)
			}
		})
	}
}

func assertFeedIngredients(t *testing.T, feed *proto.Value, want [][]string) {
	t.Helper()
	if feed == nil || feed.Kind != proto.KindObj || feed.TypeID != 120 {
		t.Fatalf("缺少窝料配方对象: %+v", feed)
	}
	for idx, code := range []string{"50", "52", "51"} {
		v := feed.Fields[idx].Val
		if v == nil || v.Kind != proto.KindArgs || v.Code == nil || *v.Code != code {
			t.Fatalf("f%d 期望 args %s，实得 %+v", idx, code, v)
		}
		got := []string{}
		for _, elem := range v.Items {
			if elem == nil || elem.TypeID != []uint32{121, 123, 122}[idx] || elem.Fields[0].Str == nil {
				t.Fatalf("f%d 元素类型错误: %+v", idx, elem)
			}
			got = append(got, *elem.Fields[0].Str)
		}
		if !reflect.DeepEqual(got, want[idx]) {
			t.Fatalf("f%d 用料=%v，期望 %v", idx, got, want[idx])
		}
	}
}

func feedTestSeed(t *testing.T, s *Server, uid int64, itemID string, qty int64, mats []string) *store.Item {
	t.Helper()
	row, err := s.prepareNewItemWithCount(t.Context(), uid, itemID, "backpack", qty, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.DB.InsertItem(t.Context(), row); err != nil {
		t.Fatal(err)
	}
	gbRecordRecipe(t, s, row.GUID, mats)
	return row
}

func TestFeedRecipeEmptyAndLegacyPVA(t *testing.T) {
	s, _ := newRigTestServer(t)
	for _, itemID := range []string{"feed_dry", "feedcompleted", "pva_stick_s"} {
		t.Run(itemID, func(t *testing.T) {
			row := feedTestSeed(t, s, 1000001, itemID, 60, nil)
			// 旧 PVA 只记录干混合定义名，不能从其他同名实例臆造配方。
			if itemID == "pva_stick_s" {
				feedTestSeed(t, s, 1000001, "feed_dry", 60, []string{"mix_combi"})
				gbRecordRecipe(t, s, row.GUID, []string{"feed_dry", "hvz_pva_material"})
			}
			tree, err := s.ItemToTree(t.Context(), row)
			if err != nil {
				t.Fatal(err)
			}
			body, err := proto.EncodeValue(tree, s.Table())
			if err != nil {
				t.Fatal(err)
			}
			tree = decodeTreeValue(t, s, body)
			if itemID == "pva_stick_s" {
				tree = tree.Fields[1].Val
			}
			assertFeedIngredients(t, tree, [][]string{{}, {}, {}})
		})
	}
}

func TestFeedRecipePVACapturesSelectedInstance(t *testing.T) {
	db := openRecipeTestDB(t)
	s := newSkillsCraftServer(t, db)
	ctx := t.Context()
	const uid = int64(1000001)
	if _, err := db.EnsureProfile(ctx, uid); err != nil {
		t.Fatal(err)
	}
	rod := baitTestRod(t, s)
	first := feedTestSeed(t, s, uid, "feed_dry", 1, []string{"mix_groundcrackers", "feedadds_cookies", "cb_attr_a"})
	second := feedTestSeed(t, s, uid, "feed_dry", 1, []string{"mix_groundpeas", "feedadds_corn", "aroma_vanila"})
	net := feedTestSeed(t, s, uid, "hvz_pva_material", 2, nil)
	for i, input := range []*store.Item{second, first} {
		want := [][][]string{
			{{"mix_groundpeas"}, {"aroma_vanila"}, {"feedadds_corn"}},
			{{"mix_groundcrackers"}, {"cb_attr_a"}, {"feedadds_cookies"}},
		}[i]
		body, ok := hCraft(economyTestCtx(s, uid, craftTreeWithSlots("craft_feed_pva_s", []craftSlot{
			{ItemID: "craft_mat_feed_dry", GUID: input.GUID},
			{ItemID: "craft_feed_pva_material", GUID: net.GUID},
		}, 1)))
		if !ok {
			t.Fatal("制作请求未处理")
		}
		response := decodeTreeValue(t, s, body)
		if u64Of(response.Fields[0].Num) != 1 {
			t.Fatal("制作 PVA 失败")
		}
		if row, _ := db.GetItemByGUID(ctx, input.GUID); row != nil {
			t.Fatal("原料应已耗尽")
		}
		var pvaGUID string
		for _, v := range response.Fields[6].Val.Items {
			if v.TypeID == 145 {
				assertFeedIngredients(t, v.Fields[1].Val, want)
				pvaGUID = v.Fields[6].Hex
			}
		}
		if pvaGUID == "" {
			t.Fatal("制作响应缺少 PVA")
		}
		// 原料配方记录也删除后，仍须从 PVA 自身快照恢复。
		if _, err := db.SQL.Exec(`DELETE FROM groundbait_recipe WHERE instance_guid=?`, input.GUID); err != nil {
			t.Fatal(err)
		}
		row, err := db.GetItemByGUID(ctx, pvaGUID)
		if err != nil || row.Qty != 60 {
			t.Fatalf("PVA 产量错误: %+v %v", row, err)
		}
		stored, err := FieldsFromJSON(row.FieldsJSON)
		if err != nil {
			t.Fatal(err)
		}
		assertFeedIngredients(t, stored[1].Val, want)
		mounted := s.rigSetComponent(ctx, uid, rod, catPVAStick, pvaGUID, 0,
			rigSetOpts{ParentType: familyRod, EnforceCategory: true, SplitStack: true})
		if mounted.Rejected || mounted.MountedGUID == "" {
			t.Fatalf("挂载 PVA 失败: %+v", mounted)
		}
		child, err := db.GetItemByGUID(ctx, mounted.MountedGUID)
		if err != nil {
			t.Fatal(err)
		}
		tree, err := s.ItemToTree(ctx, child)
		if err != nil {
			t.Fatal(err)
		}
		assertFeedIngredients(t, tree.Fields[1].Val, want)
		s.rigRemoveItemComponent(ctx, uid, rod, mounted.MountedGUID)
	}
}

func TestFeedRecipePVAFailureRollsBack(t *testing.T) {
	db := openRecipeTestDB(t)
	s := newSkillsCraftServer(t, db)
	const uid = int64(1000001)
	if _, err := db.EnsureProfile(t.Context(), uid); err != nil {
		t.Fatal(err)
	}
	input := feedTestSeed(t, s, uid, "feed_dry", 1, []string{"mix_combi"})
	if craftOKSlots(t, s, uid, "craft_feed_pva_s", []craftSlot{
		{ItemID: "craft_mat_feed_dry", GUID: input.GUID},
		{ItemID: "craft_feed_pva_material", GUID: "00112233445566778899aabbccddeeff"},
	}, 1) {
		t.Fatal("缺网袋不能制作")
	}
	after, err := db.GetItemByGUID(t.Context(), input.GUID)
	if err != nil || after == nil || after.Qty != 1 || after.FieldsJSON != input.FieldsJSON {
		t.Fatalf("失败后原料未回滚: %+v %v", after, err)
	}
	if countOfItem(t, s, t.Context(), uid, "pva_stick_s") != 0 {
		t.Fatal("失败后仍产出 PVA")
	}
}

func TestFeedRecipePreservesSnapshot(t *testing.T) {
	s, _ := newRigTestServer(t)
	row := feedTestSeed(t, s, 1000001, "feed_dry", 60, []string{"mix_combi", "feedadds_corn"})
	tree, err := s.ItemToTree(t.Context(), row)
	if err != nil {
		t.Fatal(err)
	}
	tree.Fields[2].Val.Items[0].Fields[1].Str = proto.S("我的添加剂")
	row.FieldsJSON, err = FieldsToJSON(tree.Fields)
	if err != nil {
		t.Fatal(err)
	}
	// 已保存的快照（包括有意为空的数组）优先于后续用料记录。
	gbRecordRecipe(t, s, row.GUID, []string{"mix_groundpeas", "feedadds_cookies", "cb_attr_a"})
	got, err := s.ItemToTree(t.Context(), row)
	if err != nil {
		t.Fatal(err)
	}
	assertFeedIngredients(t, got, [][]string{{"mix_combi"}, {}, {"feedadds_corn"}})
	if *got.Fields[2].Val.Items[0].Fields[1].Str != "我的添加剂" {
		t.Fatal("已保存的添加剂名称被覆盖")
	}
	// 避免通过坏类型数组再次触发客户端强转错误。
	fields := got.Fields
	fields[0].Val = proto.Args(proto.S("61"))
	row.FieldsJSON, err = FieldsToJSON(fields)
	if err != nil {
		t.Fatal(err)
	}
	got, err = s.ItemToTree(t.Context(), row)
	if err != nil {
		t.Fatal(err)
	}
	assertFeedIngredients(t, got, [][]string{{"mix_groundpeas"}, {}, {"feedadds_corn"}})
	for _, tc := range []struct{ element, code string }{{"121", "50"}, {"122", "51"}, {"123", "52"}} {
		if loadArgcode().ByElem[tc.element] != tc.code {
			t.Fatalf("缺少元素 %s 的真实注册编号 %s", tc.element, tc.code)
		}
	}
}

func TestFeedRecipePVAVending(t *testing.T) {
	db := openRecipeTestDB(t)
	s := newSkillsCraftServer(t, db)
	const uid = int64(1000001)
	if _, err := db.EnsureProfile(t.Context(), uid); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL.Exec(`UPDATE profile SET silver=100000 WHERE uid=?`, uid); err != nil {
		t.Fatal(err)
	}
	if !vendOK(t, s, uid, "craft_feed_pva_s_vend", []craftSlot{
		{ItemID: "craft_feed_generic_mix_1", Species: "mix_groundpeas"},
		{ItemID: "craft_feed_generic_add_1", Species: "feedadds_corn"},
		{ItemID: "craft_feed_generic_add_2", Species: "feedadds_cookies"},
		{ItemID: "craft_feed_generic_add_3", Species: "feedadds_bloodworms"},
		{ItemID: "craft_feed_pva_add_4", Species: "feedadds_peas"},
		{ItemID: "craft_feed_generic_aroma_1", Species: "aroma_vanila"},
		{ItemID: "craft_feed_pva_material", Species: "hvz_pva_material"},
	}, 1) {
		t.Fatal("贩卖机制作失败")
	}
	rows, err := db.ListItemsByContainers(t.Context(), uid, []string{"backpack"})
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row.ItemID != "pva_stick_s" {
			continue
		}
		fields, err := FieldsFromJSON(row.FieldsJSON)
		if err != nil {
			t.Fatal(err)
		}
		assertFeedIngredients(t, fields[1].Val, [][]string{{"mix_groundpeas"}, {"aroma_vanila"},
			{"feedadds_corn", "feedadds_cookies", "feedadds_bloodworms", "feedadds_peas"}})
		return
	}
	t.Fatal("没有 PVA 产物")
}
