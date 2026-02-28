package security

import (
	"crypto/tls"
	"errors"
	"net"
	"sync"
	"time"

	"github.com/WetQuill/p2p-chatroom/pkg/ipv6"
	"github.com/WetQuill/p2p-chatroom/pkg/ipv6/security/noise"
)

// SecureConn 安全连接包装器
type SecureConn struct {
	mu         sync.RWMutex
	conn       net.Conn
	encryptor  *noise.CipherState
	decryptor  *noise.CipherState
	noiseSession *noise.NoiseSession
	peerID     ipv6.PeerID
	verified   bool
	closed     bool

	// 统计
	bytesSent     uint64
	bytesReceived uint64
	lastActivity  time.Time
}

// NewSecureConn 创建新的安全连接
func NewSecureConn(conn net.Conn, peerID ipv6.PeerID) *SecureConn {
	return &SecureConn{
		conn:         conn,
		peerID:       peerID,
		lastActivity: time.Now(),
	}
}

// Handshake 执行握手
func (sc *SecureConn) Handshake(config *noise.NoiseConfig) error {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	if sc.closed {
		return errors.New("connection closed")
	}

	// 创建Noise协议实例
	protocol, err := noise.NewNoiseProtocol(config)
	if err != nil {
		return err
	}

	// 执行握手
	session, err := protocol.PerformHandshake(sc.conn)
	if err != nil {
		return err
	}

	sc.noiseSession = session
	sc.encryptor = session.SendState
	sc.decryptor = session.RecvState
	sc.verified = true

	// 更新活动时间
	sc.lastActivity = time.Now()

	return nil
}

// Read 读取数据
func (sc *SecureConn) Read(b []byte) (int, error) {
	sc.mu.RLock()
	if sc.closed {
		sc.mu.RUnlock()
		return 0, errors.New("connection closed")
	}
	sc.mu.RUnlock()

	// 读取加密数据
	encryptedData := make([]byte, 4096) // 初始缓冲区大小
	n, err := sc.conn.Read(encryptedData)
	if err != nil {
		return 0, err
	}

	encryptedData = encryptedData[:n]

	// 解密数据
	var plaintext []byte
	if sc.decryptor != nil {
		plaintext, err = sc.decryptor.DecryptWithAd(nil, encryptedData)
		if err != nil {
			return 0, err
		}
	} else {
		// 未加密，直接使用
		plaintext = encryptedData
	}

	// 复制到输出缓冲区
	copySize := len(plaintext)
	if copySize > len(b) {
		copySize = len(b)
	}
	copy(b[:copySize], plaintext)

	// 更新统计
	sc.mu.Lock()
	sc.bytesReceived += uint64(n)
	sc.lastActivity = time.Now()
	sc.mu.Unlock()

	return copySize, nil
}

// Write 写入数据
func (sc *SecureConn) Write(b []byte) (int, error) {
	sc.mu.RLock()
	if sc.closed {
		sc.mu.RUnlock()
		return 0, errors.New("connection closed")
	}
	sc.mu.RUnlock()

	var dataToSend []byte
	var err error

	// 加密数据
	if sc.encryptor != nil {
		dataToSend, err = sc.encryptor.EncryptWithAd(nil, b)
		if err != nil {
			return 0, err
		}
	} else {
		// 未加密，直接使用
		dataToSend = b
	}

	// 发送数据
	n, err := sc.conn.Write(dataToSend)
	if err != nil {
		return 0, err
	}

	// 更新统计
	sc.mu.Lock()
	sc.bytesSent += uint64(len(dataToSend))
	sc.lastActivity = time.Now()
	sc.mu.Unlock()

	// 返回原始数据的长度（不是加密后的长度）
	return len(b), nil
}

// Close 关闭连接
func (sc *SecureConn) Close() error {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	if sc.closed {
		return nil
	}

	sc.closed = true
	return sc.conn.Close()
}

// LocalAddr 获取本地地址
func (sc *SecureConn) LocalAddr() net.Addr {
	return sc.conn.LocalAddr()
}

// RemoteAddr 获取远程地址
func (sc *SecureConn) RemoteAddr() net.Addr {
	return sc.conn.RemoteAddr()
}

// SetDeadline 设置截止时间
func (sc *SecureConn) SetDeadline(t time.Time) error {
	return sc.conn.SetDeadline(t)
}

// SetReadDeadline 设置读取截止时间
func (sc *SecureConn) SetReadDeadline(t time.Time) error {
	return sc.conn.SetReadDeadline(t)
}

// SetWriteDeadline 设置写入截止时间
func (sc *SecureConn) SetWriteDeadline(t time.Time) error {
	return sc.conn.SetWriteDeadline(t)
}

