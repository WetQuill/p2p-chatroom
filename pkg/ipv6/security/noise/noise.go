package noise

import (
	"crypto/cipher"
	"errors"
	"io"
)

// CipherState 密码状态
type CipherState struct {
	k [32]byte       // 对称密钥
	n uint64         // 非ce
	cipher           cipher.AEAD
}

// SymmetricState 对称状态
type SymmetricState struct {
	CipherState
	ck [32]byte // 链密钥
	h  [32]byte // 握手哈希
}

// HandshakeState 握手状态
type HandshakeState struct {
	s         *CipherState // 静态密钥对
	e         *CipherState // 临时密钥对
	rs        [32]byte     // 远程静态公钥
	re        [32]byte     // 远程临时公钥
	initiator bool         // 是否是发起方
	pattern   HandshakePattern
	prologue  []byte       // 握手前导数据
	messageBuf []byte      // 消息缓冲区
}

// HandshakePattern 握手模式
type HandshakePattern string

const (
	// 基本模式
	PatternXX HandshakePattern = "XX" // 相互认证，前向安全
	PatternIK HandshakePattern = "IK" // 已知公钥，0-RTT
	PatternKK HandshakePattern = "KK" // 预共享密钥
)

// DHKey 迪菲-赫尔曼密钥接口
type DHKey interface {
	GenerateKeypair() error
	DH(pubkey []byte) ([]byte, error)
	GetPublicKey() []byte
	GetPrivateKey() []byte
}

// CipherSuite 密码套件
type CipherSuite struct {
	DH     func() DHKey     // 迪菲-赫尔曼函数
	Cipher func(key [32]byte) (cipher.AEAD, error) // 对称加密
	Hash   func(data []byte) [32]byte // 哈希函数
}

// NewCipherState 创建密码状态
func NewCipherState(k [32]byte) *CipherState {
	return &CipherState{
		k: k,
		n: 0,
	}
}

// EncryptWithAd 使用附加数据加密
func (cs *CipherState) EncryptWithAd(ad, plaintext []byte) ([]byte, error) {
	if cs.cipher == nil {
		// 如果未设置密码，直接返回明文
		return plaintext, nil
	}

	// 检查nonce溢出
	if cs.n == ^uint64(0) {
		return nil, errors.New("nonce overflow")
	}

	// 转换nonce为12字节
	nonce := make([]byte, 12)
	for i := 0; i < 8; i++ {
		nonce[4+i] = byte(cs.n >> uint(8*(7-i)))
	}

	// 加密
	ciphertext := cs.cipher.Seal(nil, nonce, plaintext, ad)

	// 递增nonce
	cs.n++

	return ciphertext, nil
}

// DecryptWithAd 使用附加数据解密
func (cs *CipherState) DecryptWithAd(ad, ciphertext []byte) ([]byte, error) {
	if cs.cipher == nil {
		// 如果未设置密码，直接返回密文
		return ciphertext, nil
	}

	// 检查nonce溢出
	if cs.n == ^uint64(0) {
		return nil, errors.New("nonce overflow")
	}

	// 转换nonce为12字节
	nonce := make([]byte, 12)
	for i := 0; i < 8; i++ {
		nonce[4+i] = byte(cs.n >> uint(8*(7-i)))
	}

	// 解密
	plaintext, err := cs.cipher.Open(nil, nonce, ciphertext, ad)
	if err != nil {
		return nil, err
	}

	// 递增nonce
	cs.n++

	return plaintext, nil
}

// InitializeSymmetric 初始化对称状态
func (ss *SymmetricState) InitializeSymmetric(protocolName []byte) {
	// 如果协议名称长度不超过32字节，h = protocolName
	// 否则 h = Hash(protocolName)
	if len(protocolName) <= 32 {
		copy(ss.h[:], protocolName)
		for i := len(protocolName); i < 32; i++ {
			ss.h[i] = 0
		}
	} else {
		hash := ss.Hash(protocolName)
		copy(ss.h[:], hash[:])
	}

	// ck = h
	copy(ss.ck[:], ss.h[:])

	// 重置密钥
	ss.k = [32]byte{}
	ss.n = 0
	ss.cipher = nil
}

// MixKey 混合密钥
func (ss *SymmetricState) MixKey(inputKeyMaterial []byte) {
	// hmac = HMACHash(ck, inputKeyMaterial)
	// ck = hmac的前32字节
	// tempK = hmac的后32字节（如果AEAD密钥是32字节）

	// 简化实现
	hash := ss.Hash(append(ss.ck[:], inputKeyMaterial...))
	copy(ss.ck[:], hash[:16])
	copy(ss.k[:], hash[16:])
}

// MixHash 混合哈希
func (ss *SymmetricState) MixHash(data []byte) {
	// h = Hash(h || data)
	combined := append(ss.h[:], data...)
	hash := ss.Hash(combined)
	copy(ss.h[:], hash[:])
}

// EncryptAndHash 加密并哈希
func (ss *SymmetricState) EncryptAndHash(plaintext []byte) ([]byte, error) {
	ciphertext, err := ss.EncryptWithAd(ss.h[:], plaintext)
	if err != nil {
		return nil, err
	}
	ss.MixHash(ciphertext)
	return ciphertext, nil
}

