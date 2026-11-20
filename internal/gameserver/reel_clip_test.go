package gameserver

import (
	"context"
	"math"
	"testing"

	"rf4server/internal/logging"
	"rf4server/internal/proto"
)

func TestReelClipDefaultsOff(t *testing.T) {
	t.Parallel()
	rig := buildRig153("rig_FloatSimple", 1, nil, 100)
	if got := rigClipOf(rig); got != 0 {
		t.Fatalf("默认轮管卡应为关闭，实得 %d", got)
	}
}

func TestReelClipRoundTrip(t *testing.T) {
	t.Parallel()
	rig := buildRig153WithClip("rig_FloatSimple", 1, nil, 100, 42)
	if got := rigClipOf(rig); got != 42 {
		t.Fatalf("轮管卡读取失败，实得 %d", got)
	}
	if got := math.Float32frombits(castBaseDistanceBits(1)); math.Abs(float64(got-7.5461488)) > 1e-6 {
		t.Fatalf("轮管卡不应改变基准抛距，实得 %.7f", got)
	}
}

func TestParseRodGuidIntClipOff(t *testing.T) {
	t.Parallel()
	tree := proto.Args(proto.S("524"), proto.Guid("10000000000000000000000000000001"), proto.U32(0))
	guid, value := parseRodGuidInt(tree)
	if guid == "" || value != 0 {
		t.Fatalf("解析失败: guid=%q value=%d", guid, value)
	}
}

func TestReelClipRodSetting(t *testing.T) {
	t.Parallel()
	db := connectTestDB(t)
	srv := &Server{DB: db}
	guid := "10000000000000000000000000000001"
	if err := db.UpsertRodSetting(context.Background(), guid, reelClipSettingKey, "27"); err != nil {
		t.Fatal(err)
	}
	got, ok := srv.savedReelClipM(context.Background(), guid)
	if !ok || got != 27 {
		t.Fatalf("轮管卡设置读取失败: got=%d ok=%v", got, ok)
	}
}

func TestReelClipRestoresMissingRigState(t *testing.T) {
	t.Parallel()
	db := connectTestDB(t)
	srv := &Server{DB: db}
	seedShopRodDef(t, db)
	insertBareRod(t, db, testUID, testShopRodGUID, "feeder_10126_FD390", 5559)
	ctx := context.Background()
	if err := db.UpsertRodSetting(ctx, testShopRodGUID, reelClipSettingKey, "33"); err != nil {
		t.Fatal(err)
	}
	row, err := db.GetItemByGUID(ctx, testShopRodGUID)
	if err != nil || row == nil {
		t.Fatalf("读取竿实例失败: %v", err)
	}
	if got := rigClipOf(srv.defaultRig153(ctx, row)); got != 33 {
		t.Fatalf("缺失 f5 时轮管卡未恢复，实得 %d", got)
	}
	if !srv.rodRigSetClipRow(ctx, row, 18) {
		t.Fatal("写入竿实例轮管卡失败")
	}
	row, err = db.GetItemByGUID(ctx, testShopRodGUID)
	if err != nil || row == nil {
		t.Fatalf("重读竿实例失败: %v", err)
	}
	fields, err := FieldsFromJSON(row.FieldsJSON)
	if err != nil {
		t.Fatal(err)
	}
	idx := fieldIdxByCS(row.WireClass, csRigObject)
	if idx < 0 || idx >= len(fields) || rigClipOf(fields[idx].Val) != 18 {
		t.Fatal("竿实例 f5 未持久化轮管卡")
	}
}

// 17/13 是无回包命令，设卡/清卡落库后必须立即推回最新竿树；客户端贴卡
// 截停读取 153.f5，不能等到下一次登录或其它操作才刷新。
func TestRodSetClipPersistsAndPushesUpdatedTree(t *testing.T) {
	t.Parallel()
	srv, db := newRigTestServer(t)
	sess, gc := newTunnelProbe(t, srv)
	ctx := context.Background()
	rod, err := srv.CreateItem(ctx, sess.Userid, "feeder_10125_FD360", "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	srv.ensureRig(ctx, sess.Userid, rod.GUID)

	for _, tc := range []struct {
		name string
		clip uint32
		want int64
	}{
		{name: "set", clip: 18, want: 18},
		{name: "clear", clip: 0, want: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tree := proto.Args524(proto.Guid(rod.GUID), proto.U32(tc.clip))
			if _, handled := hRodSetClip(&Ctx{Sess: sess, Main: 17, Sub: 13, Tree: tree,
				DecodeOK: true, ExpectResponse: false, Log: logging.Discard()}); !handled {
				t.Fatal("17/13 未处理")
			}

			if got, ok := srv.savedReelClipM(ctx, rod.GUID); !ok || got != tc.want {
				t.Fatalf("轮管卡设置持久化错误: got=%d ok=%v want=%d", got, ok, tc.want)
			}
			row, err := db.GetItemByGUID(ctx, rod.GUID)
			if err != nil || row == nil {
				t.Fatalf("读取竿实例失败: %v", err)
			}
			if got := rigClipOf(rigStoredF5(t, row)); got != tc.want {
				t.Fatalf("竿实例 153.f5 卡米=%d want=%d", got, tc.want)
			}

			push := awaitPushEnvelope(t, gc)
			if push.Main != 4 || push.Sub != 22 {
				t.Fatalf("17/13 后推送=%d/%d want 4/22", push.Main, push.Sub)
			}
			dec, _, err := proto.DecodeValue(push.Body, 0, srv.Table())
			if err != nil {
				t.Fatalf("解码 4/22 失败: %v", err)
			}
			if dec.Kind != proto.KindArgs || dec.Code == nil || *dec.Code != "61" || len(dec.Items) != 1 {
				t.Fatalf("4/22 形状异常: kind=%s code=%v len=%d", dec.Kind, dec.Code, len(dec.Items))
			}
			item := dec.Items[0]
			idx := fieldIdxByCS(item.Class, csRigObject)
			if idx < 0 || idx >= len(item.Fields) || item.Fields[idx].Val == nil {
				t.Fatalf("推送竿树缺少 153.f5: class=%s idx=%d", item.Class, idx)
			}
			if got := rigClipOf(item.Fields[idx].Val); got != tc.want {
				t.Fatalf("推送 153.f5 卡米=%d want=%d", got, tc.want)
			}
		})
	}
}
