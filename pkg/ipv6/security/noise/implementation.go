package noise

import (
	"crypto"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"time"

	"golang.org/x/crypto/blake2s"
	"golang.org/x/crypto/chacha20poly1305"
	"golang.org/x/crypto/curve25519"
)

// Curve25519Key Curve25519密钥对
type Curve25519Key struct {
	privateKey [32]byte
	publicKey  [32]byte
}

// NewCurve25519Key 创建新的Curve25519密钥对
func NewCurve25519Key() (*Curve25519Key, error) {
	key := &Curve25519Key{}

	// 生成随机私钥
	if _, err := io.ReadFull(rand.Reader, key.privateKey[:]); err != nil {
		return nil, err
	}

	// 确保私钥有效（Curve25519特定约束）
	key.privateKey[0] &= 248
	key.privateKey[31] &= 127
	key.privateKey[31] |= 64

	// 计算公钥
	curve25519.ScalarBaseMult(&key.publicKey, &key.privateKey)

	return key, nil
}

// GenerateKeypair 生成密钥对（实现DHKey接口）
func (ck *Curve25519Key) GenerateKeypair() error {
	newKey, err := NewCurve25519Key()
	if err != nil {
		return err
	}
	*ck = *newKey
	return nil
}

// DH 执行迪菲-赫尔曼密钥交换
func (ck *Curve25519Key) DH(pubkey []byte) ([]byte, error) {
	if len(pubkey) != 32 {
		return nil, errors.New("invalid public key length")
	}

	var remotePubKey [32]byte
	copy(remotePubKey[:], pubkey)

	var sharedSecret [32]byte
	curve25519.ScalarMult(&sharedSecret, &ck.privateKey, &remotePubKey)

	// 检查是否为零点（无效的共享秘密）
	isZero := true
	for _, b := range sharedSecret {
		if b != 0 {
			isZero = false
			break
		}
	}
	if isZero {
		return nil, errors.New("invalid shared secret")
	}

	return sharedSecret[:], nil
}

// GetPublicKey 获取公钥
func (ck *Curve25519Key) GetPublicKey() []byte {
	pubKeyCopy := make([]byte, 32)
	copy(pubKeyCopy, ck.publicKey[:])
	return pubKeyCopy
}

// GetPrivateKey 获取私钥
func (ck *Curve25519Key) GetPrivateKey() []byte {
	privKeyCopy := make([]byte, 32)
	copy(privKeyCopy, ck.privateKey[:])
	return privKeyCopy
}

// LoadFromPrivateKey 从私钥加载
func LoadFromPrivateKey(privateKey []byte) (*Curve25519Key, error) {
	if len(privateKey) != 32 {
		return nil, errors.New("invalid private key length")
	}

	key := &Curve25519Key{}
	copy(key.privateKey[:], privateKey)

	// 计算公钥
	curve25519.ScalarBaseMult(&key.publicKey, &key.privateKey)

	return key, nil
}

// ChaCha20Poly1305Cipher ChaCha20-Poly1305加密实现
func ChaCha20Poly1305Cipher(key [32]byte) (cipher.AEAD, error) {
	return chacha20poly1305.New(key[:])
}

// Blake2sHash BLAKE2s哈希实现
func Blake2sHash(data []byte) [32]byte {
	hash, _ := blake2s.New256(nil)
	hash.Write(data)
	var result [32]byte
	hash.Sum(result[:0])
	return result
}

// DefaultCipherSuite 默认密码套件
func DefaultCipherSuite() CipherSuite {
	return CipherSuite{
		DH: func() DHKey {
			key, _ := NewCurve25519Key()
			return key
		},
		Cipher: ChaCha20Poly1305Cipher,
		Hash:   Blake2sHash,
	}
}

// NoiseProtocolImpl Noise协议实现
type NoiseProtocolImpl struct {
	config       *NoiseConfig
	cipherSuite  CipherSuite
	localStatic  *Curve25519Key
	localEphemeral *Curve25519Key
	sessions     map[uint32]*NoiseSession
	nextSessionID uint32
}

// NewNoiseProtocol 创建Noise协议实例
func NewNoiseProtocol(config *NoiseConfig) (*NoiseProtocolImpl, error) {
	if config == nil {
		config = DefaultNoiseConfig()
	}

	// 加载或生成静态密钥
	var localStatic *Curve25519Key
	if len(config.StaticKey) == 32 {
		var err error
		localStatic, err = LoadFromPrivateKey(config.StaticKey)
		if err != nil {
			return nil, err
		}
	} else {
		localStatic, _ = NewCurve25519Key()
	}

	return &NoiseProtocolImpl{
		config:       config,
		cipherSuite:  DefaultCipherSuite(),
		localStatic:  localStatic,
		sessions:     make(map[uint32]*NoiseSession),
		nextSessionID: 1,
	}, nil
}

