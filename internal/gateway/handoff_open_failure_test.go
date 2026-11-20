package gateway_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"rf4server/internal/gateway"
	"rf4server/internal/testutil"
)

// failingLoadStore 在置位后让 LoadHandoff 返回真实错误，其余方法透传。
//
// 只影响「读承接状态」这一条路径：token 校验等仍走真存储，客户端能正常
// 完成握手走到重连分支。
type failingLoadStore struct {
	gateway.Store
	fail bool
}

func (s *failingLoadStore) LoadHandoff(ctx context.Context, token string) (*gateway.Handoff, error) {
	if s.fail {
		return nil, errors.New("模拟数据库故障")
	}
	return s.Store.LoadHandoff(ctx, token)
}

// waitHandoff 轮询等待承接状态落库。
func waitHandoff(t *testing.T, store gateway.Store, token string) *gateway.Handoff {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if h, err := store.LoadHandoff(context.Background(), token); err == nil && h != nil {
			return h
		}
		time.Sleep(20 * time.Millisecond)
	}
	return nil
}

// TestReconnectOpenFailureKeepsHandoff 锁住 P1：重连时若隧道 OPEN 失败，
// 承接状态必须留在库里，不能被提前删掉。
//
// 事故形态：读取承接状态后立刻删除，随后才发 OPEN。隧道此刻仍不可用时
// sendOpen 直接失败返回，写者从未启动，也就没有任何路径会重新保存——库里
// 那份状态永久丢失，客户端下次重连只能拿到全新密码流，整条下行解不开。
func TestReconnectOpenFailureKeepsHandoff(t *testing.T) {
	stub, addr := testutil.StartStubGameServer(t, testutil.TestPSK)
	gw := testutil.StartGateway(t, addr, testutil.TestPSK)

	token, _ := gw.Login(t)
	c := testutil.NewFakeClient(t, gw.RealAddr)
	c.Dial(token, false)
	waitTunnelUp(t, stub)
	c.SendRequest(1, 0x03, 0x03)
	c.WaitResponse(3 * time.Second)

	// 断开客户端，让写者收尾把承接状态落库。
	c.Close()
	saved := waitHandoff(t, gw.Store, token)
	if saved == nil {
		t.Fatal("断开后没有承接状态，测试前提不成立")
	}

	// 隧道整体断开：重连时 sendOpen 必然失败。
	stub.Stop()
	time.Sleep(300 * time.Millisecond)

	c2 := testutil.NewFakeClient(t, gw.RealAddr)
	c2.Dial(token, true)
	// 等到 sendOpen 真正走完失败路径（退避至少 1 秒）。
	time.Sleep(1500 * time.Millisecond)

	h, err := gw.Store.LoadHandoff(context.Background(), token)
	if err != nil {
		t.Fatalf("读取承接状态出错: %v", err)
	}
	if h == nil {
		t.Fatal("重连 OPEN 失败后承接状态被删除了：客户端再次重连只能拿到全新密码流，钓鱼进度永久丢失")
	}
	if h.NextWireID != saved.NextWireID {
		t.Fatalf("承接状态被改写: next_wire_id %d → %d", saved.NextWireID, h.NextWireID)
	}
}

// TestLoadHandoffErrorAbortsReconnect 锁住 P2-1：读承接状态本身失败时，
// 必须中止本次重连并保留状态，绝不能当成「没有状态」去新建密码流。
//
// 事故形态：把「确实没有状态」和「读库出错」放进同一个 else，记录一条
// WARN 后继续用同一个 token 重新 KSA 出全新密码流。客户端的流还在旧位置，
// 整条下行全部解不开；同时库里那份承接状态没人再来读，成了孤儿。
//
// 判别依据是桩主服收到的 OPEN 数量：中止重连就不会开新会话。
func TestLoadHandoffErrorAbortsReconnect(t *testing.T) {
	stub, addr := testutil.StartStubGameServer(t, testutil.TestPSK)
	store := &failingLoadStore{Store: gateway.NewMemStore(1000001)}
	gw := testutil.StartGatewayWithStore(t, addr, testutil.TestPSK, store)

	token, _ := gw.Login(t)
	c := testutil.NewFakeClient(t, gw.RealAddr)
	c.Dial(token, false)
	waitTunnelUp(t, stub)
	c.SendRequest(1, 0x03, 0x03)
	c.WaitResponse(3 * time.Second)
	c.Close()

	if waitHandoff(t, store.Store, token) == nil {
		t.Fatal("断开后没有承接状态，测试前提不成立")
	}

	openedBefore, _, _ := stub.Stats()

	// 从这里开始读承接状态报错：必须中止重连。
	store.fail = true
	c2 := testutil.NewFakeClient(t, gw.RealAddr)
	c2.Dial(token, true)
	time.Sleep(500 * time.Millisecond)

	openedAfter, _, _ := stub.Stats()
	if len(openedAfter) != len(openedBefore) {
		t.Fatalf("读承接状态失败却仍然开了新会话（OPEN %d → %d）：客户端流在旧位置，下行会全部解不开",
			len(openedBefore), len(openedAfter))
	}

	store.fail = false
	if h, err := store.Store.LoadHandoff(context.Background(), token); err != nil || h == nil {
		t.Fatalf("读库失败后承接状态必须保留，实际 err=%v h=%v", err, h)
	}
}
