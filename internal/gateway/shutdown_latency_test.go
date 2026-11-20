package gateway_test

import (
	"testing"
	"time"

	"rf4server/internal/testutil"
)

// TestShutdownNotBlockedByTunnelBackoff 约束关闭链的延迟上界。
//
// 事故形态：隧道断开后，N 个会话收尾都要发 CLOSE。若 getConn 在持有管理器锁
// 的情况下做重连退避（最长 32s）与拨号（5s 超时），这些收尾会在锁上串行排队，
// 紧随其后的 tm.Close() 要等 N×37s 才拿到锁。systemd 的停止超时是 90s，超时
// 即 SIGKILL，而 main 的关闭序里 db.Close() 排在 tm.Close() 之后——进程被强杀
// 时 handoff 还没落库，重连必然承接失败，客户端掉线。这就是本仓库真实发生过
// 的掉线链路，所以关闭延迟必须被测试钉死。
//
// 判定：隧道断开后，rt.Close() 与 tm.Close() 都必须在秒级返回。这里给 10s
// 余量（正确实现实测在百微秒级），既不会因调度抖动误报，也远低于 90s 的
// systemd 停止超时。
func TestShutdownNotBlockedByTunnelBackoff(t *testing.T) {
	const sessions = 8

	stub, addr := testutil.StartStubGameServer(t, "shutdown-psk")
	gw := testutil.StartGateway(t, addr, "shutdown-psk")

	clients := make([]*testutil.FakeClient, 0, sessions)
	for i := 0; i < sessions; i++ {
		token, _ := gw.Login(t)
		c := testutil.NewFakeClient(t, gw.RealAddr)
		c.Dial(token, false)
		clients = append(clients, c)
	}
	// 等全部会话的 OPEN 抵达主服桩，确保隧道上确实有 N 条会话在跑。
	waitOpened := func() bool {
		opened, _, _ := stub.Stats()
		return len(opened) >= sessions
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && !waitOpened() {
		time.Sleep(10 * time.Millisecond)
	}
	if !waitOpened() {
		opened, _, _ := stub.Stats()
		t.Fatalf("隧道上只建立了 %d/%d 条会话，测试前提不成立", len(opened), sessions)
	}

	// 掐断隧道：此后会话收尾的 sendClose 会走重连路径。
	stub.Stop()
	time.Sleep(300 * time.Millisecond)

	start := time.Now()
	_ = gw.RT.Close()
	rtElapsed := time.Since(start)
	if rtElapsed > 10*time.Second {
		t.Fatalf("隧道断开后 rt.Close() 耗时 %v，超过 10s 上界：关闭链被重连退避拖住了", rtElapsed)
	}

	start = time.Now()
	gw.TM.Close()
	tmElapsed := time.Since(start)
	if tmElapsed > 10*time.Second {
		t.Fatalf("隧道断开后 tm.Close() 耗时 %v，超过 10s 上界：会话收尾在隧道管理器锁上串行排队", tmElapsed)
	}

	for _, c := range clients {
		c.Close()
	}
	t.Logf("隧道断开后关闭链耗时: rt.Close()=%v tm.Close()=%v (sessions=%d)", rtElapsed, tmElapsed, sessions)
}