// GetPeerID 获取对端ID
func (sc *SecureConn) GetPeerID() ipv6.PeerID {
	return sc.peerID
}

// IsVerified 检查是否已验证
func (sc *SecureConn) IsVerified() bool {
	sc.mu.RLock()
	defer sc.mu.RUnlock()
	return sc.verified
}

// GetStats 获取连接统计
func (sc *SecureConn) GetStats() ConnectionStats {
	sc.mu.RLock()
	defer sc.mu.RUnlock()

	return ConnectionStats{
		BytesSent:     sc.bytesSent,
		BytesReceived: sc.bytesReceived,
		LastActivity:  sc.lastActivity,
		IsEncrypted:   sc.encryptor != nil,
		IsVerified:    sc.verified,
	}
}

// ConnectionStats 连接统计
type ConnectionStats struct {
	BytesSent     uint64    `json:"bytes_sent"`
	BytesReceived uint64    `json:"bytes_received"`
	LastActivity  time.Time `json:"last_activity"`
	IsEncrypted   bool      `json:"is_encrypted"`
	IsVerified    bool      `json:"is_verified"`
}

// SecurityManager 安全管理器
type SecurityManager struct {
	mu           sync.RWMutex
	connections  map[string]*SecureConn
	noiseManager *noise.NoiseManager
	tlsConfig    *tls.Config
	config       *SecurityConfig
}

// SecurityConfig 安全配置
type SecurityConfig struct {
	EncryptionMode   string                  `json:"encryption_mode"` // "none", "noise", "tls"
	NoiseConfig      *noise.NoiseConfig      `json:"noise_config,omitempty"`
	TLSConfig        *TLSConfig              `json:"tls_config,omitempty"`
	RequireAuth      bool                    `json:"require_auth"`
	AllowPlaintext   bool                    `json:"allow_plaintext"`
	SessionTimeout   time.Duration           `json:"session_timeout"`
	KeyRotation      time.Duration           `json:"key_rotation"`
}

// TLSConfig TLS配置
type TLSConfig struct {
	CertFile        string   `json:"cert_file"`
	KeyFile         string   `json:"key_file"`
	CAFile          string   `json:"ca_file,omitempty"`
	VerifyPeerCert  bool     `json:"verify_peer_cert"`
	MinVersion      uint16   `json:"min_version"`
	MaxVersion      uint16   `json:"max_version"`
	CipherSuites    []string `json:"cipher_suites,omitempty"`
}

// NewSecurityManager 创建安全管理器
func NewSecurityManager(config *SecurityConfig) (*SecurityManager, error) {
	if config == nil {
		config = DefaultSecurityConfig()
	}

	var noiseManager *noise.NoiseManager
	if config.EncryptionMode == "noise" && config.NoiseConfig != nil {
		var err error
		noiseManager, err = noise.NewNoiseManager(config.NoiseConfig)
		if err != nil {
			return nil, err
		}
	}

	var tlsConfig *tls.Config
	if config.EncryptionMode == "tls" && config.TLSConfig != nil {
		var err error
		tlsConfig, err = loadTLSConfig(config.TLSConfig)
		if err != nil {
			return nil, err
		}
	}

	return &SecurityManager{
		connections:  make(map[string]*SecureConn),
		noiseManager: noiseManager,
		tlsConfig:    tlsConfig,
		config:       config,
	}, nil
}

// SecureConnection 安全连接
func (sm *SecurityManager) SecureConnection(conn net.Conn, peerID ipv6.PeerID) (*SecureConn, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	// 检查是否已存在连接
	connKey := conn.RemoteAddr().String() + ":" + string(peerID)
	if existing, exists := sm.connections[connKey]; exists {
		return existing, nil
	}

	// 创建安全连接
	secureConn := NewSecureConn(conn, peerID)

	// 根据配置执行安全握手
	switch sm.config.EncryptionMode {
	case "noise":
		if sm.noiseManager != nil {
			err := secureConn.Handshake(sm.config.NoiseConfig)
			if err != nil {
				return nil, err
			}
		}
	case "tls":
		if sm.tlsConfig != nil {
			tlsConn := tls.Client(conn, sm.tlsConfig)
			err := tlsConn.Handshake()
			if err != nil {
				return nil, err
			}
			// 用TLS连接替换原始连接
			secureConn.conn = tlsConn
			secureConn.verified = true
		}
	case "none":
		// 不加密，仅验证
		secureConn.verified = sm.config.RequireAuth
	default:
		return nil, errors.New("unsupported encryption mode: " + sm.config.EncryptionMode)
	}

	// 存储连接
	sm.connections[connKey] = secureConn

	return secureConn, nil
}

