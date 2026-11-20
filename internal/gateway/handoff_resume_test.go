package gateway_test

import (
	"context"
	"testing"
	"time"

	"rf4server/internal/testutil"
)

// TestHandoffResumeKeepsKeystream 断线重连承接旧流后，双向 RC4 必须继续能解。
//
// 这是本次掉线的核心回归：客户端因为隧道抖动被断开，随即按「延续旧流」
// 重连（auth flag=0x01）。服务端若把 RC4 流复位，或拿到一份与 wire_id
// 不同源的快照，第二条连接上第一个下行帧就解不开，客户端只能一直重试
// 到弹「未连接到游戏服务器」。
//
// 断言两件事：
//  1. 断线时确实落库了 handoff，且 next_wire_id 已越过已发出的数据帧；
//  2. 重连后下行帧仍能被客户端延续的 rc4Out 解出合法信封，且 wire_id 不回退。
//
// FakeClient 的 Dial(token, true) 复用自己已有的 rc4In/rc4Out 与 wire 计数，
// 正是真实客户端跨连接延续密码流的行为。
func TestHandoffResumeKeepsKeystream(t *testing.T) {
	stub, stubAddr := testutil.StartStubGameServer(t, testutil.TestPSK)
	gw := testutil.StartGateway(t, stubAddr, testutil.TestPSK)

	token, _ := gw.Login(t)
	c := testutil.NewFakeClient(t, gw.RealAddr)
	c.Dial(token, false)
	waitTunnelUp(t, stub)

	// 第一条连接上跑几轮，把 wire_id 与两个 RC4 流都推进出去。
	for i := uint32(1); i <= 3; i++ {
		c.SendRequest(i, 0x03, 0x03)
		c.WaitResponse(3 * time.Second)
	}
	c.Close()

	// 等服务端把会话收尾并落库（客户端断线 → 读循环退出 → writer 收尾保存）。
	var handoffWireID uint64
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		h, err := gw.Store.LoadHandoff(context.Background(), token)
		if err == nil && h != nil {
			handoffWireID = h.NextWireID
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if handoffWireID == 0 {
		t.Fatal("断线后没有落库 handoff：重连将无从承接")
	}
	if handoffWireID < 4 {
		t.Fatalf("handoff next_wire_id=%d，至少应越过已发出的 3 个数据帧", handoffWireID)
	}

	// 重连：复用同一条客户端（同 token、同 RC4 流、同 wire 计数）。
	c.Dial(token, true)
	c.SendRequest(100, 0x03, 0x03)
	env := c.WaitResponse(5 * time.Second)
	if env.CallID != 100 || env.Marker != -2 {
		t.Fatalf("重连后下行解不出正确信封: call=%d marker=%d body=%x", env.CallID, env.Marker, env.Body)
	}
	if c.WireBad != 0 {
		t.Fatalf("重连后 wire_id 出现回退 %d 次，客户端会判定乱序", c.WireBad)
	}
}
