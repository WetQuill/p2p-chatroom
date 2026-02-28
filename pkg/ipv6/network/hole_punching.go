package network

import (
	"errors"
	"net"
	"sync"
	"time"
)

// HolePunchingManager UDP打洞管理器
type HolePunchingManager struct {
	transport    *UDPTransport
	stunClient   *STUNClient
	holePunchMap map[string]*HolePunchSession
	rendezvous   *RendezvousClient
	mu           sync.RWMutex
}

// HolePunchSession 打洞会话
type HolePunchSession struct {
	ID              string
	RemoteAddr      string
	LocalAddr       string
	ViaRendezvous   bool
	State           HolePunchState
	CreatedAt       time.Time
	LastAttempt     time.Time
	AttemptCount    int
	MaxAttempts     int
	SuccessCallback func(*UDPConnection)
	ErrorCallback   func(error)
}

// HolePunchState 打洞状态枚举
type HolePunchState string

const (
	HolePunchStateInitial    HolePunchState = "initial"
	HolePunchStateProbing    HolePunchState = "probing"
	HolePunchStatePunching   HolePunchState = "punching"
	HolePunchStateEstablished HolePunchState = "established"
	HolePunchStateFailed     HolePunchState = "failed"
	HolePunchStateTimeout    HolePunchState = "timeout"
)

// STUNClient STUN客户端
type STUNClient struct {
	servers []string
	timeout time.Duration
}

// RendezvousClient 会合服务器客户端
type RendezvousClient struct {
	serverAddr string
	timeout    time.Duration
}

// NewHolePunchingManager 创建打洞管理器
func NewHolePunchingManager(transport *UDPTransport) *HolePunchingManager {
	return &HolePunchingManager{
		transport:    transport,
		holePunchMap: make(map[string]*HolePunchSession),
		stunClient: &STUNClient{
			servers: []string{
				"stun1.l.google.com:19302",
				"stun2.l.google.com:19302",
				"stun3.l.google.com:19302",
			},
			timeout: 5 * time.Second,
		},
	}
}

// PunchHole 执行打洞
func (hpm *HolePunchingManager) PunchHole(remoteAddr string, viaRendezvous bool) (*UDPConnection, error) {
	sessionID := generateSessionID(hpm.transport.localAddr.String(), remoteAddr)

	hpm.mu.Lock()
	// 检查是否已有会话
	if session, exists := hpm.holePunchMap[sessionID]; exists {
		hpm.mu.Unlock()
		if session.State == HolePunchStateEstablished {
			return hpm.transport.GetConnectionByID(sessionID), nil
		}
		// 等待打洞完成
		return nil, errors.New("hole punching in progress")
	}

	// 创建新会话
	session := &HolePunchSession{
		ID:            sessionID,
		RemoteAddr:    remoteAddr,
		LocalAddr:     hpm.transport.localAddr.String(),
		ViaRendezvous: viaRendezvous,
		State:         HolePunchStateInitial,
		CreatedAt:     time.Now(),
		MaxAttempts:   5,
	}

	hpm.holePunchMap[sessionID] = session
	hpm.mu.Unlock()

	// 启动打洞协程
	go hpm.performHolePunch(session)

	// 等待打洞完成
	return hpm.waitForCompletion(session)
}

// MaintainHole 维护打洞
func (hpm *HolePunchingManager) MaintainHole(sessionID string) error {
	hpm.mu.RLock()
	session, exists := hpm.holePunchMap[sessionID]
	hpm.mu.RUnlock()

	if !exists {
		return errors.New("session not found")
	}

	// 检查是否需要重新打洞
	if session.State == HolePunchStateEstablished {
		// 定期发送保活包
		go hpm.sendKeepAlive(session)
	}

	return nil
}

// CloseHole 关闭打洞
func (hpm *HolePunchingManager) CloseHole(sessionID string) {
	hpm.mu.Lock()
	defer hpm.mu.Unlock()

	delete(hpm.holePunchMap, sessionID)
}

// SetRendezvousServer 设置会合服务器
func (hpm *HolePunchingManager) SetRendezvousServer(addr string) {
	hpm.rendezvous = &RendezvousClient{
		serverAddr: addr,
		timeout:    10 * time.Second,
	}
}

// 私有方法

func (hpm *HolePunchingManager) performHolePunch(session *HolePunchSession) {
	hpm.updateSessionState(session, HolePunchStateProbing)

	// 步骤1：获取公网地址和端口
	publicAddr, err := hpm.getPublicAddress()
	if err != nil {
		hpm.handleHolePunchError(session, err)
		return
	}

	// 步骤2：如果通过会合服务器，进行协调
	if session.ViaRendezvous && hpm.rendezvous != nil {
		err = hpm.coordinateViaRendezvous(session, publicAddr)
		if err != nil {
			hpm.handleHolePunchError(session, err)
			return
		}
	}

	// 步骤3：执行打洞
	hpm.updateSessionState(session, HolePunchStatePunching)
	conn, err := hpm.executeHolePunch(session, publicAddr)
	if err != nil {
		hpm.handleHolePunchError(session, err)
		return
	}

	// 步骤4：打洞成功
	hpm.updateSessionState(session, HolePunchStateEstablished)
	hpm.onHolePunchSuccess(session, conn)
}

func (hpm *HolePunchingManager) getPublicAddress() (string, error) {
	// 使用STUN获取公网地址
	// 简化实现，实际需要完整的STUN协议实现
	return hpm.transport.localAddr.String(), nil
}