// DecryptAndHash 解密并哈希
func (ss *SymmetricState) DecryptAndHash(ciphertext []byte) ([]byte, error) {
	plaintext, err := ss.DecryptWithAd(ss.h[:], ciphertext)
	if err != nil {
		return nil, err
	}
	ss.MixHash(ciphertext)
	return plaintext, nil
}

// Split 分裂为发送和接收状态
func (ss *SymmetricState) Split() (*CipherState, *CipherState, error) {
	// k1, k2 = HKDF(ck, zerolen, 2)
	// 返回两个CipherState

	// 简化实现
	sendKey := [32]byte{}
	recvKey := [32]byte{}

	// 使用ck生成两个密钥
	hash1 := ss.Hash(append(ss.ck[:], 0x00))
	hash2 := ss.Hash(append(ss.ck[:], 0x01))

	copy(sendKey[:], hash1[:])
	copy(recvKey[:], hash2[:])

	sendState := NewCipherState(sendKey)
	recvState := NewCipherState(recvKey)

	return sendState, recvState, nil
}

// NewHandshakeState 创建握手状态
func NewHandshakeState(pattern HandshakePattern, initiator bool, prologue []byte, s, e DHKey, rs, re [32]byte) *HandshakeState {
	return &HandshakeState{
		initiator: initiator,
		pattern:   pattern,
		prologue:  prologue,
		s:         &CipherState{k: s.GetPublicKey()}, // 简化表示
		e:         &CipherState{k: e.GetPublicKey()},
		rs:        rs,
		re:        re,
	}
}

// WriteMessage 写入握手消息
func (hs *HandshakeState) WriteMessage(payload []byte, messageBuffer []byte) ([]byte, error) {
	// 根据握手模式处理消息
	switch hs.pattern {
	case PatternXX:
		return hs.writeMessageXX(payload)
	default:
		return nil, errors.New("unsupported handshake pattern")
	}
}

// ReadMessage 读取握手消息
func (hs *HandshakeState) ReadMessage(message []byte, payloadBuffer []byte) ([]byte, error) {
	// 根据握手模式处理消息
	switch hs.pattern {
	case PatternXX:
		return hs.readMessageXX(message)
	default:
		return nil, errors.New("unsupported handshake pattern")
	}
}

// XX模式握手实现
func (hs *HandshakeState) writeMessageXX(payload []byte) ([]byte, error) {
	// XX模式握手流程：
	// 1. 发起方发送e
	// 2. 响应方发送e, ee
	// 3. 发起方发送s, se
	// 4. 响应方发送s, se

	// 简化实现
	return payload, nil
}

func (hs *HandshakeState) readMessageXX(message []byte) ([]byte, error) {
	// 简化实现
	return message, nil
}

// NoiseConfig Noise协议配置
type NoiseConfig struct {
	Pattern    HandshakePattern `json:"pattern"`
	Initiator  bool             `json:"initiator"`
	Prologue   []byte           `json:"prologue,omitempty"`
	StaticKey  []byte           `json:"static_key,omitempty"`
	RemoteStaticKey []byte      `json:"remote_static_key,omitempty"`
}

// DefaultNoiseConfig 默认Noise配置
func DefaultNoiseConfig() *NoiseConfig {
	return &NoiseConfig{
		Pattern:   PatternXX,
		Initiator: true,
		Prologue:  []byte("Noise_XX_25519_ChaChaPoly_BLAKE2s"),
	}
}

// MessageType 消息类型
type MessageType byte

const (
	MessageTypeHandshake MessageType = 0x00
	MessageTypeData      MessageType = 0x01
	MessageTypeError     MessageType = 0xFF
)

// NoiseMessage Noise消息格式
type NoiseMessage struct {
	Type      MessageType `json:"type"`
	SessionID uint32      `json:"session_id"`
	Nonce     uint64      `json:"nonce"`
	Payload   []byte      `json:"payload"`
}

// NoiseSession Noise会话
type NoiseSession struct {
	SessionID     uint32
	SendState     *CipherState
	RecvState     *CipherState
	RemotePeerID  string
	HandshakeDone bool
	CreatedAt     int64
	LastActivity  int64
}

// NoiseProtocol Noise协议接口
type NoiseProtocol interface {
	PerformHandshake(conn io.ReadWriter) (*NoiseSession, error)
	EncryptMessage(session *NoiseSession, plaintext []byte) ([]byte, error)
	DecryptMessage(session *NoiseSession, ciphertext []byte) ([]byte, error)
	CloseSession(session *NoiseSession) error
}

// 辅助函数
func (cs *CipherState) Hash(data []byte) [32]byte {
	// 实现具体的哈希函数
	// 这里应该是BLAKE2s或类似
	var hash [32]byte
	// 简化实现
	copy(hash[:], data[:min(32, len(data))])
	return hash
}

func (ss *SymmetricState) Hash(data []byte) [32]byte {
	// 调用CipherState的Hash方法
	return ss.CipherState.Hash(data)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}