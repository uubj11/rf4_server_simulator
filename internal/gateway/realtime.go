package gateway

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"rf4server/internal/config"
	"rf4server/internal/metrics"
	"rf4server/internal/proto"
)

// realtime 指标。
var (
	rtConnsActive    prometheus.Gauge
	rtFramesIn       *prometheus.CounterVec
	rtFramesOut      prometheus.Counter
	rtAcksSent       prometheus.Counter
	rtAuthFailures   prometheus.Counter
	rtHandoffSaved   prometheus.Counter
	rtHandoffUsed    prometheus.Counter
	rtHandoffMissing prometheus.Counter
)

func initRealtimeMetrics(reg *prometheus.Registry) {
	rtConnsActive = metrics.NewGauge(reg, "gateway_realtime_active", "当前 realtime 连接数")
	rtFramesIn = metrics.NewLabeledCounter(reg, "gateway_realtime_frames_in_total", "客户端入帧数", "kind")
	rtFramesOut = metrics.NewCounter(reg, "gateway_realtime_frames_out_total", "服务端出帧数")
	rtAcksSent = metrics.NewCounter(reg, "gateway_realtime_acks_total", "ACK 帧数")
	rtAuthFailures = metrics.NewCounter(reg, "gateway_realtime_auth_failures_total", "认证失败数")
	rtHandoffSaved = metrics.NewCounter(reg, "gateway_realtime_handoff_saved_total", "重连状态保存数")
	rtHandoffUsed = metrics.NewCounter(reg, "gateway_realtime_handoff_used_total", "重连状态恢复数")
	rtHandoffMissing = metrics.NewCounter(reg, "gateway_realtime_handoff_missing_total", "重连请求但无状态可承接数")
}

// obsRegistry 观测端口缺省时用本地一次性注册表（测试场景），保证指标永不 nil。
func obsRegistry(obs *metrics.Server) *prometheus.Registry {
	if obs != nil {
		return obs.Registry()
	}
	return prometheus.NewRegistry()
}

// RealtimeServer TCP :9341 realtime 接入服务。
type RealtimeServer struct {
	cfg    config.Gateway
	store  Store
	log    *slog.Logger
	tm     *TunnelManager
	ln     net.Listener
	sem    chan struct{}
	sids   sync.Map // sid -> *clientSession
	sidSeq atomic.Uint64
	// writers 只跟踪写者 goroutine。Close 必须等它归零，否则 main 里的
	// db.Close() 会先于 handoff 落库，承接状态直接丢失。这里刻意不跟踪
	// 整个会话 goroutine：会话收尾还要走隧道 sendClose，而 getConn 持全局
	// 锁串行退避与拨号，会话一多就会拖到分钟级。
	// writerMu 与 closing 配合，保证 Wait 期间不会有新的 Add。
	writerMu sync.Mutex
	writers  sync.WaitGroup
	closing  atomic.Bool
	// owners 记录每个 token 当前由哪个会话持有（token -> sid）。
	//
	// 承接状态按 token 存储，而同一 token 可能同时活着两个会话：主服已接管
	// 旧会话，但网关侧旧 TCP 还没被读循环发现断开，旧写者仍在跑、仍会周期性
	// 保存快照。没有这张表的话，旧写者会用落后的 wire_id 把新会话刚保存的
	// 状态覆盖回去，客户端下次重连就从更早的位置续流，于是又解不开。
	ownerMu sync.Mutex
	owners  map[string]uint64

	// tokenSlots 把「所有权校验 + 快照导出 + 落库」和「所有权夺取」串行化到
	// 同一个 token 上。只做一次布尔校验挡不住 TOCTOU：旧写者通过校验之后可能
	// 被调度走，新会话趁这个窗口接管并写入更新的 next_wire_id，随后旧写者把
	// 自己那份更旧的快照写回去，客户端下次重连又从更早的位置续流。
	// 用固定条纹而不是按 token 建锁：token 会不断新增，按需建锁会让这张表
	// 无界增长；同一 stripe 上的不同 token 偶尔互相串行，代价可以忽略。
	tokenSlots [tokenSlotCount]sync.Mutex
}

