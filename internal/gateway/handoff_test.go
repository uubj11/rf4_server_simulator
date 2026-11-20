package gateway_test

import (
	"context"
	"testing"
	"time"

	"rf4server/internal/testutil"
)

// waitTunnelUp 等待主服桩收到 OPEN（隧道真正建立、会话可转发）。
func waitTunnelUp(t *testing.T, stub *testutil.StubGameServer) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if opened, _, _ := stub.Stats(); len(opened) > 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("隧道在 5 秒内没有建立（未收到 OPEN）")
}

// TestHandoffPersistedOnClose 关闭网关必须等会话把 RC4 流与 wire_id 落库。
//
// 现场事故：隧道断开时 gateway 立刻走 db.Close()，在线会话的写者 goroutine
// 还没收尾，最后一次（也是最准确的一次）handoff 保存失败，日志里是
// `handoff 保存失败 err="sql: database is closed"`。客户端重连只能承接
// 一份过期快照，RC4 keystream 从第一个字节起就错位，表现为服务端满屏
// 「信封解析失败」，最后客户端弹「未连接到游戏服务器」并断开。
func TestHandoffPersistedOnClose(t *testing.T) {
	stub, stubAddr := testutil.StartStubGameServer(t, testutil.TestPSK)
	gw := testutil.StartGateway(t, stubAddr, testutil.TestPSK)

	token, _ := gw.Login(t)
	c := testutil.NewFakeClient(t, gw.RealAddr)
	c.Dial(token, false)
	waitTunnelUp(t, stub)

	// 走一轮真实请求，让会话产生下行数据帧（wire_id 与 rc4Out 都被推进）。
	c.SendRequest(1, 0x03, 0x03)
	c.WaitResponse(3 * time.Second)

	// Close 必须阻塞到会话收尾完成；返回后 handoff 必须已经可读。
	if err := gw.RT.Close(); err != nil {
		t.Logf("Close 返回（容忍 listener 已关）: %v", err)
	}
	h, err := gw.Store.LoadHandoff(context.Background(), token)
	if err != nil {
		t.Fatalf("读取 handoff 失败: %v", err)
	}
	if h == nil {
		t.Fatal("关闭后没有 handoff：写者收尾前进程就退出了，重连必然承接失败")
	}
	if h.NextWireID < 2 {
		t.Fatalf("handoff 的 next_wire_id=%d，至少应推进到 2（发出过数据帧）", h.NextWireID)
	}
}
