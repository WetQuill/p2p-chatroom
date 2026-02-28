package mdns

import (
	"context"
	"errors"
	"net"
	"sync"
	"time"

	"github.com/WetQuill/p2p-chatroom/pkg/ipv6"
)

// mDNSDiscoverer mDNS发现器
type mDNSDiscoverer struct {
	mu          sync.RWMutex
	serviceName string
	serviceType string
	domain      string
	entries     map[string]*ServiceEntry
	discovered  chan *ServiceEntry
	stopChan    chan struct{}
	announcing  bool
	interfaceIP net.IP

	// 回调函数
	onServiceDiscovered func(*ServiceEntry)
	onServiceRemoved   func(*ServiceEntry)
}

// ServiceEntry 服务条目
type ServiceEntry struct {
	Name       string   `json:"name"`
	Type       string   `json:"type"`
	Domain     string   `json:"domain"`
	AddrIPv4   net.IP   `json:"addr_ipv4,omitempty"`
	AddrIPv6   net.IP   `json:"addr_ipv6,omitempty"`
	Port       int      `json:"port"`
	TTL        uint32   `json:"ttl"`
	Text       []string `json:"text"`
	PeerID     string   `json:"peer_id,omitempty"`
	Discovered time.Time `json:"discovered"`
}

// ServiceInfo 服务信息
type ServiceInfo struct {
	Name     string   `json:"name"`
	Type     string   `json:"type"`
	Domain   string   `json:"domain"`
	Port     int      `json:"port"`
	Text     []string `json:"text"`
	IPv4Addr net.IP   `json:"ipv4_addr,omitempty"`
	IPv6Addr net.IP   `json:"ipv6_addr,omitempty"`
	PeerID   string   `json:"peer_id,omitempty"`
}

// NewmDNSDiscoverer 创建mDNS发现器
func NewmDNSDiscoverer(serviceType string) *mDNSDiscoverer {
	return &mDNSDiscoverer{
		serviceType: serviceType,
		serviceName: "p2p-chatroom",
		domain:      "local",
		entries:     make(map[string]*ServiceEntry),
		discovered:  make(chan *ServiceEntry, 100),
		stopChan:    make(chan struct{}),
	}
}

// Start 启动mDNS发现
func (md *mDNSDiscoverer) Start() error {
	md.mu.Lock()
	defer md.mu.Unlock()

	if md.stopChan != nil {
		return errors.New("already started")
	}

	md.stopChan = make(chan struct{})

	// 启动发现协程
	go md.discoverLoop()

	return nil
}

// Stop 停止mDNS发现
func (md *mDNSDiscoverer) Stop() error {
	md.mu.Lock()
	defer md.mu.Unlock()

	if md.stopChan == nil {
		return errors.New("not started")
	}

	close(md.stopChan)
	md.stopChan = nil

	// 停止公告
	md.stopAnnounce()

	return nil
}

// Browse 浏览服务
func (md *mDNSDiscoverer) Browse() <-chan *ServiceEntry {
	return md.discovered
}

// Announce 公告服务
func (md *mDNSDiscoverer) Announce(service *ServiceInfo) error {
	md.mu.Lock()
	defer md.mu.Unlock()

	// 设置服务信息
	md.serviceName = service.Name
	if service.Domain != "" {
		md.domain = service.Domain
	}

	// 获取本地IP地址
	if err := md.detectInterfaceIP(); err != nil {
		return err
	}

	// 启动公告协程
	md.announcing = true
	go md.announceLoop(service)

	return nil
}

// SetOnServiceDiscovered 设置服务发现回调
func (md *mDNSDiscoverer) SetOnServiceDiscovered(callback func(*ServiceEntry)) {
	md.mu.Lock()
	defer md.mu.Unlock()

	md.onServiceDiscovered = callback
}

// SetOnServiceRemoved 设置服务移除回调
func (md *mDNSDiscoverer) SetOnServiceRemoved(callback func(*ServiceEntry)) {
	md.mu.Lock()
	defer md.mu.Unlock()

	md.onServiceRemoved = callback
}

// GetDiscoveredServices 获取已发现的服务
func (md *mDNSDiscoverer) GetDiscoveredServices() []*ServiceEntry {
	md.mu.RLock()
	defer md.mu.RUnlock()

	services := make([]*ServiceEntry, 0, len(md.entries))
	for _, entry := range md.entries {
		services = append(services, entry)
	}
	return services
}