// tokenSlotCount 是所有权临界区的条纹数。取足够大的 2 的幂，让 hash 分布均匀。
const tokenSlotCount = 64

// NewRealtimeServer 构建 realtime 服务。
func NewRealtimeServer(cfg config.Gateway, store Store, log *slog.Logger, tm *TunnelManager, obs *metrics.Server) *RealtimeServer {
	initRealtimeMetrics(obsRegistry(obs))
	return &RealtimeServer{
		cfg:    cfg,
		store:  store,
		log:    log,
		tm:     tm,
		sem:    make(chan struct{}, cfg.MaxConns),
		owners: make(map[string]uint64),
	}
}

// Listen 绑定端口。
func (s *RealtimeServer) Listen() error {
	ln, err := net.Listen("tcp", s.cfg.RealtimeAddr)
	if err != nil {
		return fmt.Errorf("realtime 监听 %s: %w", s.cfg.RealtimeAddr, err)
	}
	s.ln = ln
	s.log.Info("realtime TCP 监听", "addr", s.cfg.RealtimeAddr)
	return nil
}

// RealAddr 实际监听地址（Listen 后有效）。
func (s *RealtimeServer) RealAddr() string {
	if s.ln == nil {
		return ""
	}
	return s.ln.Addr().String()
}

// Serve 接受循环（阻塞）。
func (s *RealtimeServer) Serve() error {
	for {
		nc, err := s.ln.Accept()
		if err != nil {
			return err
		}
		s.sem <- struct{}{}
		go func() {
			defer func() { <-s.sem }()
			s.handleConn(nc)
		}()
	}
}

// Close 停止接受新连接，并等待写者把 handoff 落库后返回。
//
// 关闭顺序很关键：隧道断开时主服会要求断开全部会话，这些会话的写者
// goroutine 还要把 RC4 流与 wire_id 写进 handoff。如果这里不等它们结束，
// 调用方随后的 db.Close() 会让最后一次——也是最准确的一次——保存失败，
// 客户端重连时只能承接一份过期快照，keystream 立刻错位。
//
// 只等写者，不等整个会话：会话收尾随后的 sendClose 要走隧道，而 getConn
// 持全局锁串行退避与拨号，20 个会话能拖过 systemd 的 90 秒停止超时。
// 一旦被 SIGKILL，db.Close() 反而抢在 handoff 落库之前，修复目标全丢。
func (s *RealtimeServer) Close() error {
	first := !s.closing.Swap(true)
	var err error
	if s.ln != nil {
		err = s.ln.Close()
	}
	if first {
		s.dropAll("gateway_close")
	}
	// 持 writerMu 再 Wait：同时挡住新建会话的 writers.Add（WaitGroup 不允许
	// 在 Wait 进行中 Add）。已经通过检查的会话不会被丢下，Add 都在此锁内。
	s.writerMu.Lock()
	s.writers.Wait()
	s.writerMu.Unlock()
	return err
}

// byID 按 sid 取会话。
func (s *RealtimeServer) byID(sid uint64) *clientSession {
	v, ok := s.sids.Load(sid)
	if !ok {
		return nil
	}
	return v.(*clientSession)
}

// dropAll 断开全部在线会话（隧道断开时调用；客户端会自动重连并经 handoff 恢复）。
func (s *RealtimeServer) dropAll(reason string) {
	s.sids.Range(func(_, v any) bool {
		v.(*clientSession).Close(reason)
		return true
	})
}

// outbound 写任务。
type outbound struct {
	kind    int // 0=data(明文信封) 1=ack(wireID) 2=finish(保存 handoff 后退出)
	payload []byte
	wireID  uint64
}

const (
	outData   = 0
	outAck    = 1
	outFinish = 2
)