func (hpm *HolePunchingManager) coordinateViaRendezvous(session *HolePunchSession, publicAddr string) error {
	// 通过会合服务器协调打洞
	// 1. 注册自己的公网地址
	// 2. 获取对端的公网地址
	// 3. 交换打洞信息

	// 简化实现
	return nil
}

func (hpm *HolePunchingManager) executeHolePunch(session *HolePunchSession, publicAddr string) (*UDPConnection, error) {
	// IPv6 UDP打洞策略：
	// 1. 双方同时向对方的公网地址发送UDP包
	// 2. 这会触发防火墙创建临时规则
	// 3. 后续通信就可以通过这个"洞"进行

	remoteUDPAddr, err := net.ResolveUDPAddr("udp", session.RemoteAddr)
	if err != nil {
		return nil, err
	}

	// 尝试直接连接
	conn, err := hpm.transport.dialDirect(remoteUDPAddr)
	if err == nil {
		return conn, nil
	}

	// 如果直接连接失败，尝试打洞
	session.AttemptCount++

	// 发送打洞包
	punchPacket := []byte{0x01, 0x02, 0x03, 0x04} // 简单的打洞包
	for i := 0; i < 3; i++ {
		_, err = hpm.transport.conn.WriteToUDP(punchPacket, remoteUDPAddr)
		if err == nil {
			// 等待对方响应
			time.Sleep(100 * time.Millisecond)
			break
		}
		time.Sleep(200 * time.Millisecond)
	}

	// 再次尝试连接
	return hpm.transport.dialDirect(remoteUDPAddr)
}

func (hpm *HolePunchingManager) waitForCompletion(session *HolePunchSession) (*UDPConnection, error) {
	timeout := time.After(30 * time.Second)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			hpm.mu.RLock()
			session, exists := hpm.holePunchMap[session.ID]
			hpm.mu.RUnlock()

			if !exists {
				return nil, errors.New("session removed")
			}

			switch session.State {
			case HolePunchStateEstablished:
				return hpm.transport.GetConnectionByID(session.ID), nil
			case HolePunchStateFailed, HolePunchStateTimeout:
				return nil, errors.New("hole punching failed: " + string(session.State))
			}
		case <-timeout:
			hpm.updateSessionState(session, HolePunchStateTimeout)
			return nil, errors.New("hole punching timeout")
		}
	}
}

func (hpm *HolePunchingManager) handleHolePunchError(session *HolePunchSession, err error) {
	hpm.updateSessionState(session, HolePunchStateFailed)

	// 记录错误
	hpm.mu.Lock()
	delete(hpm.holePunchMap, session.ID)
	hpm.mu.Unlock()

	// 调用错误回调
	if session.ErrorCallback != nil {
		session.ErrorCallback(err)
	}
}

func (hpm *HolePunchingManager) onHolePunchSuccess(session *HolePunchSession, conn *UDPConnection) {
	// 调用成功回调
	if session.SuccessCallback != nil {
		session.SuccessCallback(conn)
	}
}

func (hpm *HolePunchingManager) updateSessionState(session *HolePunchSession, state HolePunchState) {
	hpm.mu.Lock()
	defer hpm.mu.Unlock()

	if s, exists := hpm.holePunchMap[session.ID]; exists {
		s.State = state
		s.LastAttempt = time.Now()
	}
}

func (hpm *HolePunchingManager) sendKeepAlive(session *HolePunchSession) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			hpm.mu.RLock()
			_, exists := hpm.holePunchMap[session.ID]
			hpm.mu.RUnlock()

			if !exists {
				return
			}

			// 发送保活包
			conn := hpm.transport.GetConnectionByID(session.ID)
			if conn != nil {
				keepAlive := []byte{0x00}
				conn.Send(keepAlive)
			}
		}
	}
}

// GetSessions 获取所有打洞会话
func (hpm *HolePunchingManager) GetSessions() []*HolePunchSession {
	hpm.mu.RLock()
	defer hpm.mu.RUnlock()

	sessions := make([]*HolePunchSession, 0, len(hpm.holePunchMap))
	for _, session := range hpm.holePunchMap {
		sessions = append(sessions, session)
	}
	return sessions
}

// GetStats 获取打洞统计
func (hpm *HolePunchingManager) GetStats() HolePunchStats {
	hpm.mu.RLock()
	defer hpm.mu.RUnlock()

	stats := HolePunchStats{
		TotalAttempts:  0,
		SuccessCount:   0,
		FailureCount:   0,
		TimeoutCount:   0,
		ActiveSessions: len(hpm.holePunchMap),
	}

	for _, session := range hpm.holePunchMap {
		stats.TotalAttempts += session.AttemptCount
		switch session.State {
		case HolePunchStateEstablished:
			stats.SuccessCount++
		case HolePunchStateFailed:
			stats.FailureCount++
		case HolePunchStateTimeout:
			stats.TimeoutCount++
		}
	}

	return stats
}

// HolePunchStats 打洞统计
type HolePunchStats struct {
	TotalAttempts  int `json:"total_attempts"`
	SuccessCount   int `json:"success_count"`
	FailureCount   int `json:"failure_count"`
	TimeoutCount   int `json:"timeout_count"`
	ActiveSessions int `json:"active_sessions"`
}

// 辅助函数
func generateSessionID(localAddr, remoteAddr string) string {
	return "holepunch:" + localAddr + ":" + remoteAddr
}