package identity

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/WetQuill/p2p-chatroom/pkg/ipv6"
)

// IdentityManager 身份管理器
type IdentityManager struct {
	mu          sync.RWMutex
	privateKey  ed25519.PrivateKey
	publicKey   ed25519.PublicKey
	peerID      ipv6.PeerID
	keyStore    KeyStore
	keyRotation KeyRotationPolicy
	identities  map[string]*Identity
}

// Identity 身份信息
type Identity struct {
	PeerID      ipv6.PeerID    `json:"peer_id"`
	PublicKey   []byte         `json:"public_key"`
	PrivateKey  []byte         `json:"private_key,omitempty"`
	Name        string         `json:"name,omitempty"`
	CreatedAt   time.Time      `json:"created_at"`
	LastUsed    time.Time      `json:"last_used"`
	IsCurrent   bool           `json:"is_current"`
}

// KeyStore 密钥存储接口
type KeyStore interface {
	Save(identity *Identity) error
	Load(peerID ipv6.PeerID) (*Identity, error)
	List() ([]ipv6.PeerID, error)
	Delete(peerID ipv6.PeerID) error
}

// KeyRotationPolicy 密钥轮换策略
type KeyRotationPolicy struct {
	Enabled        bool          `json:"enabled"`
	RotationPeriod time.Duration `json:"rotation_period"`
	MaxIdentities  int           `json:"max_identities"`
	AutoGenerate   bool          `json:"auto_generate"`
}

// FileKeyStore 文件密钥存储实现
type FileKeyStore struct {
	baseDir string
}

// NewIdentityManager 创建身份管理器
func NewIdentityManager() *IdentityManager {
	return &IdentityManager{
		identities: make(map[string]*Identity),
		keyRotation: KeyRotationPolicy{
			Enabled:        true,
			RotationPeriod: 24 * time.Hour,
			MaxIdentities:  10,
			AutoGenerate:   true,
		},
	}
}

// Generate 生成新身份
func (im *IdentityManager) Generate(name string) (*Identity, error) {
	im.mu.Lock()
	defer im.mu.Unlock()

	// 生成密钥对
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}

	// 计算PeerID
	peerID := CalculatePeerID(publicKey)

	// 创建身份
	identity := &Identity{
		PeerID:     peerID,
		PublicKey:  publicKey,
		PrivateKey: privateKey,
		Name:       name,
		CreatedAt:  time.Now(),
		LastUsed:   time.Now(),
		IsCurrent:  true,
	}

	// 存储身份
	im.identities[string(peerID)] = identity

	// 设置为当前密钥
	im.privateKey = privateKey
	im.publicKey = publicKey
	im.peerID = peerID

	// 保存到存储
	if im.keyStore != nil {
		if err := im.keyStore.Save(identity); err != nil {
			return nil, err
		}
	}

	return identity, nil
}

// Load 从存储加载身份
func (im *IdentityManager) Load(peerID ipv6.PeerID) error {
	im.mu.Lock()
	defer im.mu.Unlock()

	if im.keyStore == nil {
		return errors.New("key store not configured")
	}

	identity, err := im.keyStore.Load(peerID)
	if err != nil {
		return err
	}

	im.identities[string(peerID)] = identity
	im.privateKey = identity.PrivateKey
	im.publicKey = identity.PublicKey
	im.peerID = identity.PeerID

	return nil
}

// LoadFromFile 从文件加载身份
func (im *IdentityManager) LoadFromFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	// 这里需要实现身份文件的解析
	// 简化实现：假设文件包含base64编码的私钥
	privateKey, err := base64.StdEncoding.DecodeString(string(data))
	if err != nil {
		return err
	}

	if len(privateKey) != ed25519.PrivateKeySize {
		return errors.New("invalid private key size")
	}

	// 从私钥派生公钥和PeerID
	im.privateKey = ed25519.PrivateKey(privateKey)
	im.publicKey = im.privateKey.Public().(ed25519.PublicKey)
	im.peerID = CalculatePeerID(im.publicKey)

	// 创建身份
	identity := &Identity{
		PeerID:     im.peerID,
		PublicKey:  im.publicKey,
		PrivateKey: im.privateKey,
		CreatedAt:  time.Now(),
		LastUsed:   time.Now(),
		IsCurrent:  true,
	}

	im.identities[string(im.peerID)] = identity

	return nil
}

// SaveToFile 保存身份到文件
func (im *IdentityManager) SaveToFile(path string) error {
	if len(im.privateKey) == 0 {
		return errors.New("no private key available")
	}

	// 确保目录存在
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}

	// 将私钥编码为base64
	data := base64.StdEncoding.EncodeToString(im.privateKey)
	return os.WriteFile(path, []byte(data), 0600)
}

// Sign 签名数据
func (im *IdentityManager) Sign(data []byte) ([]byte, error) {
	im.mu.RLock()
	defer im.mu.RUnlock()

	if len(im.privateKey) == 0 {
		return nil, errors.New("no private key available")
	}

	return ed25519.Sign(im.privateKey, data), nil
}

// Verify 验证签名
func (im *IdentityManager) Verify(publicKey []byte, data []byte, signature []byte) bool {
	if len(publicKey) != ed25519.PublicKeySize {
		return false
	}

	if len(signature) != ed25519.SignatureSize {
		return false
	}

	return ed25519.Verify(ed25519.PublicKey(publicKey), data, signature)
}

