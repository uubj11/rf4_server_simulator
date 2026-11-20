package gameserver

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"rf4server/internal/proto"
	"rf4server/internal/tunnel"
)

func newTunnelProbe(t *testing.T, srv *Server) (*Session, *tunnel.Conn) {
	t.Helper()
	sess := newProbeSess(srv)
	sess.ID = 77

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	type hsRes struct {
		c   *tunnel.Conn
		err error
	}
	scCh := make(chan hsRes, 1)
	go func() {
		nc, aerr := ln.Accept()
		if aerr != nil {
			scCh <- hsRes{err: aerr}
			return
		}
		c, herr := tunnel.HandshakeServer(nc, "misc")
		scCh <- hsRes{c: c, err: herr}
	}()
	nc, err := net.DialTimeout("tcp", ln.Addr().String(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	gc, err := tunnel.HandshakeClient(nc, "misc")
	if err != nil {
		t.Fatal(err)
	}
	sr := <-scCh
	if sr.err != nil {
		t.Fatal(sr.err)
	}
	sess.tunnel = sr.c
	t.Cleanup(func() { _ = gc.Close(); _ = sr.c.Close() })
	return sess, gc
}

func awaitPushEnvelope(t *testing.T, gc *tunnel.Conn) *proto.Envelope {
	t.Helper()
	type result struct {
		env *proto.Envelope
		err error
	}
	ch := make(chan result, 1)
	go func() {
		_, _, payload, err := gc.ReadRecord()
		if err != nil {
			ch <- result{err: err}
			return
		}
		env, ok := proto.ParseEnvelope(payload)
		if !ok {
			ch <- result{err: fmt.Errorf("信封解析失败")}
			return
		}
		ch <- result{env: env}
	}()
	select {
	case r := <-ch:
		if r.err != nil {
			t.Fatalf("读推送失败: %v", r.err)
		}
		return r.env
	case <-time.After(3 * time.Second):
		t.Fatal("等待推送超时")
		return nil
	}
}

func assertNoPush(t *testing.T, gc *tunnel.Conn, timeout time.Duration, message string) {
	t.Helper()
	ch := make(chan error, 1)
	go func() {
		_, _, _, err := gc.ReadRecord()
		ch <- err
	}()
	select {
	case err := <-ch:
		if err != nil {
			t.Fatalf("%s，读取出错: %v", message, err)
		}
		t.Fatal(message)
	case <-time.After(timeout):
	}
}

// 未上钩离场（14/12 f0=1/2 窗口超时；f0=8 且未中过鱼=刺鱼瞬间钩没挂上）
// 官方语义静默：客户端模板表无「未上钩」类，客户端自己播游走动画；
// 服务端不得广播模板 7「鱼脱钩了」（鱼从未上钩，语义不成立）。
func TestFishUnhookedDepartureIsSilent(t *testing.T) {
	srv, _ := newRigTestServer(t)
	sess, gc := newTunnelProbe(t, srv)

	st := srv.stateFor(sess.Userid)
	st.mu.Lock()
	st.CastActive = true
	st.Pending = &RolledFish{Species: "g_tench", WeightG: 547}
	st.mu.Unlock()

	for _, f0 := range []uint64{1, 2, 8} {
		tree := proto.Args524(proto.Obj("lhicjpmglbo", 0,
			proto.Field{Op: "U32", Num: ptrU64(f0)}))
		if _, ok := hFishingLeft(miscCtx(sess, 14, 12, tree)); !ok {
			t.Fatalf("14/12 f0=%d 未处理", f0)
		}
	}

	// 静默断言：短窗口内不得有任何推送。
	assertNoPush(t, gc, 400*time.Millisecond, "f0=1/2/8 未上钩离场不应产生任何推送")
}

// 已中鱼后的离场按 f0 分流（2026-11-16 客户端两阶段模型收敛，
// docs/路亚脱钩率分析-2026-11-16.md）：f0=1 = 阶段一「叼住吐出」——
// 鱼咬了饵但从未真正挂牢，客户端连「上鱼」通知都不出、本地化模板表
// 无此类载体，即使服务端记了 14/11 中鱼确认也官方静默，不计 escapes、
// 不触发大鱼脱钩补偿；f0=2/8 = 阶段二搏鱼危险掷骰失败（真脱钩）：
// 默认蓝色模板 3 播报并计 escapes。
func TestFishHookedEscapeSplitsByF0(t *testing.T) {
	srv, db := newRigTestServer(t)

	// f0=1：已中鱼也静默；用奖杯级鱼和预置非零补偿状态，
	// 防止 recordHuntEscape 回归时只被 Grade=0 样本掩盖。
	const (
		loc  = "hunt-test"
		spot = uint64(7)
	)
	sess, gc := newTunnelProbe(t, srv)
	seed := &fishHuntState{
		UID: sess.Userid, Loc: loc, Spot: spot,
		BigMiss: 2, CompFish: 2, CompGrade: 3,
		CompUntil: float64(time.Now().Add(time.Hour).Unix()),
	}
	srv.saveFishHuntState(context.Background(), seed)
	st := srv.stateFor(sess.Userid)
	st.mu.Lock()
	st.CastActive = true
	st.Hooked = true
	st.Loc, st.Spot = loc, spot
	st.Pending = &RolledFish{
		Species: "g_tench", WeightG: 6300, Grade: 4,
		HuntStageKnown: true, HuntStage: fishHuntActive,
	}
	st.mu.Unlock()
	tree := proto.Args524(proto.Obj("lhicjpmglbo", 0,
		proto.Field{Op: "U32", Num: ptrU64(1)}))
	if _, ok := hFishingLeft(miscCtx(sess, 14, 12, tree)); !ok {
		t.Fatal("14/12 f0=1 未处理")
	}
	if got := counterValue(t, db, sess.Userid, "escapes"); got != 0 {
		t.Fatalf("f0=1 叼住吐出不应计 escapes，实得 %d", got)
	}
	hunt := srv.loadFishHuntState(context.Background(), sess.Userid, loc, spot)
	if hunt.BigMiss != 2 || hunt.CompFish != 2 || hunt.CompGrade != 3 {
		t.Fatalf("f0=1 不应改写鱼猎脱钩补偿，实得 BigMiss=%d CompFish=%d CompGrade=%d",
			hunt.BigMiss, hunt.CompFish, hunt.CompGrade)
	}
	assertNoPush(t, gc, 400*time.Millisecond, "f0=1 叼住吐出不应产生任何推送")

	// f0=2/8：真脱钩，播报 24/27 蓝色模板 3。
	for _, f0 := range []uint64{2, 8} {
		sess, gc := newTunnelProbe(t, srv)
		st := srv.stateFor(sess.Userid)
		st.mu.Lock()
		st.CastActive = true
		st.Hooked = true
		st.Pending = &RolledFish{Species: "g_tench", WeightG: 547}
		st.mu.Unlock()
		tree := proto.Args524(proto.Obj("lhicjpmglbo", 0,
			proto.Field{Op: "U32", Num: ptrU64(f0)}))
		if _, ok := hFishingLeft(miscCtx(sess, 14, 12, tree)); !ok {
			t.Fatalf("14/12 f0=%d 未处理", f0)
		}
		env := awaitPushEnvelope(t, gc)
		if env.Main != 24 || env.Sub != 27 {
			t.Fatalf("f0=%d 拉脱播报应走 24/27，实得 %d/%d", f0, env.Main, env.Sub)
		}
		dec, _, err := proto.DecodeValue(env.Body, 0, srv.Table())
		if err != nil {
			t.Fatalf("f0=%d 推送 body 不可解码: %v", f0, err)
		}
		mf0 := dec.Items[0].Field(0)
		if got := mf0.Num; got == nil || *got != 3 {
			t.Fatalf("f0=%d 真脱钩应走蓝色模板 3，实得 %v", f0, got)
		}
		if !bytes.Contains(env.Body, []byte("你的")) {
			t.Fatalf("f0=%d 推送缺少跑鱼文案: %x", f0, env.Body)
		}
	}

	// 关闭嘲讽开关后回退官方模板 7（f0=2 真脱钩），保证可回滚。
	if err := db.TunableSet(context.Background(), "fish_escape_taunt", "0", "test"); err != nil {
		t.Fatal(err)
	}
	srv.execAdminCmd(&AdminCommand{Op: "reload_tunables"})
	sess, gc = newTunnelProbe(t, srv)
	st = srv.stateFor(sess.Userid)
	st.mu.Lock()
	st.CastActive = true
	st.Hooked = true
	st.Pending = &RolledFish{Species: "g_tench", WeightG: 547}
	st.mu.Unlock()
	tree = proto.Args524(proto.Obj("lhicjpmglbo", 0,
		proto.Field{Op: "U32", Num: ptrU64(2)}))
	if _, ok := hFishingLeft(miscCtx(sess, 14, 12, tree)); !ok {
		t.Fatal("14/12 未处理")
	}
	env := awaitPushEnvelope(t, gc)
	dec, _, err := proto.DecodeValue(env.Body, 0, srv.Table())
	if err != nil {
		t.Fatalf("推送 body 不可解码: %v", err)
	}
	if got := dec.Items[0].Field(0).Num; got == nil || *got != 7 {
		t.Fatalf("关闭嘲讽后应回退官方模板 7，实得 %v", got)
	}
}

// 未知 f0 兜底分支（f0=7 =「无钓组」终态语义，非脱钩）在已中鱼时
// 保持保守播报：无法确认客户端状态机路径时按真脱钩处理，宁播勿漏。
func TestFishUnknownFrameHookedStillBroadcasts(t *testing.T) {
	srv, _ := newRigTestServer(t)
	sess, gc := newTunnelProbe(t, srv)

	st := srv.stateFor(sess.Userid)
	st.mu.Lock()
	st.CastActive = true
	st.Hooked = true // 已中鱼（14/11）确认；未知 f0 走兜底保守播报。
	st.Pending = &RolledFish{Species: "g_tench", WeightG: 547}
	st.mu.Unlock()

	tree := proto.Args524(proto.Obj("lhicjpmglbo", 0,
		proto.Field{Op: "U32", Num: ptrU64(7)}))
	if _, ok := hFishingLeft(miscCtx(sess, 14, 12, tree)); !ok {
		t.Fatal("14/12 f0=7 未处理")
	}

	env := awaitPushEnvelope(t, gc)
	if env.Main != 24 || env.Sub != 27 {
		t.Fatalf("脱钩提示应走 24/27，实得 %d/%d", env.Main, env.Sub)
	}
	dec, _, err := proto.DecodeValue(env.Body, 0, srv.Table())
	if err != nil {
		t.Fatalf("推送 body 不可解码: %v", err)
	}
	if dec.Kind != proto.KindArgs || len(dec.Items) != 3 {
		t.Fatalf("24/27 body 应 args[3]")
	}
	mf0 := dec.Items[0].Field(0)
	if got := mf0.Num; got == nil || *got != 3 {
		t.Fatalf("未知帧已中鱼应走蓝色模板 3，实得 %v", got)
	}
	if !bytes.Contains(env.Body, []byte("你的")) {
		t.Fatalf("推送缺少跑鱼文案: %x", env.Body)
	}
}

func TestPushChatProbeUsesRequestedChannel(t *testing.T) {
	srv, _ := newRigTestServer(t)
	sess, gc := newTunnelProbe(t, srv)

	for _, channel := range []uint8{27, 28, 29, 30, 31} {
		sess.pushChatProbe(int(channel), 0, "鱼汛通道测试", nil)
		env := awaitPushEnvelope(t, gc)
		if env.Main != 24 || env.Sub != channel {
			t.Fatalf("pushChatProbe(%d) 应发送 24/%d，实得 %d/%d", channel, channel, env.Main, env.Sub)
		}
	}
}

// 鱼汛播报文本：奖杯/蓝冠/破纪录/入护/放生/跑鱼。鱼种名按静态库
// fish_zh 显示，测试库缺行时回退 key，因此只断言稳定片段。
func TestFishEventChatTexts(t *testing.T) {
	srv, _ := newRigTestServer(t)
	tench := func(grade int64, w int64) *RolledFish {
		return &RolledFish{Species: "g_tench", Grade: grade, WeightG: w}
	}

	if got := srv.fishLandedText(tench(1, 547), false); got != "" {
		t.Fatalf("普通鱼不应播报: %q", got)
	}
	if got := srv.fishLandedText(tench(1, 547), true); got != "" {
		t.Fatalf("未达标破纪录也不应播报: %q", got)
	}
	if got := srv.fishLandedText(tench(4, 6300), false); !strings.Contains(got, "钓获蓝冠") || !strings.Contains(got, "6.30kg") {
		t.Fatalf("蓝冠播报不符: %q", got)
	}
	if got := srv.fishLandedText(tench(4, 6300), true); !strings.Contains(got, "（新纪录）") {
		t.Fatalf("蓝冠破纪录播报不符: %q", got)
	}
	if got := srv.fishLandedText(tench(2, 800), true); !strings.Contains(got, "钓获达标") || !strings.Contains(got, "（新纪录）") {
		t.Fatalf("达标破纪录播报不符: %q", got)
	}
	if got := srv.fishLandedText(tench(2, 800), false); !strings.Contains(got, "钓获达标") {
		t.Fatalf("达标鱼应播报: %q", got)
	}
	if got := srv.fishLandedText(tench(3, 4100), false); !strings.Contains(got, "钓获奖杯") || !strings.Contains(got, "4.10kg") {
		t.Fatalf("奖杯播报不符: %q", got)
	}

	kept := srv.fishKeptText(tench(1, 547))
	if !strings.Contains(kept, "入护") || !strings.Contains(kept, "547g") {
		t.Fatalf("入护播报不符: %q", kept)
	}
	if strings.Contains(kept, "护内") {
		t.Fatalf("入护播报不应再含护内统计: %q", kept)
	}
	if got := srv.fishReleasedText(tench(2, 3280)); !strings.Contains(got, "放生：") || !strings.Contains(got, "3.28kg") {
		t.Fatalf("普通放生播报不符: %q", got)
	}
	if got := srv.fishReleasedText(tench(4, 6300)); !strings.Contains(got, "放生蓝冠") {
		t.Fatalf("蓝冠放生播报不符: %q", got)
	}
	esc := srv.fishEscapeText(tench(1, 547), "断主线")
	if !strings.Contains(esc, "跑鱼") || !strings.Contains(esc, "断主线") {
		t.Fatalf("跑鱼播报不符: %q", esc)
	}
	if got := srv.fishEscapeText(nil, "脱钩"); got != "跑鱼（脱钩）" {
		t.Fatalf("无鱼跑鱼播报不符: %q", got)
	}
}

// 奖杯/蓝冠鱼上鱼时，若管理员显式关闭 KV 模板（fish_tv_kv=0），
// 不得发带参数的 k25/k26/k03 模板播报（缺参会导致客户端解析异常断连），
// 应退化为普通系统文本播报（24/27，kind=0）。
func TestFishLandedFallsBackToPlainChatWithoutKV(t *testing.T) {
	srv, _ := newRigTestServer(t)
	srv.tunMu.Lock()
	srv.tunCache = map[string]float64{"fish_tv_kv": 0}
	srv.tunAt = time.Now()
	srv.tunMu.Unlock()
	sess, gc := newTunnelProbe(t, srv)

	srv.notifyFishLanded(sess, &RolledFish{Species: "g_tench", Grade: 3, WeightG: 4100}, false)

	env := awaitPushEnvelope(t, gc)
	if env.Main != 24 || env.Sub != 27 {
		t.Fatalf("上鱼播报应走 24/27，实得 %d/%d", env.Main, env.Sub)
	}
	dec, _, err := proto.DecodeValue(env.Body, 0, srv.Table())
	if err != nil {
		t.Fatalf("推送 body 不可解码: %v", err)
	}
	if dec.Kind != proto.KindArgs || len(dec.Items) != 3 {
		t.Fatalf("24/27 body 应 args[3]，实得 kind=%v len=%d", dec.Kind, len(dec.Items))
	}
	msg := dec.Items[0]
	if got := msg.Field(0).Num; got == nil || u64Of(got) != 0 {
		t.Fatalf("KV 未启用时上鱼播报应退化为 kind=0 纯文本，实得 %v", got)
	}
	mf1 := msg.Field(1)
	if txt := mf1.StrOr(); !strings.Contains(txt, "钓获奖杯") || !strings.Contains(txt, "4.10kg") {
		t.Fatalf("上鱼播报文本不符: %q", txt)
	}
}
