package discovery

import (
	"context"
	"sync"
	"time"

	"github.com/WetQuill/p2p-chatroom/pkg/ipv6"
	"github.com/WetQuill/p2p-chatroom/pkg/ipv6/discovery/dht"
	"github.com/WetQuill/p2p-chatroom/pkg/ipv6/discovery/mdns"
	"github.com/WetQuill/p2p-chatroom/pkg/ipv6/identity"
)

// DiscoveryManager 发现管理器
type DiscoveryManager struct {
	mu         sync.RWMutex
	dht        *dht.Kademlia
	mdns       *mdns.mDNSManager
	identity   *identity.IdentityManager
	config     *Config
	discovered chan ipv6.PeerInfo
	events     chan ipv6.Event
	stopChan   chan struct{}
	running    bool

	// 缓存和状态
	peerCache  map[string]*ipv6.PeerInfo
	lastSync   time.Time
	stats      *DiscoveryStats
}

// Config 发现配置
type Config struct {
	Enabled          bool     `json:"enabled"`
	DHTEnabled       bool     `json:"dht_enabled"`
	MDNSEnabled      bool     `json:"mdns_enabled"`
	BootstrapNodes   []string `json:"bootstrap_nodes"`
	ListenAddress    string   `json:"listen_address"`
	AnnounceInterval int      `json:"announce_interval"` // 秒
	SyncInterval     int      `json:"sync_interval"`     // 秒
	CacheTTL         int      `json:"cache_ttl"`         // 秒
}

// DiscoveryStats 发现统计
type DiscoveryStats struct {
	DHTPeers        int `json:"dht_peers"`
	MDNSPeers       int `json:"mdns_peers"`
	TotalDiscovered int `json:"total_discovered"`
	CacheSize       int `json:"cache_size"`
	Lookups         int `json:"lookups"`
	Failures        int `json:"failures"`
}

// NewDiscoveryManager 创建发现管理器
func NewDiscoveryManager(config *Config, identityMgr *identity.IdentityManager) *DiscoveryManager {
	return &DiscoveryManager{
		identity:   identityMgr,
		config:     config,
		discovered: make(chan ipv6.PeerInfo, 100),
		events:     make(chan ipv6.Event, 100),
		stopChan:   make(chan struct{}),
		peerCache:  make(map[string]*ipv6.PeerInfo),
		stats:      &DiscoveryStats{},
	}
}

// Start 启动发现管理器
func (dm *DiscoveryManager) Start() error {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	if dm.running {
		return nil
	}

	// 初始化DHT
	if dm.config.DHTEnabled {
		if err := dm.initDHT(); err != nil {
			return err
		}
	}

	// 初始化mDNS
	if dm.config.MDNSEnabled {
		if err := dm.initMDNS(); err != nil {
			return err
		}
	}

	// 启动发现协程
	go dm.discoveryLoop()

	// 启动同步协程
	go dm.syncLoop()

	// 启动清理协程
	go dm.cleanupLoop()

	dm.running = true
	return nil
}

// Stop 停止发现管理器
func (dm *DiscoveryManager) Stop() error {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	if !dm.running {
		return nil
	}

	close(dm.stopChan)
	dm.running = false

	return nil
}

// Discover 获取发现通道
func (dm *DiscoveryManager) Discover() <-chan ipv6.PeerInfo {
	return dm.discovered
}

// GetEvents 获取事件通道
func (dm *DiscoveryManager) GetEvents() <-chan ipv6.Event {
	return dm.events
}

// FindPeer 查找特定节点
func (dm *DiscoveryManager) FindPeer(peerID string) (*ipv6.PeerInfo, error) {
	// 先检查缓存
	if peer, exists := dm.peerCache[peerID]; exists {
		if time.Since(peer.LastSeen) < time.Duration(dm.config.CacheTTL)*time.Second {
			return peer, nil
		}
		delete(dm.peerCache, peerID)
	}

	dm.stats.Lookups++

	// 尝试通过DHT查找
	if dm.dht != nil {
		// 将peerID转换为NodeID（简化实现）
		// 在实际实现中需要正确的转换逻辑
		nodeID := dht.GenerateNodeID() // 这里应该是从peerID派生

		contacts, err := dm.dht.FindNode(nodeID)
		if err == nil && len(contacts) > 0 {
			peer := dm.contactToPeerInfo(contacts[0])
			dm.cachePeer(peer)
			return peer, nil
		}
		dm.stats.Failures++
	}

	// 尝试通过mDNS查找
	if dm.mdns != nil {
		service := dm.mdns.GetServiceByPeerID(peerID)
		if service != nil {
			peer := dm.serviceEntryToPeerInfo(service)
			dm.cachePeer(peer)
			return peer, nil
		}
	}

	return nil, NewDiscoveryError("peer not found", peerID)
}