// PerformHandshake 执行握手
func (np *NoiseProtocolImpl) PerformHandshake(conn io.ReadWriter) (*NoiseSession, error) {
	// 生成临时密钥
	localEphemeral, err := NewCurve25519Key()
	if err != nil {
		return nil, err
	}
	np.localEphemeral = localEphemeral

	// 创建握手状态
	handshakeState := np.createHandshakeState()

	// 根据模式执行握手
	session, err := np.performXXHandshake(conn, handshakeState)
	if err != nil {
		return nil, err
	}

	// 存储会话
	np.sessions[session.SessionID] = session

	return session, nil
}

// EncryptMessage 加密消息
func (np *NoiseProtocolImpl) EncryptMessage(session *NoiseSession, plaintext []byte) ([]byte, error) {
	if !session.HandshakeDone {
		return nil, errors.New("handshake not completed")
	}

	// 创建消息
	msg := &NoiseMessage{
		Type:      MessageTypeData,
		SessionID: session.SessionID,
		Nonce:     session.SendState.n,
		Payload:   plaintext,
	}

	// 加密负载
	ciphertext, err := session.SendState.EncryptWithAd(nil, msg.Payload)
	if err != nil {
		return nil, err
	}
	msg.Payload = ciphertext

	// 序列化消息
	return np.serializeMessage(msg)
}

// DecryptMessage 解密消息
func (np *NoiseProtocolImpl) DecryptMessage(session *NoiseSession, ciphertext []byte) ([]byte, error) {
	if !session.HandshakeDone {
		return nil, errors.New("handshake not completed")
	}

	// 反序列化消息
	msg, err := np.deserializeMessage(ciphertext)
	if err != nil {
		return nil, err
	}

	// 检查会话ID
	if msg.SessionID != session.SessionID {
		return nil, errors.New("invalid session ID")
	}

	// 检查nonce
	if msg.Nonce != session.RecvState.n {
		return nil, fmt.Errorf("nonce mismatch: expected %d, got %d", session.RecvState.n, msg.Nonce)
	}

	// 解密负载
	plaintext, err := session.RecvState.DecryptWithAd(nil, msg.Payload)
	if err != nil {
		return nil, err
	}

	// 更新最后活动时间
	session.LastActivity = time.Now().Unix()

	return plaintext, nil
}

// CloseSession 关闭会话
func (np *NoiseProtocolImpl) CloseSession(session *NoiseSession) error {
	delete(np.sessions, session.SessionID)
	return nil
}

// GetSession 获取会话
func (np *NoiseProtocolImpl) GetSession(sessionID uint32) *NoiseSession {
	return np.sessions[sessionID]
}

// GetAllSessions 获取所有会话
func (np *NoiseProtocolImpl) GetAllSessions() []*NoiseSession {
	sessions := make([]*NoiseSession, 0, len(np.sessions))
	for _, session := range np.sessions {
		sessions = append(sessions, session)
	}
	return sessions
}

// CleanupStaleSessions 清理过期会话
func (np *NoiseProtocolImpl) CleanupStaleSessions(maxAge time.Duration) int {
	now := time.Now().Unix()
	staleCount := 0

	for sessionID, session := range np.sessions {
		if time.Duration(now-session.LastActivity)*time.Second > maxAge {
			delete(np.sessions, sessionID)
			staleCount++
		}
	}

	return staleCount
}

// 私有方法

func (np *NoiseProtocolImpl) createHandshakeState() *HandshakeState {
	// 创建远程公钥数组
	var rs, re [32]byte
	if len(np.config.RemoteStaticKey) == 32 {
		copy(rs[:], np.config.RemoteStaticKey)
	}

	return NewHandshakeState(
		np.config.Pattern,
		np.config.Initiator,
		np.config.Prologue,
		np.localStatic,
		np.localEphemeral,
		rs,
		re,
	)
}

func (np *NoiseProtocolImpl) performXXHandshake(conn io.ReadWriter, hs *HandshakeState) (*NoiseSession, error) {
	var session *NoiseSession

	if np.config.Initiator {
		// 发起方握手流程
		session = np.performXXInitiatorHandshake(conn, hs)
	} else {
		// 响应方握手流程
		session = np.performXXResponderHandshake(conn, hs)
	}

	if session != nil {
		session.HandshakeDone = true
		session.CreatedAt = time.Now().Unix()
		session.LastActivity = time.Now().Unix()
	}

	return session, nil
}

func (np *NoiseProtocolImpl) performXXInitiatorHandshake(conn io.ReadWriter, hs *HandshakeState) *NoiseSession {
	// XX模式发起方握手：
	// 1. 发送e
	// 2. 接收e, ee
	// 3. 发送s, se
	// 4. 接收s, se

	// 简化实现 - 实际需要完整的Noise协议实现
	sessionID := np.generateSessionID()

	return &NoiseSession{
		SessionID:    sessionID,
		SendState:    NewCipherState([32]byte{}),
		RecvState:    NewCipherState([32]byte{}),
		RemotePeerID: "",
		HandshakeDone: false,
	}
}