// clientSession 一条客户端 realtime 会话。
type clientSession struct {
	id      uint64
	conn    net.Conn
	token   string
	userid  int64
	rc4In   *proto.RC4Stream
	rc4Out  *proto.RC4Stream
	resumed bool
	// nextWireID 仅由写者 goroutine 消费；重连恢复时从 handoff 注入，
	// 保持 wire_id 严格递增。
	nextWireID uint64
	outCh      chan outbound
	closed     atomic.Bool
	srv        *RealtimeServer
	log        *slog.Logger

	rc4Mu          sync.Mutex   // 保护 Export（读/写协程之外的周期性快照）
	framesSent     atomic.Int64 // 已发 S→C 数据帧数（防污染守卫）
	framesParsedOK atomic.Int64 // 已成功解析的 C→S 信封数
}

func (s *RealtimeServer) handleConn(nc net.Conn) {
	rtConnsActive.Inc()
	defer rtConnsActive.Dec()
	defer nc.Close()
	remote := nc.RemoteAddr().String()
	log := s.log.With("remote", remote)

	// ---- 握手阶段（认证包 → 问候 → hermes）----
	_ = nc.SetReadDeadline(time.Now().Add(30 * time.Second))
	var buf []byte
	token, consumed, ok := proto.TryParseAuthPacket(buf)
	for !ok {
		chunk := make([]byte, 4096)
		n, err := nc.Read(chunk)
		if err != nil {
			log.Info("认证阶段连接关闭", "buffered", len(buf))
			return
		}
		buf = append(buf, chunk[:n]...)
		token, consumed, ok = proto.TryParseAuthPacket(buf)
		if ok {
			break
		}
		if len(buf) > 1<<20 {
			log.Warn("认证包超长，拒绝", "len", len(buf))
			rtAuthFailures.Inc()
			return
		}
	}
	reconnect := proto.AuthPacketReconnect(buf)
	buf = buf[consumed:]

	info, found, err := s.store.LookupToken(context.Background(), token)
	if err != nil {
		rtAuthFailures.Inc()
		log.Warn("token 校验查询异常，拒绝", "err", err, "reconnect", reconnect)
		return
	}
	if !found {
		// 客户端可能回传未登记的 logintoken。只要格式合法（至少 3 个 '|'），
		// 就从第三段解析 uid 并放行。
		uid := useridFromTokenStr(token)
		if uid <= 0 {
			rtAuthFailures.Inc()
			log.Warn("token 无法解析 uid，拒绝", "reconnect", reconnect)
			return
		}
		info = TokenInfo{Userid: uid, IssuedAt: time.Now().Unix()}
		log.Warn("token 未登记，按解析 uid 放行", "uid", uid, "reconnect", reconnect)
	}
	log = log.With("userid", info.Userid)
	log.Info("认证通过",
		"token_len", len(token), "reconnect", reconnect)

	if _, err := nc.Write([]byte(GreetingUUID)); err != nil {
		log.Info("写问候失败", "err", err)
		return
	}

	// hermes 帧（明文，payload 含 <hermes>）
	//
	// 只消费到 hermes 那一帧为止。客户端常在同一个 TCP 段里紧跟 hermes 发出
	// 第一个业务帧；若把整段解析结果一起吞掉，那一帧的 RC4 就被客户端消耗而
	// 服务端没有消耗，此后每条上行都对不上 keystream——承接的会话第一帧就失步。
	hermesSeen := false
	for !hermesSeen {
		frames, consumedBytes, err := proto.TakeCompleteFrames(buf)
		if err != nil {
			log.Warn("hermes 阶段帧解析失败", "err", err)
			return
		}
		keep := len(frames)
		for i, f := range frames {
			if f.FrameType == 0 && contains(f.Payload, hermesMarker) {
				hermesSeen = true
				keep = i + 1
				break
			}
		}
		consumed := 0
		for _, f := range frames[:keep] {
			consumed += len(f.Raw)
		}
		if consumed == 0 {
			consumed = consumedBytes
		}
		buf = buf[consumed:]
		if hermesSeen {
			break
		}
		chunk := make([]byte, 8192)
		n, err := nc.Read(chunk)
		if err != nil {
			log.Info("hermes 阶段连接关闭", "err", err)
			return
		}
		buf = append(buf, chunk[:n]...)
	}
	log.Info("Hermes 握手完成")

	// ---- RC4 初始化（重连接管优先）----
	sess := &clientSession{
		id:         s.sidSeq.Add(1),
		conn:       nc,
		token:      token,
		userid:     info.Userid,
		outCh:      make(chan outbound, 1024),
		nextWireID: 1,
		srv:        s,
		log:        log,
	}
	key := []byte(token)
	if reconnect {
		h, err := s.store.LoadHandoff(context.Background(), token)
		switch {
		case err == nil && h != nil:
			sess.rc4In = proto.Restore(h.RC4In)
			sess.rc4Out = proto.Restore(h.RC4Out)
			sess.nextWireID = h.NextWireID
			if sess.nextWireID < 1 {
				sess.nextWireID = 1
			}
			sess.resumed = true
			rtHandoffUsed.Inc()
			log.Info("realtime 重连成功：承接既有 RC4 密码流", "next_wire_id", sess.nextWireID)
		case err != nil:
			// 读承接状态本身失败（数据库故障等）。这和「确实没有状态」是两码事：
			// 此时按「没有状态」继续，会重新 KSA 出全新密码流，而客户端的流还在
			// 旧位置上——结果就是整条下行全部解不开，同时库里那份承接状态被留成
			// 永远没人再读的孤儿。正确做法是中止本次重连、把状态留着，让客户端
			// 重试或人工介入。
			rtHandoffMissing.Inc()
			log.Error("读取承接状态失败，中止本次重连并保留承接状态", "err", err)
			return
		default:
			// 客户端按「延续旧流」重连，但服务端确实没有可承接的状态。这里必须
			// 显式记录下来：随后会用同一个 token 重新 KSA 出全新密码流，而
			// 客户端的流早就走远了——除了客户端整条连接重建，没有别的救法。
			// 不区分这两种情形，日志里只剩一片「信封解析失败」，无法定位。
			rtHandoffMissing.Inc()
			log.Warn("realtime 重连但无 handoff 可承接，将新建密码流（客户端大概率解不开下行）",
				"reconnect", reconnect)
		}
	}
	if sess.rc4In == nil {
		var err error
		sess.rc4In, err = proto.NewRC4(key)
		if err != nil {
			log.Error("RC4 初始化失败", "err", err)
			return
		}
		sess.rc4Out, _ = proto.NewRC4(key)
	}

	s.sids.Store(sess.id, sess)
	defer s.sids.Delete(sess.id)

	// 通知主服开新会话
	if err := s.tm.sendOpen(sess.id, token, sess.userid, reconnect, remoteIP(remote)); err != nil {
		log.Error("隧道 OPEN 失败，断开客户端", "err", err)
		return
	}

	// 夺取该 token 的所有权：此后旧会话的周期性快照一律作废。
	// 必须早于写者启动——否则新写者可能先存一版，随后被仍在跑的旧写者
	// 用更旧的 wire_id 覆盖回去，客户端下次重连就从更早的位置续流。
	s.claimOwnership(token, sess.id)
	defer s.releaseOwnership(token, sess.id)

	// ---- 单写者 goroutine ----
	// 登记到 writers：Close 只等写者把 handoff 落库，不等随后的隧道收尾。
	// 登记与 Close 的等待用 writerMu 互斥，避免 WaitGroup 在 Wait 期间被 Add。
	s.writerMu.Lock()
	if s.closing.Load() {
		s.writerMu.Unlock()
		// 关闭已经进入等待阶段，这个会话赶不上收尾：把刚开的隧道会话也关掉，
		// 否则主服会留下一条没有 CLOSE 的悬挂会话。
		_ = s.tm.sendClose(sess.id, "gateway_close")
		return
	}
	s.writers.Add(1)
	s.writerMu.Unlock()

	// 到这里写者已登记、必然会被启动并收尾，承接状态才算真正「交出去」：
	// 写者退出前会重新保存最新快照。删除必须落在「OPEN 成功 + 写者已登记」
	// 之后、写者启动之前——放在启动之后有竞态：写者可能先刷一次快照，随后
	// 这里再删，等于把刚存的状态删掉；放在 OPEN 之前则有 P1：sendOpen 失败
	// 或撞上关闭提前返回，库里那份状态成了永远没人读的孤儿。
	if sess.resumed {
		_ = s.store.DeleteHandoff(context.Background(), token)
	}

	writerDone := make(chan struct{})
	go func() {
		defer s.writers.Done()
		sess.writer(writerDone)
	}()

	// ---- 读循环 ----
	_ = nc.SetReadDeadline(time.Now().Add(120 * time.Second))
	readBuf := buf // hermes 阶段可能残留后续帧
	for {
		frames, consumed, err := proto.TakeCompleteFrames(readBuf)
		if err != nil {
			log.Warn("帧解析失败，断开", "err", err)
			break
		}
		readBuf = readBuf[consumed:]
		done := false
		for _, f := range frames {
			if sess.handleFrame(f) {
				done = true
				break
			}
		}
		if done {
			break
		}
		chunk := make([]byte, 65536)
		n, err := nc.Read(chunk)
		if err != nil {
			log.Info("客户端断开", "err", err)
			break
		}
		readBuf = append(readBuf, chunk[:n]...)
		if len(readBuf) > 64<<20 {
			log.Warn("读缓冲超限，断开")
			break
		}
		_ = nc.SetReadDeadline(time.Now().Add(120 * time.Second))
	}

	sess.closed.Store(true)
	close(sess.outCh)
	<-writerDone // 写者收尾（含 handoff 保存）
	_ = s.tm.sendClose(sess.id, "session_end")
	s.log.Info("会话结束", "sid", sess.id, "resumed", sess.resumed)
}

