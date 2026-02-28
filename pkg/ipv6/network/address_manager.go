package network

import (
	"net"
	"sync"
	"time"
)

// AddressManager IPv6地址管理器
type AddressManager struct {
	mu                 sync.RWMutex
	addresses          map[string]*AddressEntry
	currentAddr        net.IP
	changeChan         chan AddressChangeEvent
	monitorStopChan    chan struct{}
	onChangeCallbacks []func(AddressChangeEvent)

	// 配置
	enablePrivacyExtensions bool
	preferredPrefixes      []string
	addressLifetime       time.Duration
	monitoringInterval   time.Duration
}

// AddressEntry IPv6地址条目
type AddressEntry struct {
	IP               net.IP    `json:"ip"`
	Netmask          net.IPMask `json:"netmask"`
	Interface        string    `json:"interface"`
	InterfaceIndex   int       `json:"interface_index"`
	Scope            int       `json:"scope"` // 地址作用域
	IsGlobal         bool      `json:"is_global"`
	IsDeprecated     bool      `json:"is_deprecated"`
	IsTemporary      bool      `json:"is_temporary"` // 隐私扩展地址
	ValidLifetime    time.Duration `json:"valid_lifetime"`
	PreferredLifetime time.Duration `json:"preferred_lifetime"`
	LastUpdated      time.Time   `json:"last_updated"`
}

// AddressChangeEvent 地址变更事件
type AddressChangeEvent struct {
	Type      ChangeType     `json:"type"`
	OldAddr   net.IP         `json:"old_addr,omitempty"`
	NewAddr   net.IP         `json:"new_addr,omitempty"`
	Addresses []*AddressEntry `json:"addresses"`
	Timestamp time.Time      `json:"timestamp"`
}

// ChangeType 变更类型枚举
type ChangeType string

const (
	ChangeAdded    ChangeType = "added"
	ChangeRemoved  ChangeType = "removed"
	ChangeModified ChangeType = "modified"
	ChangeSwitched ChangeType = "switched"
)

// NewAddressManager 创建新的地址管理器
func NewAddressManager() *AddressManager {
	return &AddressManager{
		addresses:          make(map[string]*AddressEntry),
		changeChan:         make(chan AddressChangeEvent, 10),
		monitorStopChan:    make(chan struct{}),
		onChangeCallbacks:  make([]func(AddressChangeEvent), 0),
		preferredPrefixes:  []string{"2001:", "2002:", "2400:", "2401:", "2402:"},
		addressLifetime:    time.Hour,
		monitoringInterval: time.Minute,
	}
}

// StartMonitoring 开始监控地址变化
func (am *AddressManager) StartMonitoring() error {
	// 初始扫描
	am.scanAddresses()

	// 启动监控协程
	go am.monitorLoop()

	return nil
}

// StopMonitoring 停止监控
func (am *AddressManager) StopMonitoring() {
	if am.monitorStopChan != nil {
		close(am.monitorStopChan)
		am.monitorStopChan = nil
	}
}

// GetBestAddress 获取最佳IPv6地址
func (am *AddressManager) GetBestAddress() (net.IP, error) {
	am.mu.RLock()
	defer am.mu.RUnlock()

	if am.currentAddr != nil {
		return am.currentAddr, nil
	}

	// 选择策略：
	// 1. 非临时地址优先
	// 2. 特定前缀优先
	// 3. 最新地址优先

	for _, entry := range am.addresses {
		if entry.IsGlobal && !entry.IsDeprecated {
			// 优先选择非临时地址
			if !entry.IsTemporary && am.isPreferredPrefix(entry.IP) {
				return entry.IP, nil
			}
		}
	}

	// 如果没有合适的地址，选择第一个全局地址
	for _, entry := range am.addresses {
		if entry.IsGlobal && !entry.IsDeprecated {
			return entry.IP, nil
		}
	}

	return nil, net.UnknownNetworkError("no suitable IPv6 address found")
}

// GetAllAddresses 获取所有IPv6地址
func (am *AddressManager) GetAllAddresses() []AddressEntry {
	am.mu.RLock()
	defer am.mu.RUnlock()

	entries := make([]AddressEntry, 0, len(am.addresses))
	for _, entry := range am.addresses {
		entries = append(entries, *entry)
	}

	return entries
}

// ValidateAddress 验证IPv6地址有效性
func (am *AddressManager) ValidateAddress(ip net.IP) bool {
	if ip == nil || ip.To4() != nil {
		return false
	}

	// 检查是否是全局单播地址
	if !ip.IsGlobalUnicast() {
		return false
	}

	// 检查是否是链路本地地址
	if ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return false
	}

	// 检查是否是组播地址
	if ip.IsMulticast() {
		return false
	}

	// 检查是否是未指定地址
	if ip.IsUnspecified() {
		return false
	}

	return true
}

