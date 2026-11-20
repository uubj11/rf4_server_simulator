package gateway

import (
	"encoding/json"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"rf4server/internal/metrics"
	"rf4server/internal/tunnel"
)

// 隧道指标。
var (
	tlUp        prometheus.Gauge
	tlSent      *prometheus.CounterVec
	tlRecv      *prometheus.CounterVec
	tlDialTotal prometheus.Counter
)

func initTunnelMetrics(reg *prometheus.Registry) {
	tlUp = metrics.NewGauge(reg, "gateway_tunnel_up", "隧道是否在线（1/0）")
	tlSent = metrics.NewLabeledCounter(reg, "gateway_tunnel_sent_total", "隧道发送记录数", "type")
	tlRecv = metrics.NewLabeledCounter(reg, "gateway_tunnel_recv_total", "隧道接收记录数", "type")
	tlDialTotal = metrics.NewCounter(reg, "gateway_tunnel_dials_total", "隧道拨号次数")
}

// TunnelManager 网关侧隧道客户端：维持到 gameserver 的连接，收发会话记录。
//
// 隧道断开策略：杀掉全部会话的客户端连接（客户端会自动重连并经 handoff 恢复），
// 然后带退避重拨。这是最保守且协议安全的做法——主服侧会话随隧道丢失而丢失，
// 但 RC4/计数器 handoff 保证客户端无感续玩。
type TunnelManager struct {
	addr string
	psk  string
	log  *slog.Logger
	gw   *RealtimeServer

	mu      sync.Mutex
	dialMu  sync.Mutex // 串行化拨号；持它期间不睡、不持 mu
	conn    *tunnel.Conn
	retryN  int
	closed  bool
	onState func(up bool)
	// gen 是连接代际号：每成功拨号一次 +1，读循环带着自己的代际跑。
	// 收尾时比较代际，就能区分「我退出了，通道也没了」与「我退出了，
	// 但别人已经换上新连接」。只看 m.conn 指针不够：send() 写失败时会先把
	// m.conn 清成 nil 再关连接，此时读循环看到 nil 既可能是「没有新连接」，
	// 也可能是「新连接刚被别处换走」，两种情形处理方式相反。
	gen uint64
}

// errTunnelClosed 表示隧道管理器已关闭，不再拨号。
var errTunnelClosed = errors.New("隧道已关闭")

// NewTunnelManager 构建隧道管理器（不拨号；首次 sendOpen 时懒拨）。
func NewTunnelManager(addr, psk string, log *slog.Logger, obs *metrics.Server, onState func(up bool)) *TunnelManager {
	initTunnelMetrics(obsRegistry(obs))
	tlUp.Set(0)
	return &TunnelManager{addr: addr, psk: psk, log: log, onState: onState}
}

// SetGateway 绑定 realtime 服务（收下行记录时派发）。
func (m *TunnelManager) SetGateway(gw *RealtimeServer) { m.gw = gw }

// getConn 取当前隧道连接；不在则拨号（含退避）。
//
// 退避 sleep 与拨号都在 dialMu 里，绝不在 mu 里做：mu 还保护 Close 与
// readLoop 的收尾，一旦在里面睡几十秒，隧道断开时 N 个会话收尾的
// sendClose 和随后的 tm.Close() 会全部堵在这个锁上，gateway 关不掉只能
// 被 SIGKILL，handoff 也就没机会落库。
func (m *TunnelManager) getConn() (*tunnel.Conn, error) {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil, errTunnelClosed
	}
	if m.conn != nil {
		select {
		case <-m.conn.Done():
			m.conn = nil
		default:
			c := m.conn
			m.mu.Unlock()
			return c, nil
		}
	}
	m.mu.Unlock()

	// 同一时刻只允许一个拨号者；排队者进来后先看有没有现成连接。
	m.dialMu.Lock()
	defer m.dialMu.Unlock()

	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil, errTunnelClosed
	}
	if m.conn != nil {
		select {
		case <-m.conn.Done():
			m.conn = nil
		default:
			c := m.conn
			m.mu.Unlock()
			return c, nil
		}
	}
	// 指数退避（1s..32s），睡眠期间不持 mu，关闭可以立刻打断。
	backoff := time.Duration(1<<min(m.retryN, 5)) * time.Second
	needBackoff := m.retryN > 0
	m.mu.Unlock()

	if needBackoff {
		m.log.Info("隧道重连退避", "wait", backoff)
		time.Sleep(backoff)
		m.mu.Lock()
		closed := m.closed
		m.mu.Unlock()
		if closed {
			return nil, errTunnelClosed
		}
	}

	c, err := tunnel.Dial(m.addr, m.psk)

	m.mu.Lock()
	if m.closed {
		// 期间被 Close：不要把手上的连接挂上去，直接丢掉。
		m.mu.Unlock()
		if c != nil {
			_ = c.Close()
		}
		return nil, errTunnelClosed
	}
	if err != nil {
		m.retryN++
		m.mu.Unlock()
		return nil, err
	}
	m.retryN = 0
	m.conn = c
	m.gen++
	gen := m.gen
	tlUp.Set(1)
	tlDialTotal.Inc()
	m.log.Info("隧道已连接", "addr", m.addr, "remote", c.RemoteAddr())
	state := m.onState
	m.mu.Unlock()

	// 先报「已连接」再起读循环：反过来一旦连接立刻断开，readLoop 的收尾
	// 可能先报 onState(false)，随后这里又报 true，把状态永远留在「在线」。
	if state != nil {
		state(true)
	}
	go m.readLoop(c, gen)
	return c, nil
}