// useridFromTokenStr 从 4 段 token 第三段解析 uid（解析失败回 0）。
func useridFromTokenStr(token string) int64 {
	parts := strings.Split(token, "|")
	if len(parts) < 3 {
		return 0
	}
	uid, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil {
		return 0
	}
	return uid
}

// handleFrame 处理一个客户端帧；返回 true 表示连接应结束。
func (s *clientSession) handleFrame(f *proto.Frame) bool {
	switch {
	case f.BodyLen == 0:
		// 零帧：传输层保活，常由服务端发往客户端；收到时也容忍。
		rtFramesIn.WithLabelValues("zero").Inc()
		return false
	case f.BodyLen == 1:
		rtFramesIn.WithLabelValues("type_only").Inc()
		return false
	case f.FrameType == 1:
		// 客户端 ACK 我们的数据帧：记录即可
		rtFramesIn.WithLabelValues("ack").Inc()
		return false
	case f.FrameType == 0:
		rtFramesIn.WithLabelValues("data").Inc()
		// 协议要求先 ACK 后处理；ACK 无载荷，不消耗 keystream。
		if s.srv.cfg.AckClientFrames {
			s.outCh <- outbound{kind: outAck, wireID: f.WireID}
			rtAcksSent.Inc()
		}
		plain := s.rc4In.Crypt(f.Payload)
		if err := s.srv.tm.sendData(s.id, plain); err != nil {
			s.log.Warn("隧道转发失败，断开", "err", err)
			return true
		}
		s.framesParsedOK.Add(1)
		return false
	default:
		rtFramesIn.WithLabelValues("unknown_type").Inc()
		s.log.Warn("未知帧类型", "type", f.FrameType)
		return false
	}
}