// CloseConnection 关闭连接
func (sm *SecurityManager) CloseConnection(peerID ipv6.PeerID) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	for key, conn := range sm.connections {
		if conn.GetPeerID() == peerID {
			delete(sm.connections, key)
			return conn.Close()
		}
	}

	return errors.New("connection not found")
}

// GetConnection 获取连接
func (sm *SecurityManager) GetConnection(peerID ipv6.PeerID) *SecureConn {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	for _, conn := range sm.connections {
		if conn.GetPeerID() == peerID {
			return conn
		}
	}

	return nil
}

// GetAllConnections 获取所有连接
func (sm *SecurityManager) GetAllConnections() []*SecureConn {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	connections := make([]*SecureConn, 0, len(sm.connections))
	for _, conn := range sm.connections {
		connections = append(connections, conn)
	}
	return connections
}

// CleanupStaleConnections 清理过期连接
func (sm *SecurityManager) CleanupStaleConnections() int {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	now := time.Now()
	staleCount := 0

	for key, conn := range sm.connections {
		stats := conn.GetStats()
		if now.Sub(stats.LastActivity) > sm.config.SessionTimeout {
			conn.Close()
			delete(sm.connections, key)
			staleCount++
		}
	}

	return staleCount
}

// GetStats 获取安全统计
func (sm *SecurityManager) GetStats() SecurityStats {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	stats := SecurityStats{
		TotalConnections: len(sm.connections),
		EncryptedCount:   0,
		VerifiedCount:    0,
		TotalBytesSent:   0,
		TotalBytesRecv:   0,
	}

	for _, conn := range sm.connections {
		connStats := conn.GetStats()
		if connStats.IsEncrypted {
			stats.EncryptedCount++
		}
		if connStats.IsVerified {
			stats.VerifiedCount++
		}
		stats.TotalBytesSent += connStats.BytesSent
		stats.TotalBytesRecv += connStats.BytesReceived
	}

	return stats
}

// SecurityStats 安全统计
type SecurityStats struct {
	TotalConnections int     `json:"total_connections"`
	EncryptedCount   int     `json:"encrypted_count"`
	VerifiedCount    int     `json:"verified_count"`
	TotalBytesSent   uint64  `json:"total_bytes_sent"`
	TotalBytesRecv   uint64  `json:"total_bytes_recv"`
}

// DefaultSecurityConfig 默认安全配置
func DefaultSecurityConfig() *SecurityConfig {
	return &SecurityConfig{
		EncryptionMode: "noise",
		NoiseConfig:    noise.DefaultNoiseConfig(),
		RequireAuth:    true,
		AllowPlaintext: false,
		SessionTimeout: 30 * time.Minute,
		KeyRotation:    24 * time.Hour,
	}
}

// loadTLSConfig 加载TLS配置
func loadTLSConfig(tlsConfig *TLSConfig) (*tls.Config, error) {
	if tlsConfig == nil {
		return nil, errors.New("tls config is nil")
	}

	config := &tls.Config{
		MinVersion: tls.VersionTLS12,
	}

	// 加载证书
	if tlsConfig.CertFile != "" && tlsConfig.KeyFile != "" {
		cert, err := tls.LoadX509KeyPair(tlsConfig.CertFile, tlsConfig.KeyFile)
		if err != nil {
			return nil, err
		}
		config.Certificates = []tls.Certificate{cert}
	}

	// 设置TLS版本
	if tlsConfig.MinVersion > 0 {
		config.MinVersion = tlsConfig.MinVersion
	}
	if tlsConfig.MaxVersion > 0 {
		config.MaxVersion = tlsConfig.MaxVersion
	}

	// 验证对端证书
	if tlsConfig.VerifyPeerCert {
		config.ClientAuth = tls.RequireAndVerifyClientCert
		if tlsConfig.CAFile != "" {
			// 在实际实现中需要加载CA证书
		}
	}

	return config, nil
}

// SecurityError 安全错误
type SecurityError struct {
	Operation string
	PeerID    ipv6.PeerID
	Reason    string
}

func (se *SecurityError) Error() string {
	return se.Operation + " failed for peer " + string(se.PeerID) + ": " + se.Reason
}

// VerifyPeer 验证对端身份
func (sm *SecurityManager) VerifyPeer(peerID ipv6.PeerID, publicKey []byte) bool {
	// 在实际实现中，这里应该验证对端的公钥
	// 简化实现：总是返回true
	return true
}

// RotateKeys 轮换密钥
func (sm *SecurityManager) RotateKeys() error {
	// 密钥轮换逻辑
	// 在实际实现中，这会生成新的密钥并通知对端
	return nil
}