// GetServiceByPeerID 通过PeerID获取服务
func (md *mDNSDiscoverer) GetServiceByPeerID(peerID string) *ServiceEntry {
	md.mu.RLock()
	defer md.mu.RUnlock()

	for _, entry := range md.entries {
		if entry.PeerID == peerID {
			return entry
		}
	}
	return nil
}

// CleanupStaleEntries 清理过期条目
func (md *mDNSDiscoverer) CleanupStaleEntries(maxAge time.Duration) int {
	md.mu.Lock()
	defer md.mu.Unlock()

	now := time.Now()
	removedCount := 0

	for key, entry := range md.entries {
		if now.Sub(entry.Discovered) > maxAge {
			delete(md.entries, key)
			removedCount++

			// 调用移除回调
			if md.onServiceRemoved != nil {
				go md.onServiceRemoved(entry)
			}
		}
	}

	return removedCount
}

// 私有方法

func (md *mDNSDiscoverer) discoverLoop() {
	// 这里应该实现mDNS发现逻辑
	// 简化实现：模拟发现过程

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// 模拟发现新服务
			md.simulateDiscovery()
		case <-md.stopChan:
			return
		}
	}
}

func (md *mDNSDiscoverer) announceLoop(service *ServiceInfo) {
	// 这里应该实现mDNS公告逻辑
	// 简化实现：定期更新公告

	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	for md.announcing {
		select {
		case <-ticker.C:
			// 更新服务公告
			md.updateAnnouncement(service)
		case <-md.stopChan:
			return
		}
	}
}

func (md *mDNSDiscoverer) stopAnnounce() {
	md.announcing = false
}

func (md *mDNSDiscoverer) detectInterfaceIP() error {
	// 检测本地网络接口
	ifaces, err := net.Interfaces()
	if err != nil {
		return err
	}

	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}

		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}

		for _, addr := range addrs {
			ipNet, ok := addr.(*net.IPNet)
			if ok && ipNet.IP.To4() != nil && ipNet.IP.IsGlobalUnicast() {
				md.interfaceIP = ipNet.IP
				return nil
			}
		}
	}

	return errors.New("no suitable network interface found")
}

func (md *mDNSDiscoverer) simulateDiscovery() {
	// 模拟发现一个服务
	entry := &ServiceEntry{
		Name:       md.serviceName + "-" + randomString(4),
		Type:       md.serviceType,
		Domain:     md.domain,
		AddrIPv4:   net.ParseIP("192.168.1.100"),
		AddrIPv6:   net.ParseIP("2409:8a20:441:2371:2431:54af:ece2:7"),
		Port:       1010,
		TTL:        120,
		Text:       []string{"peer_id=test-peer-123", "version=1.0.0"},
		PeerID:     "test-peer-123",
		Discovered: time.Now(),
	}

	// 检查是否已存在
	key := entry.Name + "." + entry.Type + "." + entry.Domain
	md.mu.Lock()
	_, exists := md.entries[key]
	md.mu.Unlock()

	if !exists {
		md.mu.Lock()
		md.entries[key] = entry
		md.mu.Unlock()

		// 发送到发现通道
		select {
		case md.discovered <- entry:
			// 成功发送
		default:
			// 通道已满
		}

		// 调用回调
		if md.onServiceDiscovered != nil {
			go md.onServiceDiscovered(entry)
		}
	}
}

func (md *mDNSDiscoverer) updateAnnouncement(service *ServiceInfo) {
	// 更新服务公告信息
	// 在实际实现中，这会发送mDNS公告包
}

// CreateServiceInfo 创建服务信息
func CreateServiceInfo(name string, port int, peerID string, ipv6Addr net.IP) *ServiceInfo {
	return &ServiceInfo{
		Name:     name,
		Type:     "_p2p-chatroom._tcp",
		Domain:   "local",
		Port:     port,
		Text:     []string{"peer_id=" + peerID, "protocol=ipv6", "transport=udp"},
		IPv6Addr: ipv6Addr,
		PeerID:   peerID,
	}
}

// Discoverer 发现器接口
type Discoverer interface {
	Start() error
	Stop() error
	Browse() <-chan *ServiceEntry
	Announce(service *ServiceInfo) error
	GetDiscoveredServices() []*ServiceEntry
}

// mDNSConfig mDNS配置
type mDNSConfig struct {
	Enabled            bool          `json:"enabled"`
	ServiceType        string        `json:"service_type"`
	Domain             string        `json:"domain"`
	DiscoveryInterval  time.Duration `json:"discovery_interval"`
	AnnounceInterval   time.Duration `json:"announce_interval"`
	EntryTTL           time.Duration `json:"entry_ttl"`
	MaxDiscovered      int           `json:"max_discovered"`
}