// Announce 公告自身节点
func (dm *DiscoveryManager) Announce(port int, ipv6Addr string) error {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	// 通过DHT公告
	if dm.dht != nil {
		// 构建公告数据
		announceData := map[string]interface{}{
			"peer_id":   dm.identity.GetPeerID(),
			"address":   ipv6Addr,
			"port":      port,
			"timestamp": time.Now().Unix(),
		}

		// 简化实现：在实际实现中需要序列化和存储
	}

	// 通过mDNS公告
	if dm.mdns != nil {
		serviceInfo := mdns.CreateServiceInfo(
			"p2p-chatroom-"+string(dm.identity.GetPeerID()[:8]),
			port,
			string(dm.identity.GetPeerID()),
			nil, // IP地址在实际实现中设置
		)

		if err := dm.mdns.Announce(serviceInfo); err != nil {
			return err
		}
	}

	dm.sendEvent(ipv6.Event{
		Type:    ipv6.EventStarted,
		Source:  "discovery",
		Message: "Node announced successfully",
		Time:    time.Now(),
	})

	return nil
}

// GetStats 获取统计信息
func (dm *DiscoveryManager) GetStats() *DiscoveryStats {
	dm.mu.RLock()
	defer dm.mu.RUnlock()

	stats := *dm.stats
	stats.CacheSize = len(dm.peerCache)
	stats.TotalDiscovered = stats.DHTPeers + stats.MDNSPeers
	return &stats
}

// GetPeers 获取所有发现的节点
func (dm *DiscoveryManager) GetPeers() []*ipv6.PeerInfo {
	dm.mu.RLock()
	defer dm.mu.RUnlock()

	peers := make([]*ipv6.PeerInfo, 0, len(dm.peerCache))
	for _, peer := range dm.peerCache {
		peers = append(peers, peer)
	}
	return peers
}

// 私有方法

func (dm *DiscoveryManager) initDHT() error {
	// 创建DHT实例
	// 这里需要实现实际的网络层和数据存储
	// 简化实现
	return nil
}

func (dm *DiscoveryManager) initMDNS() error {
	mdnsConfig := mdns.DefaultmDNSConfig()
	mdnsConfig.Enabled = true

	dm.mdns = mdns.NewmDNSManager(mdnsConfig)

	// 设置事件处理器
	go dm.handleMDNSEvents()

	return dm.mdns.Start()
}

func (dm *DiscoveryManager) discoveryLoop() {
	ticker := time.NewTicker(time.Duration(dm.config.AnnounceInterval) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			dm.performDiscovery()
		case <-dm.stopChan:
			return
		}
	}
}

func (dm *DiscoveryManager) syncLoop() {
	ticker := time.NewTicker(time.Duration(dm.config.SyncInterval) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			dm.syncPeers()
		case <-dm.stopChan:
			return
		}
	}
}

func (dm *DiscoveryManager) cleanupLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			dm.cleanupCache()
		case <-dm.stopChan:
			return
		}
	}
}

func (dm *DiscoveryManager) performDiscovery() {
	// 执行发现操作
	// 1. 从DHT获取新节点
	// 2. 从mDNS获取新节点
	// 3. 合并结果并发送到通道

	if dm.dht != nil {
		dm.discoverFromDHT()
	}

	if dm.mdns != nil {
		dm.discoverFromMDNS()
	}
}

func (dm *DiscoveryManager) discoverFromDHT() {
	// 简化实现
	// 在实际实现中，这会执行DHT查找
}

func (dm *DiscoveryManager) discoverFromMDNS() {
	if dm.mdns == nil {
		return
	}

	peers := dm.mdns.GetDiscoveredPeers()
	for _, peer := range peers {
		dm.handleDiscoveredPeer(&peer)
	}
}

func (dm *DiscoveryManager) syncPeers() {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	dm.lastSync = time.Now()

	// 同步DHT和mDNS的状态
	// 在实际实现中，这会交换节点信息
}