// VerifyWithPeerID 使用PeerID验证签名
func (im *IdentityManager) VerifyWithPeerID(peerID ipv6.PeerID, data []byte, signature []byte) bool {
	im.mu.RLock()
	defer im.mu.RUnlock()

	// 查找身份
	identity, exists := im.identities[string(peerID)]
	if !exists {
		// 尝试从存储加载
		if im.keyStore != nil {
			identity, err := im.keyStore.Load(peerID)
			if err == nil {
				im.identities[string(peerID)] = identity
				exists = true
			}
		}
	}

	if !exists {
		return false
	}

	return im.Verify(identity.PublicKey, data, signature)
}

// GetPeerID 获取当前PeerID
func (im *IdentityManager) GetPeerID() ipv6.PeerID {
	im.mu.RLock()
	defer im.mu.RUnlock()

	return im.peerID
}

// GetPublicKey 获取当前公钥
func (im *IdentityManager) GetPublicKey() []byte {
	im.mu.RLock()
	defer im.mu.RUnlock()

	publicKeyCopy := make([]byte, len(im.publicKey))
	copy(publicKeyCopy, im.publicKey)
	return publicKeyCopy
}

// GetCurrentIdentity 获取当前身份
func (im *IdentityManager) GetCurrentIdentity() *Identity {
	im.mu.RLock()
	defer im.mu.RUnlock()

	if identity, exists := im.identities[string(im.peerID)]; exists {
		return &Identity{
			PeerID:     identity.PeerID,
			PublicKey:  identity.PublicKey,
			Name:       identity.Name,
			CreatedAt:  identity.CreatedAt,
			LastUsed:   identity.LastUsed,
			IsCurrent:  true,
		}
	}
	return nil
}

// ListIdentities 列出所有身份
func (im *IdentityManager) ListIdentities() []*Identity {
	im.mu.RLock()
	defer im.mu.RUnlock()

	identities := make([]*Identity, 0, len(im.identities))
	for _, identity := range im.identities {
		// 返回副本，不包含私钥
		identities = append(identities, &Identity{
			PeerID:    identity.PeerID,
			PublicKey: identity.PublicKey,
			Name:      identity.Name,
			CreatedAt: identity.CreatedAt,
			LastUsed:  identity.LastUsed,
			IsCurrent: identity.PeerID == im.peerID,
		})
	}
	return identities
}

// SetKeyStore 设置密钥存储
func (im *IdentityManager) SetKeyStore(store KeyStore) {
	im.mu.Lock()
	defer im.mu.Unlock()

	im.keyStore = store
}

// SetKeyRotationPolicy 设置密钥轮换策略
func (im *IdentityManager) SetKeyRotationPolicy(policy KeyRotationPolicy) {
	im.mu.Lock()
	defer im.mu.Unlock()

	im.keyRotation = policy
}

// RotateKey 轮换密钥
func (im *IdentityManager) RotateKey() error {
	if !im.keyRotation.Enabled {
		return nil
	}

	// 生成新身份
	newIdentity, err := im.Generate("rotated-" + time.Now().Format("20060102-150405"))
	if err != nil {
		return err
	}

	// 更新当前身份为非当前
	if currentIdentity, exists := im.identities[string(im.peerID)]; exists {
		currentIdentity.IsCurrent = false
	}

	// 清理旧身份（如果超过最大数量）
	im.cleanupOldIdentities()

	return nil
}

// cleanupOldIdentities 清理旧身份
func (im *IdentityManager) cleanupOldIdentities() {
	if len(im.identities) <= im.keyRotation.MaxIdentities {
		return
	}

	// 按最后使用时间排序
	identities := im.ListIdentities()
	if len(identities) <= im.keyRotation.MaxIdentities {
		return
	}

	// 找到最旧的非当前身份
	var oldestIdentity *Identity
	for _, identity := range identities {
		if identity.IsCurrent {
			continue
		}
		if oldestIdentity == nil || identity.LastUsed.Before(oldestIdentity.LastUsed) {
			oldestIdentity = identity
		}
	}

	if oldestIdentity != nil {
		delete(im.identities, string(oldestIdentity.PeerID))
		if im.keyStore != nil {
			im.keyStore.Delete(oldestIdentity.PeerID)
		}
	}
}

// CalculatePeerID 计算PeerID（基于公钥）
func CalculatePeerID(publicKey []byte) ipv6.PeerID {
	if len(publicKey) != ed25519.PublicKeySize {
		return ""
	}

	// 使用SHA-256哈希公钥，然后base64编码
	// 简化实现：使用hex编码
	return ipv6.PeerID(hex.EncodeToString(publicKey[:16])) // 取前16字节
}

// NewFileKeyStore 创建文件密钥存储
func NewFileKeyStore(baseDir string) *FileKeyStore {
	return &FileKeyStore{
		baseDir: baseDir,
	}
}

func (fks *FileKeyStore) Save(identity *Identity) error {
	// 确保目录存在
	if err := os.MkdirAll(fks.baseDir, 0700); err != nil {
		return err
	}

	// 创建身份文件路径
	filename := filepath.Join(fks.baseDir, string(identity.PeerID)+".json")

	// 这里应该实现JSON序列化
	// 简化实现：只保存私钥
	data := base64.StdEncoding.EncodeToString(identity.PrivateKey)
	return os.WriteFile(filename, []byte(data), 0600)
}

func (fks *FileKeyStore) Load(peerID ipv6.PeerID) (*Identity, error) {
	filename := filepath.Join(fks.baseDir, string(peerID)+".json")
	return nil, errors.New("not implemented")
}

func (fks *FileKeyStore) List() ([]ipv6.PeerID, error) {
	return nil, errors.New("not implemented")
}

func (fks *FileKeyStore) Delete(peerID ipv6.PeerID) error {
	filename := filepath.Join(fks.baseDir, string(peerID)+".json")
	return os.Remove(filename)
}