// DefaultmDNSConfig 默认mDNS配置
func DefaultmDNSConfig() *mDNSConfig {
	return &mDNSConfig{
		Enabled:           true,
		ServiceType:       "_p2p-chatroom._tcp",
		Domain:            "local",
		DiscoveryInterval: 10 * time.Second,
		AnnounceInterval:  60 * time.Second,
		EntryTTL:          2 * time.Hour,
		MaxDiscovered:     50,
	}
}

// mDNSManager mDNS管理器
type mDNSManager struct {
	discoverer *mDNSDiscoverer
	config     *mDNSConfig
	eventChan  chan ipv6.Event
	stopChan   chan struct{}
}

// NewmDNSManager 创建mDNS管理器
func NewmDNSManager(config *mDNSConfig) *mDNSManager {
	return &mDNSManager{
		discoverer: NewmDNSDiscoverer(config.ServiceType),
		config:     config,
		eventChan:  make(chan ipv6.Event, 100),
		stopChan:   make(chan struct{}),
	}
}

// Start 启动mDNS管理器
func (mm *mDNSManager) Start() error {
	if !mm.config.Enabled {
		return nil
	}

	// 设置回调
	mm.discoverer.SetOnServiceDiscovered(mm.onServiceDiscovered)
	mm.discoverer.SetOnServiceRemoved(mm.onServiceRemoved)

	// 启动发现器
	if err := mm.discoverer.Start(); err != nil {
		return err
	}

	// 启动清理协程
	go mm.cleanupLoop()

	return nil
}

// Stop 停止mDNS管理器
func (mm *mDNSManager) Stop() error {
	close(mm.stopChan)
	return mm.discoverer.Stop()
}

// Announce 公告服务
func (mm *mDNSManager) Announce(service *ServiceInfo) error {
	return mm.discoverer.Announce(service)
}

// GetEvents 获取事件通道
func (mm *mDNSManager) GetEvents() <-chan ipv6.Event {
	return mm.eventChan
}

// GetDiscoveredPeers 获取发现的节点
func (mm *mDNSManager) GetDiscoveredPeers() []ipv6.PeerInfo {
	services := mm.discoverer.GetDiscoveredServices()
	peers := make([]ipv6.PeerInfo, 0, len(services))

	for _, service := range services {
		peer := ipv6.PeerInfo{
			ID:      service.PeerID,
			Address: service.AddrIPv6.String(),
		}
		peers = append(peers, peer)
	}

	return peers
}

// 私有方法

func (mm *mDNSManager) onServiceDiscovered(entry *ServiceEntry) {
	// 创建事件
	event := ipv6.Event{
		Type:    ipv6.EventPeerDiscovered,
		Source:  "mdns",
		Message: "Service discovered: " + entry.Name,
		Data:    entry,
		Time:    time.Now(),
	}

	// 发送事件
	select {
	case mm.eventChan <- event:
		// 成功发送
	default:
		// 通道已满
	}
}

func (mm *mDNSManager) onServiceRemoved(entry *ServiceEntry) {
	// 创建事件
	event := ipv6.Event{
		Type:    ipv6.EventPeerDisconnected,
		Source:  "mdns",
		Message: "Service removed: " + entry.Name,
		Data:    entry,
		Time:    time.Now(),
	}

	// 发送事件
	select {
	case mm.eventChan <- event:
		// 成功发送
	default:
		// 通道已满
	}
}

func (mm *mDNSManager) cleanupLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			mm.discoverer.CleanupStaleEntries(mm.config.EntryTTL)
		case <-mm.stopChan:
			return
		}
	}
}

// 辅助函数
func randomString(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, length)
	for i := range b {
		// 简化实现
		b[i] = charset[i%len(charset)]
	}
	return string(b)
}

// ResolveWithContext 使用上下文解析服务
func (md *mDNSDiscoverer) ResolveWithContext(ctx context.Context, name, serviceType string) (*ServiceEntry, error) {
	// 实现带上下文的解析
	// 简化实现：从已发现的服务中查找
	md.mu.RLock()
	defer md.mu.RUnlock()

	fullName := name + "." + serviceType + "." + md.domain
	if entry, exists := md.entries[fullName]; exists {
		return entry, nil
	}

	return nil, errors.New("service not found")
}

// SetInterface 设置网络接口
func (md *mDNSDiscoverer) SetInterface(iface *net.Interface) error {
	// 设置特定的网络接口进行mDNS通信
	// 简化实现
	return nil
}