// writer 是会话的唯一写者，依次执行分配 wire_id、RC4 加密和写 socket。
// call_id 分配、RC4 消耗与发送必须保持同一顺序；主服经隧道 FIFO 下发完整信封，
// 写者只负责 wire_id、加密和输出。
func (s *clientSession) writer(done chan<- struct{}) {
	defer close(done)
	keepalive := time.NewTicker(keepaliveInterval(s.srv.cfg.TransportKeepaliveSec))
	defer keepalive.Stop()

	nextWireID := s.nextWireID
	_ = s.conn.SetWriteDeadline(time.Now().Add(30 * time.Second))

	// 周期性保存 handoff，避免进程异常退出时丢失 RC4 流和 wire_id。
	//
	// 这个 ticker 必须由写者本人在主循环里消费：nextWireID 与两个 RC4 流
	// 都只由写者推进，快照才能是「同一时刻的流 + 对应的下一个 wire_id」。
	// 曾经这里另起 goroutine 读 nextWireID，既与写者构成无同步读，抓到的
	// 也永远是落后的值，客户端重连承接后就地错位。
	periodicSave := time.NewTicker(2 * time.Second)
	defer periodicSave.Stop()

drain:
	for {
		select {
		case <-periodicSave.C:
			s.saveHandoff(nextWireID)
			continue

		case task, ok := <-s.outCh:
			if !ok {
				break drain
			}
			switch task.kind {
			case outAck:
				if _, err := s.conn.Write(proto.BuildAckFrame(task.wireID)); err != nil {
					s.log.Info("写 ACK 失败", "err", err)
					s.closed.Store(true)
					continue
				}
				_ = s.conn.SetWriteDeadline(time.Now().Add(30 * time.Second))

			case outData:
				enc := s.rc4Out.Crypt(task.payload)
				frame := proto.BuildFrame(0, nextWireID, enc)
				nextWireID++
				if _, err := s.conn.Write(frame); err != nil {
					s.log.Info("写数据帧失败", "err", err)
					s.closed.Store(true)
					continue
				}
				s.framesSent.Add(1)
				rtFramesOut.Inc()
				_ = s.conn.SetWriteDeadline(time.Now().Add(30 * time.Second))

			case outFinish:
				// 不会出现在 channel（close(outCh) 退出）；防御
			}

			if s.closed.Load() {
				break drain
			}
		}
	}

	// 收尾：保存重连接管状态（RC4 双向流 + 下一 wire_id）
	s.saveHandoff(nextWireID)
}