// readLoop 读隧道下行记录并派发到会话。
//
// gen 是本次连接建立时分配的代际号，收尾时用它判断自己是不是「还在岗」。
func (m *TunnelManager) readLoop(c *tunnel.Conn, gen uint64) {
	defer func() {
		// 收尾要分清两种情况：
		//   1. 退出的就是当前连接（代际仍等于自己）——隧道真的断了，
		//      清在线指标、报离线、踢掉全部会话让客户端重连；
		//   2. 别人已经换上新连接（代际变大了）——旧连接只关自己，
		//      不能碰任何全局状态，否则会把新隧道上的健康会话一起杀掉。
		//
		// 判据用代际而不是 m.conn == c：send() 写失败时会先把 m.conn 清成
		// nil 再关连接，读循环随后看到 nil。若把「指针不等」当成已被接替，
		// nil 这两种含义（没有新连接 / 新连接刚被换走）就被混在一处，
		// 结果是离线回调与 dropAll 都被跳过，主服已没有对应隧道会话、
		// 网关侧会话却还挂着，后续请求全部静默丢失。
		m.mu.Lock()
		superseded := m.gen != gen
		if !superseded {
			m.conn = nil
			m.retryN++
		}
		m.mu.Unlock()
		_ = c.Close()
		if superseded {
			return
		}
		tlUp.Set(0)
		if m.onState != nil {
			m.onState(false)
		}
		m.log.Warn("隧道断开：杀掉全部在线会话（客户端将自动重连并恢复）")
		if m.gw != nil {
			m.gw.dropAll("tunnel_down")
		}
	}()
	for {
		rt, sid, payload, err := c.ReadRecord()
		if err != nil {
			return
		}
		tlRecv.WithLabelValues(rt.String()).Inc()
		switch rt {
		case tunnel.TypeData:
			if m.gw == nil {
				continue
			}
			if s := m.gw.byID(sid); s != nil {
				s.EnqueueData(payload)
			}
		case tunnel.TypeClose:
			if m.gw != nil {
				if s := m.gw.byID(sid); s != nil {
					s.Close(string(payload))
				}
			}
		case tunnel.TypePing:
			_ = c.WriteRecord(tunnel.TypePong, 0, nil)
		case tunnel.TypeOpen, tunnel.TypePong:
			// 网关不收 OPEN；PONG 忽略
		}
	}
}

// sendOpen 发送 OPEN。
func (m *TunnelManager) sendOpen(sid uint64, token string, userid int64, reconnect bool, clientIP string) error {
	op := tunnel.OpenPayload{Token: token, Userid: userid, Reconnect: reconnect, ClientIP: clientIP}
	b, err := json.Marshal(op)
	if err != nil {
		return err
	}
	return m.send(tunnel.TypeOpen, sid, b)
}

// sendData 转发客户端明文信封。
func (m *TunnelManager) sendData(sid uint64, payload []byte) error {
	return m.send(tunnel.TypeData, sid, payload)
}

// sendClose 通知主服会话结束。
func (m *TunnelManager) sendClose(sid uint64, reason string) error {
	return m.send(tunnel.TypeClose, sid, []byte(reason))
}

func (m *TunnelManager) send(rt tunnel.RecordType, sid uint64, payload []byte) error {
	c, err := m.getConn()
	if err != nil {
		return err
	}
	if err := c.WriteRecord(rt, sid, payload); err != nil {
		m.log.Warn("隧道发送失败", "type", rt, "err", err)
		_ = c.Close()
		m.mu.Lock()
		if m.conn == c {
			m.conn = nil
		}
		m.mu.Unlock()
		return err
	}
	tlSent.WithLabelValues(rt.String()).Inc()
	return nil
}

// Close 关闭当前隧道并阻止后续拨号。
func (m *TunnelManager) Close() {
	m.mu.Lock()
	m.closed = true
	c := m.conn
	m.conn = nil
	m.mu.Unlock()
	if c != nil {
		_ = c.Close()
	}
}
