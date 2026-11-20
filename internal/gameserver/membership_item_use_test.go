package gameserver

import (
	"context"
	"testing"
	"time"

	"rf4server/internal/logging"
	"rf4server/internal/proto"
)

// membershipUseTestCtx 构造一次 4/8 使用物品请求（args(524)[storage,guid]）。
func membershipUseTestCtx(t *testing.T, s *Server, guid string) *Ctx {
	t.Helper()
	sess := newProbeSess(s)
	sess.Token = "value-item-test"
	tree := proto.Args524(proto.U32(0), proto.Guid(guid))
	body, err := proto.EncodeValue(tree, s.Table())
	if err != nil {
		t.Fatal(err)
	}
	return &Ctx{Sess: sess, Main: 4, Sub: 8, CallID: 1, Tree: tree, Body: body,
		DecodeOK: true, ExpectResponse: true, Log: logging.Discard(), replyCapture: &ctxReplyCapture{}}
}

// 会员物品：使用后续期会员天数并消失。
func TestMembershipItemAddsDaysAndDisappears(t *testing.T) {
	srv, db := newEconomyTxTestServer(t)
	ctx := context.Background()
	const uid = int64(1000001)
	if _, err := db.EnsureProfile(ctx, uid); err != nil {
		t.Fatal(err)
	}
	before, _ := db.GetProfile(ctx, uid)
	row, err := srv.CreateItem(ctx, uid, "premium_3d", "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	useCtx := membershipUseTestCtx(t, srv, row.GUID)
	body, handled := hItemUse(useCtx)
	if !handled || len(body) == 0 || len(useCtx.afterResponse) == 0 {
		t.Fatalf("4/8 会员时长物品未完成: handled=%v body=%v pushes=%d", handled, body, len(useCtx.afterResponse))
	}
	after, _ := db.GetProfile(ctx, uid)
	if after.Membership != 1 {
		t.Fatalf("会员类型应为 1，实得 %d", after.Membership)
	}
	wantMin := time.Now().Unix() + 3*86400 - 5
	if after.MembershipExp < wantMin {
		t.Fatalf("会员应至少续期 3 天，exp=%d want>=%d", after.MembershipExp, wantMin)
	}
	if before.MembershipExp > 0 && after.MembershipExp < before.MembershipExp {
		t.Fatalf("续期不应缩短原有到期时间: %d -> %d", before.MembershipExp, after.MembershipExp)
	}
	got, err := db.GetItemByGUID(ctx, row.GUID)
	if err == nil && got != nil && got.Qty > 0 {
		t.Fatalf("会员物品用后应消失，实得 qty=%d", got.Qty)
	}
}

// 非会员品类不应被本 handler 拦截。
func TestMembershipItemIgnoresOtherCategories(t *testing.T) {
	srv, db := newEconomyTxTestServer(t)
	ctx := context.Background()
	const uid = int64(1000001)
	if _, err := db.EnsureProfile(ctx, uid); err != nil {
		t.Fatal(err)
	}
	// 造一件 category 27（普通消耗品）物品
	row, err := srv.CreateItem(ctx, uid, "worm_01", "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	useCtx := membershipUseTestCtx(t, srv, row.GUID)
	if _, handled := srv.useMembershipItem(useCtx); handled {
		t.Fatal("非银币/会员类别不应被 useValueItem 拦截")
	}
}

func TestMembershipItemRenewalPermanentAndRollback(t *testing.T) {
	t.Run("active membership extends from current expiry", func(t *testing.T) {
		srv, db := newEconomyTxTestServer(t)
		ctx := context.Background()
		uid := newProbeSess(srv).Userid
		if _, err := db.EnsureProfile(ctx, uid); err != nil {
			t.Fatal(err)
		}
		oldExpiry := time.Now().Unix() + 10*86400
		if _, err := db.SQL.Exec(`UPDATE profile SET membership=1,membership_exp=? WHERE uid=?`, oldExpiry, uid); err != nil {
			t.Fatal(err)
		}
		row, err := srv.CreateItem(ctx, uid, "premium_7d", "backpack", 1)
		if err != nil {
			t.Fatal(err)
		}
		useCtx := membershipUseTestCtx(t, srv, row.GUID)
		body, handled := hItemUse(useCtx)
		if !handled || len(body) == 0 || len(useCtx.afterResponse) != 1 {
			t.Fatalf("会员物品使用失败: handled=%v body=%x pushes=%d", handled, body, len(useCtx.afterResponse))
		}
		decoded, _, err := proto.DecodeValue(body, 0, srv.Table())
		if err != nil || decoded.Class != "dlbahppbdad" {
			t.Fatalf("800 快照形状异常: class=%v err=%v", decoded, err)
		}
		p, err := db.GetProfile(ctx, uid)
		if err != nil {
			t.Fatal(err)
		}
		if p.Membership != 1 || p.MembershipExp < oldExpiry+7*86400-2 || p.MembershipExp > oldExpiry+7*86400+2 {
			t.Fatalf("活跃会员未从原到期时间顺延: %+v", p)
		}
		if got, err := db.GetItemByGUID(ctx, row.GUID); err == nil && got != nil && got.Qty > 0 {
			t.Fatalf("会员物品使用后应消失: qty=%d", got.Qty)
		}
	})

	t.Run("permanent membership rejects and keeps item", func(t *testing.T) {
		srv, db := newEconomyTxTestServer(t)
		ctx := context.Background()
		uid := newProbeSess(srv).Userid
		if _, err := db.EnsureProfile(ctx, uid); err != nil {
			t.Fatal(err)
		}
		if _, err := db.SQL.Exec(`UPDATE profile SET membership=2,membership_exp=0 WHERE uid=?`, uid); err != nil {
			t.Fatal(err)
		}
		row, err := srv.CreateItem(ctx, uid, "premium_3d", "backpack", 1)
		if err != nil {
			t.Fatal(err)
		}
		useCtx := membershipUseTestCtx(t, srv, row.GUID)
		if _, handled := hItemUse(useCtx); !handled {
			t.Fatal("永久会员物品未进入会员处理分支")
		}
		if useCtx.replyCapture == nil || !useCtx.replyCapture.present {
			t.Fatal("永久会员使用应返回失败")
		}
		if got, err := db.GetItemByGUID(ctx, row.GUID); err != nil || got == nil || got.Qty != 1 {
			t.Fatalf("永久会员使用不得扣物: %+v %v", got, err)
		}
		p, _ := db.GetProfile(ctx, uid)
		if p.Membership != 2 || p.MembershipExp != 0 {
			t.Fatalf("永久会员状态被修改: %+v", p)
		}
	})

	t.Run("transaction failure rolls back membership and item", func(t *testing.T) {
		srv, db := newEconomyTxTestServer(t)
		ctx := context.Background()
		uid := newProbeSess(srv).Userid
		if _, err := db.EnsureProfile(ctx, uid); err != nil {
			t.Fatal(err)
		}
		row, err := srv.CreateItem(ctx, uid, "premium_3d", "backpack", 1)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.SQL.Exec(`CREATE TRIGGER fail_membership_txn BEFORE INSERT ON txn WHEN NEW.kind='membership_item' BEGIN SELECT RAISE(ABORT,'membership probe failure'); END`); err != nil {
			t.Fatal(err)
		}
		before, err := db.GetProfile(ctx, uid)
		if err != nil {
			t.Fatal(err)
		}
		useCtx := membershipUseTestCtx(t, srv, row.GUID)
		if _, handled := hItemUse(useCtx); !handled {
			t.Fatal("会员物品未进入会员处理分支")
		}
		if useCtx.replyCapture == nil || !useCtx.replyCapture.present {
			t.Fatal("流水失败应返回失败响应")
		}
		after, err := db.GetProfile(ctx, uid)
		if err != nil {
			t.Fatal(err)
		}
		if after.Membership != before.Membership || after.MembershipExp != before.MembershipExp {
			t.Fatalf("会员状态未回滚: %+v -> %+v", before, after)
		}
		if got, err := db.GetItemByGUID(ctx, row.GUID); err != nil || got == nil || got.Qty != 1 {
			t.Fatalf("物品未回滚: %+v %v", got, err)
		}
	})
}