func (dm *DiscoveryManager) cleanupCache() {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	now := time.Now()
	for peerID, peer := range dm.peerCache {
		if now.Sub(peer.LastSeen) > time.Duration(dm.config.CacheTTL)*time.Second {
			delete(dm.peerCache, peerID)
		}
	}
}

func (dm *DiscoveryManager) handleMDNSEvents() {
	if dm.mdns == nil {
		return
	}

	for event := range dm.mdns.GetEvents() {
		dm.events <- event
	}
}

func (dm *DiscoveryManager) handleDiscoveredPeer(peer *ipv6.PeerInfo) {
	// 检查是否已存在
	if existing, exists := dm.peerCache[peer.ID]; exists {
		// 更新现有节点
		existing.Address = peer.Address
		existing.LastSeen = time.Now()
		existing.PublicKey = peer.PublicKey
		return
	}

	// 缓存新节点
	peer.LastSeen = time.Now()
	dm.cachePeer(peer)

	// 发送到发现通道
	select {
	case dm.discovered <- *peer:
		// 成功发送
	default:
		// 通道已满
	}

	// 发送事件
	dm.sendEvent(ipv6.Event{
		Type:    ipv6.EventPeerDiscovered,
		Source:  "discovery",
		Message: "Peer discovered: " + peer.ID,
		Data:    peer,
		Time:    time.Now(),
	})
}

func (dm *DiscoveryManager) cachePeer(peer *ipv6.PeerInfo) {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	dm.peerCache[peer.ID] = peer

	// 更新统计
	if peer.Connection != nil && peer.Connection.Protocol == "dht" {
		dm.stats.DHTPeers++
	} else {
		dm.stats.MDNSPeers++
	}
}

func (dm *DiscoveryManager) contactToPeerInfo(contact *dht.Contact) *ipv6.PeerInfo {
	return &ipv6.PeerInfo{
		ID:       string(contact.ID[:]), // 简化转换
		Address:  contact.Addr.String(),
		LastSeen: contact.LastSeen,
	}
}

func (dm *DiscoveryManager) serviceEntryToPeerInfo(entry *mdns.ServiceEntry) *ipv6.PeerInfo {
	address := ""
	if entry.AddrIPv6 != nil {
		address = entry.AddrIPv6.String()
	} else if entry.AddrIPv4 != nil {
		address = entry.AddrIPv4.String()
	}

	return &ipv6.PeerInfo{
		ID:       entry.PeerID,
		Address:  address,
		LastSeen: entry.Discovered,
	}
}

func (dm *DiscoveryManager) sendEvent(event ipv6.Event) {
	select {
	case dm.events <- event:
		// 成功发送
	default:
		// 通道已满
	}
}

// DiscoveryError 发现错误
type DiscoveryError struct {
	Message string
	PeerID  string
}

func NewDiscoveryError(message, peerID string) *DiscoveryError {
	return &DiscoveryError{
		Message: message,
		PeerID:  peerID,
	}
}

func (de *DiscoveryError) Error() string {
	return de.Message + ": " + de.PeerID
}

// DefaultConfig 默认发现配置
func DefaultConfig() *Config {
	return &Config{
		Enabled:          true,
		DHTEnabled:       true,
		MDNSEnabled:      true,
		BootstrapNodes: []string{
			"bootstrap1.p2p.example.com:9000",
			"bootstrap2.p2p.example.com:9000",
		},
		ListenAddress:    "[::]:0",
		AnnounceInterval: 300, // 5分钟
		SyncInterval:     60,  // 1分钟
		CacheTTL:         1800, // 30分钟
	}
}

// ResolveWithContext 使用上下文解析节点
func (dm *DiscoveryManager) ResolveWithContext(ctx context.Context, peerID string) (*ipv6.PeerInfo, error) {
	// 带上下文的查找，支持超时
	// 简化实现：调用普通查找
	return dm.FindPeer(peerID)
}

// SetBootstrapNodes 设置引导节点
func (dm *DiscoveryManager) SetBootstrapNodes(nodes []string) {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	dm.config.BootstrapNodes = nodes
}

// IsRunning 检查是否正在运行
func (dm *DiscoveryManager) IsRunning() bool {
	dm.mu.RLock()
	defer dm.mu.RUnlock()

	return dm.running
}