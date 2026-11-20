package gateway

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"rf4server/internal/proto"
)

// TestOwnershipHandover 直接钉住「同一 token 的所有权交接」语义。
//
// 行为层面的覆盖在 handoff_ownership_test.go 里（旧写者不得覆盖新会话）；
// 这里守的是交接本身的两个易错点：
//   - 旧会话收尾时不能把新会话的所有权一并抹掉（否则更早的旧写者又获得保存资格）；
//   - 只有当前所有者才算持有，被接管的旧会话一律不算。
func TestOwnershipHandover(t *testing.T) {
	s := &RealtimeServer{owners: make(map[string]uint64)}
	const token = "tok-ownership"

	// 旧会话 1 拿到所有权。
	s.claimOwnership(token, 1)
	if !s.ownsToken(token, 1) {
		t.Fatal("刚夺取所有权的会话应当被认为持有")
	}

	// 新会话 2 接管。
	s.claimOwnership(token, 2)
	if s.ownsToken(token, 1) {
		t.Fatal("被接管的旧会话不应再被认为持有：它会继续写落后的快照")
	}
	if !s.ownsToken(token, 2) {
		t.Fatal("新会话应当持有所有权")
	}

	// 旧会话 1 收尾：不得抹掉 2 的所有权。
	s.releaseOwnership(token, 1)
	if !s.ownsToken(token, 2) {
		t.Fatal("旧会话收尾抹掉了新会话的所有权：更早的旧写者会重新获得保存资格")
	}

	// 新会话 2 收尾：正常释放。
	s.releaseOwnership(token, 2)
	if s.ownsToken(token, 2) {
		t.Fatal("所有者收尾后应释放所有权")
	}

	// 未知 token 的释放不应 panic 或产生条目。
	s.releaseOwnership("nobody", 99)
	if s.ownsToken("nobody", 99) {
		t.Fatal("未知 token 不应被认为持有")
	}
}

// blockingSaveStore 在写库处阻塞，用来把「所有权校验已通过、但尚未写进
// 数据库」这个窗口确定性钉住。
type blockingSaveStore struct {
	Store
	entered chan struct{}
	release chan struct{}
}

func (s *blockingSaveStore) SaveHandoff(ctx context.Context, token string, h Handoff, ttl time.Duration) error {
	close(s.entered)
	<-s.release
	return s.Store.SaveHandoff(ctx, token, h, ttl)
}

// TestOwnershipCheckIsAtomicWithSave 锁住「所有权校验与落库之间的 TOCTOU」。
//
// 事故形态：旧写者通过 ownsToken 校验后被调度走，新会话趁机夺取所有权并写入
// 更新的 next_wire_id，随后旧写者把自己那份更旧的快照写回去——数据库回退到
// 更旧的进度，客户端下次重连从更早的位置续流，下行解不开。
//
// 判别点很小但确定：旧写者卡在落库途中时，接管动作**必须**被挡住。只做布尔
// 检查的实现里接管会在毫秒级完成，这里 500ms 的观察窗口足以稳定区分。
func TestOwnershipCheckIsAtomicWithSave(t *testing.T) {
	initRealtimeMetrics(obsRegistry(nil))
	inner := NewMemStore(1)
	store := &blockingSaveStore{
		Store:   inner,
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}
	srv := &RealtimeServer{store: store, log: slog.New(slog.DiscardHandler), owners: make(map[string]uint64)}
	const token = "tok-atomic"

	rc4In, err := proto.NewRC4([]byte(token))
	if err != nil {
		t.Fatalf("初始化 RC4 失败: %v", err)
	}
	rc4Out, _ := proto.NewRC4([]byte(token))
	old := &clientSession{
		id: 1, token: token, rc4In: rc4In, rc4Out: rc4Out,
		resumed: true, srv: srv, log: srv.log,
	}
	srv.claimOwnership(token, old.id)

	// 旧写者开始保存：它已通过所有权校验，现在卡在写库里。
	saved := make(chan struct{})
	go func() { old.saveHandoff(5); close(saved) }()
	select {
	case <-store.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("旧写者没有进入落库，测试前提不成立")
	}

	// 窗口里发起接管，它必须等旧写者落库完成才能拿到所有权。
	claimed := make(chan struct{})
	go func() { srv.claimOwnership(token, 2); close(claimed) }()
	select {
	case <-claimed:
		t.Fatal("接管在旧写者落库途中就完成了：所有权校验与落库不是原子的，" +
			"旧写者随后会把更旧的快照写进库里覆盖新会话的进度")
	case <-time.After(500 * time.Millisecond):
	}

	close(store.release)
	<-saved
	select {
	case <-claimed:
	case <-time.After(5 * time.Second):
		t.Fatal("放行后接管仍未完成")
	}

	// 接管生效后，旧写者再存必须被丢弃（这正是审查者复现的第 3 步）。
	old.saveHandoff(3)
	if h, err := inner.LoadHandoff(context.Background(), token); err == nil && h != nil && h.NextWireID == 3 {
		t.Fatal("接管之后旧写者仍把更旧的快照写进库里：next_wire_id 回退到 3")
	}
}
