package network

import (
	"errors"
	"net"
	"sync"
	"time"
)

// UDPTransport UDP传输层实现
type UDPTransport struct {
	mu            sync.RWMutex
	conn          *net.UDPConn
	localAddr     net.Addr
	holePunching  *HolePunchingManager
	connections   map[string]*UDPConnection
	natType       NATType
	packetHandler PacketHandler

	// 配置
	enableHolePunching bool
	keepAliveInterval time.Duration
	maxPacketSize    int
	listenAddress    string
}

// UDPConnection UDP连接
type UDPConnection struct {
	ID              string
	RemoteAddr      *net.UDPAddr
	LocalAddr       *net.UDPAddr
	Established     time.Time
	LastActivity    time.Time
	BytesSent       uint64
	BytesReceived   uint64
	IsSecure        bool
	IsHolePunched   bool
	CloseChan       chan struct{}
	SendQueue       chan []byte
}

// NATType NAT类型枚举
type NATType string

const (
	NATTypeNone              NATType = "none"
	NATTypeFullCone          NATType = "full_cone"
	NATTypeRestrictedCone    NATType = "restricted_cone"
	NATTypePortRestrictedCone NATType = "port_restricted_cone"
	NATTypeSymmetric         NATType = "symmetric"
	NATTypeIPv6Firewall      NATType = "ipv6_firewall"
)

// PacketHandler 数据包处理器接口
type PacketHandler interface {
	HandlePacket(data []byte, remoteAddr net.Addr) error
}

// NewUDPTransport 创建UDP传输层
func NewUDPTransport(listenAddr string) (*UDPTransport, error) {
	transport := &UDPTransport{
		connections:        make(map[string]*UDPConnection),
		enableHolePunching: true,
		keepAliveInterval:  30 * time.Second,
		maxPacketSize:      1400, // 考虑MTU
		listenAddress:      listenAddr,
	}

	if err := transport.init(); err != nil {
		return nil, err
	}

	return transport, nil
}

// init 初始化UDP传输层
func (ut *UDPTransport) init() error {
	// 解析监听地址
	udpAddr, err := net.ResolveUDPAddr("udp", ut.listenAddress)
	if err != nil {
		return err
	}

	// 创建UDP连接
	conn, err := net.ListenUDP("udp", udpAddr)
	if err != nil {
		return err
	}

	ut.conn = conn
	ut.localAddr = conn.LocalAddr()

	// 初始化打洞管理器
	if ut.enableHolePunching {
		ut.holePunching = NewHolePunchingManager(ut)
	}

	// 启动接收协程
	go ut.receiveLoop()

	// 启动保活协程
	go ut.keepAliveLoop()

	return nil
}

// Dial 拨号到远程地址
func (ut *UDPTransport) Dial(addr string) (*UDPConnection, error) {
	remoteAddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return nil, err
	}

	// 尝试直接连接
	conn, err := ut.dialDirect(remoteAddr)
	if err == nil {
		return conn, nil
	}

	// 如果需要打洞，尝试打洞连接
	if ut.enableHolePunching {
		return ut.holePunching.PunchHole(remoteAddr.String(), false)
	}

	return nil, err
}

// Listen 开始监听（已在上层实现）
func (ut *UDPTransport) Listen() error {
	// UDP连接已在init中创建
	return nil
}

// Close 关闭传输层
func (ut *UDPTransport) Close() error {
	ut.mu.Lock()
	defer ut.mu.Unlock()

	if ut.conn != nil {
		ut.conn.Close()
		ut.conn = nil
	}

	// 关闭所有连接
	for _, conn := range ut.connections {
		conn.Close()
	}
	ut.connections = make(map[string]*UDPConnection)

	return nil
}

// GetConnections 获取所有活跃连接
func (ut *UDPTransport) GetConnections() []*UDPConnection {
	ut.mu.RLock()
	defer ut.mu.RUnlock()

	conns := make([]*UDPConnection, 0, len(ut.connections))
	for _, conn := range ut.connections {
		conns = append(conns, conn)
	}
	return conns
}

// GetConnectionByID 通过ID获取连接
func (ut *UDPTransport) GetConnectionByID(id string) *UDPConnection {
	ut.mu.RLock()
	defer ut.mu.RUnlock()

	return ut.connections[id]
}

