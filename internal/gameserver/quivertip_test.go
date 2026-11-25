package gameserver

import (
	"context"
	"reflect"
	"testing"

	"rf4server/internal/proto"
)

func TestSetQuivertipRefreshesEquippedRod(t *testing.T) {
	t.Run("Feeder", func(t *testing.T) {
		testEquippedRodTipSequence(t, "feeder_10125_FD360", []string{"kam_quiv_0.5", "kam_quiv_1.0", "kam_quiv_1.5"})
	})
	t.Run("Picker", func(t *testing.T) {
		testEquippedRodTipSequence(t, "picker_10251_f98mh", []string{"kgf_quiv_pkr_0.25", "kgf_quiv_pkr_0.5", "kgf_quiv_pkr_0.75", "kgf_quiv_pkr_1.0", "kgf_quiv_pkr_0.25"})
	})
}

func testEquippedRodTipSequence(t *testing.T, model string, tips []string) {
	t.Helper()
	srv, db := newRigTestServer(t)
	sess, gc := newTunnelProbe(t, srv)
	ctx := context.Background()
	create := func(id string) string {
		t.Helper()
		row, err := srv.CreateItem(ctx, sess.Userid, id, "backpack", 1)
		if err != nil {
			t.Fatal(err)
		}
		return row.GUID
	}
	rod := create(model)
	other := create(model)
	reel := create("bclp_5004")
	mountGearForTest(t, srv, sess, rod, reel)
	mountGearForTest(t, srv, sess, rod, create("c_hook_20712"))

	decode := func(body []byte) *proto.Value {
		t.Helper()
		v, n, err := proto.DecodeValue(body, 0, srv.Table())
		if err != nil || n != len(body) {
			t.Fatalf("解码物品快照: n=%d len=%d err=%v", n, len(body), err)
		}
		return v
	}
	detail := func(guid string) *proto.Value {
		t.Helper()
		body, ok := hItemDetails(nativeInventoryCtx(srv, 0, proto.Guid(guid)))
		if !ok {
			t.Fatal("4/5 未处理")
		}
		list := decode(body)
		if list.ArgCount() != 1 {
			t.Fatalf("4/5 物品数=%d", list.ArgCount())
		}
		return list.Items[0]
	}
	before, untouched := detail(rod), detail(other)
	check := func(list *proto.Value, tip string) uint64 {
		t.Helper()
		if list == nil || list.Code == nil || *list.Code != "61" || list.ArgCount() != 1 {
			t.Fatal("更新段不是单件物品 args(61)")
		}
		v := list.Items[0]
		if v.Class != before.Class || v.Field(10).Hex != rod {
			t.Fatal("更新了错误的物品")
		}
		if got := v.Field(8).Str; got == nil || *got != tip {
			t.Fatalf("竿稍=%q，应为 %q", v.Fields[8].StrOr(), tip)
		}
		if v.Field(4).Hex != reel || !reflect.DeepEqual(v.Field(5).Val, before.Field(5).Val) {
			t.Fatal("渔轮或钓组改变")
		}
		return u64Of(v.Field(16).Num)
	}
	version := u64Of(before.Field(16).Num)
	for _, tip := range tips {
		c := &Ctx{Sess: sess, Main: 17, Sub: 5, Tree: rigRequestTree(t, proto.U32(0), proto.Guid(rod), proto.StrVal(tip)), ExpectResponse: true, DecodeOK: true, Log: sess.log}
		body, ok := hSetQuivertip(c)
		if !ok {
			t.Fatal("17/5 未处理")
		}
		if stored, _ := rodTipOf(t, db, rod); stored != tip {
			t.Fatalf("落库竿稍=%q，应为 %q", stored, tip)
		}
		resp := decode(body)
		if resp.Class != "dlbahppbdad" {
			t.Fatalf("响应不是 800: %s", resp.Class)
		}
		rv := check(rigSeg(resp, 1), tip)
		if rv <= version {
			t.Fatalf("响应版本 %d 未超过 %d", rv, version)
		}
		if len(c.afterResponse) != 1 {
			t.Fatal("缺少回包后的竿模型刷新")
		}
		c.afterResponse[0]()
		p := awaitPushEnvelope(t, gc)
		if p.Main != 4 || p.Sub != 22 {
			t.Fatalf("意外推送 %d/%d", p.Main, p.Sub)
		}
		version = check(decode(p.Body), tip)
		if version <= rv {
			t.Fatalf("刷新版本 %d 未超过响应 %d", version, rv)
		}
	}
	final := detail(rod)
	if tip := final.Field(8).Str; tip == nil || *tip != tips[len(tips)-1] || !reflect.DeepEqual(final.Field(5).Val, before.Field(5).Val) || final.Field(4).Hex != reel {
		t.Fatal("新会话 4/5 未保留最后选择或装配")
	}
	if setting, err := db.GetRodSetting(ctx, rod, "quivertip"); err != nil || setting != tips[len(tips)-1] {
		t.Fatalf("最终设置=%q err=%v", setting, err)
	}
	if !reflect.DeepEqual(detail(other), untouched) {
		t.Fatal("另一根竿发生变化")
	}
}
