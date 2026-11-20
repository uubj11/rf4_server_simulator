package gateway_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"rf4server/internal/gateway"
	"rf4server/internal/testutil"
)

// TestOldWriterDoesNotOverwriteNewHandoff 锁住「并发重连时旧写者覆盖新会话快照」。
//
// 事故形态：客户端与服务端之间的旧 TCP 尚未被读到断开（链路半开，或客户端只是
// 另开了一条新连接），客户端就用同一个 token 重连。主服侧接管了旧会话，网关侧
// 旧 clientSession 却还在跑，它的写者每 2 秒存一次快照，wire_id 比新会话更旧，
// 于是新会话刚落库的进度被覆盖回去，客户端下次重连从更早的位置续流——下行又
// 解不开，复现「反复掉线」的原事故。
//
// 时序刻意做成确定的：先让新会话存下领先的进度并退出（写者收尾后不再写），
// 再留出整整一个旧写者周期。这样「最后一次写入」只可能来自旧写者，
// 有无所有权校验的差异会稳定显现，而不是靠两个写者抢时序碰运气。
func TestOldWriterDoesNotOverwriteNewHandoff(t *testing.T) {
	stub, addr := testutil.StartStubGameServer(t, testutil.TestPSK)
	gw := testutil.StartGateway(t, addr, testutil.TestPSK)

	token, _ := gw.Login(t)

	// 同一个 FakeClient 连续 Dial 两次：它复用同一对 RC4 流（真实客户端跨连接
	// 延续密码流的语义），而服务端侧旧 TCP 一直没关——这正是新旧会话重叠的窗口。
	c := testutil.NewFakeClient(t, gw.RealAddr)

	// 旧会话：发两帧后保持连接开着。
	c.Dial(token, false)
	waitTunnelUp(t, stub)
	for i := uint32(1); i <= 2; i++ {
		c.SendRequest(i, 0x03, 0x03)
		c.WaitResponse(3 * time.Second)
	}
	if waitHandoff(t, gw.Store, token) == nil {
		t.Fatal("旧会话没有落库，测试前提不成立")
	}

	// 新会话：同 token 重连，进度明显领先。
	c.Dial(token, true)
	for i := uint32(3); i <= 5; i++ {
		c.SendRequest(i, 0x03, 0x03)
		c.WaitResponse(3 * time.Second)
	}

	// 等新会话把领先进度落库，然后关掉它：写者收尾后再不会写。
	var newWire uint64
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if h, err := gw.Store.LoadHandoff(context.Background(), token); err == nil && h != nil && h.NextWireID >= 4 {
			newWire = h.NextWireID
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if newWire < 4 {
		t.Fatalf("新会话进度没有落库（next_wire_id=%d），测试前提不成立", newWire)
	}
	c.Close()

	// 留出整整一个旧写者周期：此刻若旧写者仍被允许落库，它必然用更旧的
	// wire_id 覆盖掉上面这份进度。
	time.Sleep(2500 * time.Millisecond)

	h, err := gw.Store.LoadHandoff(context.Background(), token)
	if err != nil {
		t.Fatalf("读取承接状态出错: %v", err)
	}
	if h == nil {
		t.Fatal("承接状态被删空了")
	}
	if h.NextWireID < newWire {
		t.Fatalf("旧写者用落后的快照覆盖了新会话的进度：next_wire_id %d → %d，客户端下次重连会从更早的位置续流，下行解不开",
			newWire, h.NextWireID)
	}
}

// blockingSaveStore 在 SaveHandoff 上按条件阻塞，用来把「所有权校验通过、
// 但还没写进数据库」这个窗口精确地钉住。
//
// 只靠 sleep 拼时序是测不稳的：旧写者通过校验之后到落库之间的窗口极短，
// 靠调度碰运气，去掉修复也大概率能过。这里让第一次落库在存储层停住，
// 让新旧会话的先后关系变成确定的。
type blockingSaveStore struct {
	gateway.Store

	mu      sync.Mutex
	armed   bool
	entered chan struct{} // 旧写者进入 SaveHandoff 时关闭
	release chan struct{} // 测试侧放行
	writes  []uint64      // 每次真正落库时的 next_wire_id，按完成顺序
}

func newBlockingSaveStore(inner gateway.Store) *blockingSaveStore {
	return &blockingSaveStore{
		Store:   inner,
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}
}

// arm 让下一次 SaveHandoff 阻塞。
func (s *blockingSaveStore) arm() {
	s.mu.Lock()
	s.armed = true
	s.mu.Unlock()
}

func (s *blockingSaveStore) SaveHandoff(ctx context.Context, token string, h gateway.Handoff, ttl time.Duration) error {
	s.mu.Lock()
	block := s.armed
	if block {
		s.armed = false
	}
	s.mu.Unlock()
	if block {
		close(s.entered)
		<-s.release
	}
	// 记录完成顺序：谁最后真正写进库，谁就决定了客户端下次续流的位置。
	s.mu.Lock()
	s.writes = append(s.writes, h.NextWireID)
	s.mu.Unlock()
	return s.Store.SaveHandoff(ctx, token, h, ttl)
}

// recorded 返回按完成顺序记录的 next_wire_id 序列。
func (s *blockingSaveStore) recorded() []uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]uint64{}, s.writes...)
}