// claimOwnership 把 token 的所有权交给 sid。
//
// 承接状态按 token 存储，而同一 token 可能同时活着两个网关侧会话：主服已经
// 接管了旧会话，但旧 TCP 还没被读循环发现断开，旧写者仍在周期性保存快照。
// 夺取所有权后，旧写者的保存会因不再是所有者而被丢弃，不会用落后的
// wire_id 覆盖新会话刚存下的状态。
func (s *RealtimeServer) claimOwnership(token string, sid uint64) {
	// 必须与 saveHandoff 的「校验 + 落库」互斥：否则夺取动作可能插进旧写者
	// 已经通过校验、还没落库的那个窗口里，让更旧的快照在接管之后才落地。
	unlock := s.lockTokenSlot(token)
	defer unlock()
	s.ownerMu.Lock()
	s.owners[token] = sid
	s.ownerMu.Unlock()
}

// lockTokenSlot 返回该 token 所在条纹的解锁函数。
//
// 临界区的配对规则：saveHandoff 里「查所有权 → 导出快照 → 落库」必须在同一次
// 持有期内完成；claimOwnership 也持有同一把条纹锁，于是两者的先后关系被强制
// 成全序，不会出现「检查时还是旧所有者、落库时已经换了人」。
func (s *RealtimeServer) lockTokenSlot(token string) func() {
	var h uint32 = 2166136261
	for i := 0; i < len(token); i++ {
		h ^= uint32(token[i])
		h *= 16777619
	}
	lk := &s.tokenSlots[h%uint32(len(s.tokenSlots))]
	lk.Lock()
	return lk.Unlock
}

// ownsToken 判断 sid 当前是否持有该 token 的所有权。
func (s *RealtimeServer) ownsToken(token string, sid uint64) bool {
	s.ownerMu.Lock()
	defer s.ownerMu.Unlock()
	cur, ok := s.owners[token]
	return ok && cur == sid
}