func (np *NoiseProtocolImpl) performXXResponderHandshake(conn io.ReadWriter, hs *HandshakeState) *NoiseSession {
	// XX模式响应方握手：
	// 1. 接收e
	// 2. 发送e, ee
	// 3. 接收s, se
	// 4. 发送s, se

	// 简化实现
	sessionID := np.generateSessionID()

	return &NoiseSession{
		SessionID:    sessionID,
		SendState:    NewCipherState([32]byte{}),
		RecvState:    NewCipherState([32]byte{}),
		RemotePeerID: "",
		HandshakeDone: false,
	}
}

func (np *NoiseProtocolImpl) serializeMessage(msg *NoiseMessage) ([]byte, error) {
	// 简单序列化：类型(1) + 会话ID(4) + nonce(8) + 负载长度(2) + 负载
	buffer := make([]byte, 1+4+8+2+len(msg.Payload))
	buffer[0] = byte(msg.Type)

	// 会话ID (大端字节序)
	buffer[1] = byte(msg.SessionID >> 24)
	buffer[2] = byte(msg.SessionID >> 16)
	buffer[3] = byte(msg.SessionID >> 8)
	buffer[4] = byte(msg.SessionID)

	// Nonce (大端字节序)
	for i := 0; i < 8; i++ {
		buffer[5+i] = byte(msg.Nonce >> uint(8*(7-i)))
	}

	// 负载长度 (大端字节序)
	payloadLen := uint16(len(msg.Payload))
	buffer[13] = byte(payloadLen >> 8)
	buffer[14] = byte(payloadLen)

	// 负载
	copy(buffer[15:], msg.Payload)

	return buffer, nil
}

func (np *NoiseProtocolImpl) deserializeMessage(data []byte) (*NoiseMessage, error) {
	if len(data) < 15 {
		return nil, errors.New("message too short")
	}

	msg := &NoiseMessage{
		Type: MessageType(data[0]),
	}

	// 会话ID
	msg.SessionID = uint32(data[1])<<24 | uint32(data[2])<<16 | uint32(data[3])<<8 | uint32(data[4])

	// Nonce
	msg.Nonce = 0
	for i := 0; i < 8; i++ {
		msg.Nonce = msg.Nonce<<8 | uint64(data[5+i])
	}

	// 负载长度
	payloadLen := uint16(data[13])<<8 | uint16(data[14])

	// 检查长度
	if len(data) < 15+int(payloadLen) {
		return nil, errors.New("invalid payload length")
	}

	// 负载
	msg.Payload = make([]byte, payloadLen)
	copy(msg.Payload, data[15:15+payloadLen])

	return msg, nil
}

func (np *NoiseProtocolImpl) generateSessionID() uint32 {
	id := np.nextSessionID
	np.nextSessionID++
	return id
}

// NoiseHandshakeError 握手错误
type NoiseHandshakeError struct {
	Stage     string
	Detail    string
	RemoteID  string
}

func (e *NoiseHandshakeError) Error() string {
	return fmt.Sprintf("noise handshake error at stage %s: %s (remote: %s)", e.Stage, e.Detail, e.RemoteID)
}

// NoiseStats Noise协议统计
type NoiseStats struct {
	HandshakesAttempted  int `json:"handshakes_attempted"`
	HandshakesCompleted  int `json:"handshakes_completed"`
	HandshakesFailed     int `json:"handshakes_failed"`
	MessagesEncrypted    int `json:"messages_encrypted"`
	MessagesDecrypted    int `json:"messages_decrypted"`
	ActiveSessions       int `json:"active_sessions"`
	BytesEncrypted       int `json:"bytes_encrypted"`
	BytesDecrypted       int `json:"bytes_decrypted"`
}

// NoiseManager Noise协议管理器
type NoiseManager struct {
	protocol    *NoiseProtocolImpl
	stats       *NoiseStats
	sessionMap  map[string]uint32 // PeerID -> SessionID 映射
}

// NewNoiseManager 创建Noise管理器
func NewNoiseManager(config *NoiseConfig) (*NoiseManager, error) {
	protocol, err := NewNoiseProtocol(config)
	if err != nil {
		return nil, err
	}

	return &NoiseManager{
		protocol:   protocol,
		stats:      &NoiseStats{},
		sessionMap: make(map[string]uint32),
	}, nil
}

// GetStaticPublicKey 获取静态公钥
func (nm *NoiseManager) GetStaticPublicKey() []byte {
	return nm.protocol.localStatic.GetPublicKey()
}

// GetStats 获取统计信息
func (nm *NoiseManager) GetStats() *NoiseStats {
	stats := *nm.stats
	stats.ActiveSessions = len(nm.protocol.sessions)
	return &stats
}