// SetPacketHandler 设置数据包处理器
func (ut *UDPTransport) SetPacketHandler(handler PacketHandler) {
	ut.mu.Lock()
	defer ut.mu.Unlock()

	ut.packetHandler = handler
}

// 私有方法

func (ut *UDPTransport) dialDirect(remoteAddr *net.UDPAddr) (*UDPConnection, error) {
	connID := generateConnectionID(ut.localAddr.String(), remoteAddr.String())

	ut.mu.Lock()
	defer ut.mu.Unlock()

	// 检查是否已存在连接
	if conn, exists := ut.connections[connID]; exists {
		return conn, nil
	}

	// 创建新连接
	conn := &UDPConnection{
		ID:           connID,
		RemoteAddr:   remoteAddr,
		LocalAddr:    ut.conn.LocalAddr().(*net.UDPAddr),
		Established:  time.Now(),
		LastActivity: time.Now(),
		CloseChan:    make(chan struct{}),
		SendQueue:    make(chan []byte, 100),
	}

	// 存储连接
	ut.connections[connID] = conn

	// 启动发送协程
	go ut.sendLoop(conn)

	return conn, nil
}

func (ut *UDPTransport) receiveLoop() {
	buffer := make([]byte, ut.maxPacketSize)

	for {
		select {
		default:
			// 读取数据包
			n, addr, err := ut.conn.ReadFromUDP(buffer)
			if err != nil {
				// 检查是否是关闭错误
				if errors.Is(err, net.ErrClosed) {
					return
				}
				continue
			}

			// 复制数据
			data := make([]byte, n)
			copy(data, buffer[:n])

			// 处理数据包
			go ut.handlePacket(data, addr)
		}
	}
}

func (ut *UDPTransport) handlePacket(data []byte, remoteAddr *net.UDPAddr) {
	// 查找对应的连接
	connID := generateConnectionID(ut.localAddr.String(), remoteAddr.String())

	ut.mu.RLock()
	conn, exists := ut.connections[connID]
	ut.mu.RUnlock()

	if exists {
		conn.LastActivity = time.Now()
		conn.BytesReceived += uint64(len(data))
	}

	// 调用数据包处理器
	if ut.packetHandler != nil {
		ut.packetHandler.HandlePacket(data, remoteAddr)
	}
}

func (ut *UDPTransport) sendLoop(conn *UDPConnection) {
	for {
		select {
		case data := <-conn.SendQueue:
			// 发送数据
			_, err := ut.conn.WriteToUDP(data, conn.RemoteAddr)
			if err != nil {
				// 发送失败，可能需要重新连接
				continue
			}
			conn.LastActivity = time.Now()
			conn.BytesSent += uint64(len(data))
		case <-conn.CloseChan:
			// 关闭连接
			ut.mu.Lock()
			delete(ut.connections, conn.ID)
			ut.mu.Unlock()
			return
		}
	}
}

func (ut *UDPTransport) keepAliveLoop() {
	ticker := time.NewTicker(ut.keepAliveInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			ut.sendKeepAlive()
		}
	}
}

func (ut *UDPTransport) sendKeepAlive() {
	ut.mu.RLock()
	conns := make([]*UDPConnection, 0, len(ut.connections))
	for _, conn := range ut.connections {
		conns = append(conns, conn)
	}
	ut.mu.RUnlock()

	// 发送保活包
	keepAliveData := []byte{0x00} // 简单保活包

	for _, conn := range conns {
		select {
		case conn.SendQueue <- keepAliveData:
			// 发送成功
		default:
			// 队列满，连接可能有问题
		}
	}
}

// Close 关闭UDP连接
func (uc *UDPConnection) Close() {
	select {
	case uc.CloseChan <- struct{}{}:
		// 发送关闭信号
	default:
		// 已经关闭
	}
}

// Send 发送数据
func (uc *UDPConnection) Send(data []byte) error {
	select {
	case uc.SendQueue <- data:
		return nil
	default:
		return errors.New("send queue full")
	}
}

// IsActive 检查连接是否活跃
func (uc *UDPConnection) IsActive() bool {
	return time.Since(uc.LastActivity) < 2*time.Minute
}

// 辅助函数
func generateConnectionID(localAddr, remoteAddr string) string {
	return localAddr + "->" + remoteAddr
}