// OnAddressChange 注册地址变更回调
func (am *AddressManager) OnAddressChange(callback func(AddressChangeEvent)) {
	am.mu.Lock()
	defer am.mu.Unlock()

	am.onChangeCallbacks = append(am.onChangeCallbacks, callback)
}

// GetChangeChannel 获取地址变更通道
func (am *AddressManager) GetChangeChannel() <-chan AddressChangeEvent {
	return am.changeChan
}

// 私有方法

func (am *AddressManager) monitorLoop() {
	ticker := time.NewTicker(am.monitoringInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			am.scanAddresses()
		case <-am.monitorStopChan:
			return
		}
	}
}

func (am *AddressManager) scanAddresses() {
	am.mu.Lock()
	defer am.mu.Unlock()

	oldAddresses := am.addresses
	newAddresses := make(map[string]*AddressEntry)

	ifaces, err := net.Interfaces()
	if err != nil {
		return
	}

	for _, iface := range ifaces {
		// 跳过未启用的接口
		if iface.Flags&net.FlagUp == 0 {
			continue
		}

		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}

		for _, addr := range addrs {
			ipNet, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}

			ip := ipNet.IP
			if ip.To4() != nil {
				continue // 跳过IPv4地址
			}

			// 验证地址有效性
			if !am.ValidateAddress(ip) {
				continue
			}

			entry := &AddressEntry{
				IP:             ip,
				Netmask:        ipNet.Mask,
				Interface:      iface.Name,
				InterfaceIndex: iface.Index,
				IsGlobal:       ip.IsGlobalUnicast(),
				IsTemporary:    am.isTemporaryAddress(ip),
				LastUpdated:    time.Now(),
			}

			key := ip.String()
			newAddresses[key] = entry

			// 检查地址是否变更
			if oldEntry, exists := oldAddresses[key]; exists {
				if oldEntry.Interface != entry.Interface ||
					oldEntry.IsDeprecated != entry.IsDeprecated ||
					oldEntry.IsTemporary != entry.IsTemporary {
					am.notifyChange(AddressChangeEvent{
						Type:      ChangeModified,
						OldAddr:   oldEntry.IP,
						NewAddr:   entry.IP,
						Addresses: am.getAddressList(newAddresses),
						Timestamp: time.Now(),
					})
				}
			} else {
				// 新地址
				am.notifyChange(AddressChangeEvent{
					Type:      ChangeAdded,
					NewAddr:   entry.IP,
					Addresses: am.getAddressList(newAddresses),
					Timestamp: time.Now(),
				})
			}
		}
	}

	// 检查被删除的地址
	for key, oldEntry := range oldAddresses {
		if _, exists := newAddresses[key]; !exists {
			am.notifyChange(AddressChangeEvent{
				Type:      ChangeRemoved,
				OldAddr:   oldEntry.IP,
				Addresses: am.getAddressList(newAddresses),
				Timestamp: time.Now(),
			})
		}
	}

	am.addresses = newAddresses

	// 检查是否需要切换当前地址
	am.updateCurrentAddress()
}

func (am *AddressManager) updateCurrentAddress() {
	bestAddr, err := am.GetBestAddress()
	if err != nil {
		return
	}

	if am.currentAddr == nil || !bestAddr.Equal(am.currentAddr) {
		oldAddr := am.currentAddr
		am.currentAddr = bestAddr

		if oldAddr != nil {
			am.notifyChange(AddressChangeEvent{
				Type:      ChangeSwitched,
				OldAddr:   oldAddr,
				NewAddr:   bestAddr,
				Addresses: am.getAddressList(am.addresses),
				Timestamp: time.Now(),
			})
		}
	}
}

func (am *AddressManager) isPreferredPrefix(ip net.IP) bool {
	ipStr := ip.String()
	for _, prefix := range am.preferredPrefixes {
		if len(ipStr) >= len(prefix) && ipStr[:len(prefix)] == prefix {
			return true
		}
	}
	return false
}

func (am *AddressManager) isTemporaryAddress(ip net.IP) bool {
	// 简化的临时地址检测
	// 实际实现可能需要检查系统特定标志或地址生成方式
	ipStr := ip.String()
	// 隐私扩展地址通常包含特定模式
	return len(ipStr) > 8 && ipStr[8:] != "::"
}

func (am *AddressManager) getAddressList(addressMap map[string]*AddressEntry) []*AddressEntry {
	list := make([]*AddressEntry, 0, len(addressMap))
	for _, entry := range addressMap {
		list = append(list, entry)
	}
	return list
}

func (am *AddressManager) notifyChange(event AddressChangeEvent) {
	// 发送到通道
	select {
	case am.changeChan <- event:
	default:
		// 通道已满，丢弃事件
	}

	// 调用回调函数
	for _, callback := range am.onChangeCallbacks {
		go callback(event)
	}
}