// releaseOwnership 在会话收尾时交还所有权（仅当仍归自己）。
//
// 已被接管的旧会话走到这里时，owners 里记的是新会话的 sid，这里什么都不做——
// 否则会把新会话的所有权一并抹掉，让更早的旧写者重新获得保存资格。
func (s *RealtimeServer) releaseOwnership(token string, sid uint64) {
	s.ownerMu.Lock()
	defer s.ownerMu.Unlock()
	if cur, ok := s.owners[token]; ok && cur == sid {
		delete(s.owners, token)
	}
}

// saveHandoff 保存重连接管状态。非 resume 的会话如果尚未发送数据帧、
// 也尚未解析出客户端信封，则说明它还没有可恢复进度；此时不落库，
// 防止空状态覆盖已有的有效 handoff。
//
// 落库前先确认本会话仍持有该 token 的所有权，且校验与落库在同一个 token
// 临界区里完成：同一 token 的旧会话可能还在跑（主服已接管，但网关侧旧 TCP
// 还没断），它手里的 wire_id 更旧，写进去会让客户端下次重连从更早的位置续流。
func (s *clientSession) saveHandoff(nextWireID uint64) {
	if s.rc4In == nil || s.rc4Out == nil {
		return
	}
	// 所有权校验必须和导出、落库在同一临界区里，直到写进数据库才放开：
	// 只做一次布尔检查的话，旧写者通过检查后可能被调度走，新会话趁机接管
	// 并写入更新的 next_wire_id，随后旧写者把自己那份更旧的快照写回去
	// （TOCTOU），客户端下次重连就从更早的位置续流。
	unlock := s.srv.lockTokenSlot(s.token)
	defer unlock()
	if !s.srv.ownsToken(s.token, s.id) {
		return // 已被新会话接管：旧快照写不得
	}
	if !s.resumed && s.framesSent.Load() == 0 && s.framesParsedOK.Load() == 0 {
		return // 防污染：失步 fresh 会话的状态不落库（见上注）
	}
	s.rc4Mu.Lock()
	h := Handoff{
		RC4In:      s.rc4In.Export(),
		RC4Out:     s.rc4Out.Export(),
		NextWireID: nextWireID,
	}
	s.rc4Mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.srv.store.SaveHandoff(ctx, s.token, h, handoffTTL); err == nil {
		rtHandoffSaved.Inc()
		s.log.Debug("handoff 已保存", "next_wire_id", nextWireID)
	} else {
		s.log.Warn("handoff 保存失败", "err", err)
	}
}

// EnqueueData 主服下行的明文信封入队（隧道读 goroutine 调用）。
func (s *clientSession) EnqueueData(payload []byte) bool {
	if s.closed.Load() {
		return false
	}
	select {
	case s.outCh <- outbound{kind: outData, payload: payload}:
		return true
	default:
		s.log.Warn("下行队列满（慢客户端），断开")
		s.closed.Store(true)
		return false
	}
}

// Close 主动断开（主服 CLOSE 指令）。
func (s *clientSession) Close(reason string) {
	if s.closed.CompareAndSwap(false, true) {
		s.log.Info("主服要求断开", "reason", reason)
		_ = s.conn.Close()
	}
}

// ---- 辅助 ----

var hermesMarker = []byte("<hermes>")

func contains(haystack, needle []byte) bool {
	return len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0
}

func indexOf(haystack, needle []byte) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		match := true
		for j := range needle {
			if haystack[i+j] != needle[j] {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}

func remoteIP(addr string) string {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	return host
}

func keepaliveInterval(cfg float64) time.Duration {
	if cfg <= 0 {
		return 5 * time.Second
	}
	return time.Duration(cfg * float64(time.Second))
}

// handoffTTL 是重连接管状态的 TTL；客户端断线后约 2 分钟内会重连。
const handoffTTL = 10 * time.Minute
