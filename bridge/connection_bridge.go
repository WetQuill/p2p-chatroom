package bridge

import (
	"errors"
	"sync"
	"time"

	"github.com/WetQuill/p2p-chatroom/models"
	"github.com/WetQuill/p2p-chatroom/pkg/ipv6"
	"github.com/gorilla/websocket"
)

// ConnectionBridge 连接桥接器
type ConnectionBridge struct {
	mu sync.RWMutex

	// IPv4连接 (现有系统)
	ipv4Connections map[int]*websocket.Conn // userID -> WebSocket连接

	// IPv6连接 (新系统)
	ipv6Connections map[string]ipv6.Connection // peerID -> IPv6连接

	// 地址列表 (共享状态)
	addressList *models.AddressList

	// 映射表
	peerIDToUserID map[string]int
	userIDToPeerID map[int]string

	// 统计
	stats *BridgeStats
}

// BridgeStats 桥接器统计
type BridgeStats struct {
	IPv4Connections   int `json:"ipv4_connections"`
	IPv6Connections   int `json:"ipv6_connections"`
	TotalConnections  int `json:"total_connections"`
	MessagesForwarded int `json:"messages_forwarded"`
	BytesForwarded    int `json:"bytes_forwarded"`
	MigrationCount    int `json:"migration_count"`
}

// NewConnectionBridge 创建连接桥接器
func NewConnectionBridge(addressList *models.AddressList) *ConnectionBridge {
	return &ConnectionBridge{
		ipv4Connections: make(map[int]*websocket.Conn),
		ipv6Connections: make(map[string]ipv6.Connection),
		addressList:     addressList,
		peerIDToUserID:  make(map[string]int),
		userIDToPeerID:  make(map[int]string),
		stats:           &BridgeStats{},
	}
}

// AddIPv4Connection 添加IPv4连接
func (cb *ConnectionBridge) AddIPv4Connection(userID int, conn *websocket.Conn) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.ipv4Connections[userID] = conn
	cb.updateStats()
}

// AddIPv6Connection 添加IPv6连接
func (cb *ConnectionBridge) AddIPv6Connection(peerID string, conn ipv6.Connection) error {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	// 检查是否已存在
	if existing, exists := cb.ipv6Connections[peerID]; exists {
		// 关闭旧连接
		existing.Close()
	}

	cb.ipv6Connections[peerID] = conn
	cb.updateStats()

	return nil
}

// RemoveConnection 移除连接
func (cb *ConnectionBridge) RemoveConnection(identifier interface{}) error {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch id := identifier.(type) {
	case int:
		// IPv4连接 (userID)
		if conn, exists := cb.ipv4Connections[id]; exists {
			conn.Close()
			delete(cb.ipv4Connections, id)

			// 清理映射
			if peerID, exists := cb.userIDToPeerID[id]; exists {
				delete(cb.peerIDToUserID, peerID)
				delete(cb.userIDToPeerID, id)
			}
		}
	case string:
		// IPv6连接 (peerID)
		if conn, exists := cb.ipv6Connections[id]; exists {
			conn.Close()
			delete(cb.ipv6Connections, id)

			// 清理映射
			if userID, exists := cb.peerIDToUserID[id]; exists {
				delete(cb.userIDToPeerID, userID)
				delete(cb.peerIDToUserID, id)
			}
		}
	default:
		return errors.New("invalid identifier type")
	}

	cb.updateStats()
	return nil
}

// Broadcast 广播消息到所有连接
func (cb *ConnectionBridge) Broadcast(message *models.Message) error {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	// 广播到IPv4连接
	for _, conn := range cb.ipv4Connections {
		if err := cb.sendWebSocketMessage(conn, message); err != nil {
			// 记录错误，但继续发送给其他连接
			continue
		}
		cb.stats.MessagesForwarded++
	}

	// 广播到IPv6连接
	for _, conn := range cb.ipv6Connections {
		if err := cb.sendIPv6Message(conn, message); err != nil {
			// 记录错误，但继续发送给其他连接
			continue
		}
		cb.stats.MessagesForwarded++
	}

	return nil
}

// SendToUser 发送消息到特定用户
func (cb *ConnectionBridge) SendToUser(userID int, message *models.Message) error {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	// 尝试IPv4连接
	if conn, exists := cb.ipv4Connections[userID]; exists {
		return cb.sendWebSocketMessage(conn, message)
	}

	// 尝试IPv6连接
	if peerID, exists := cb.userIDToPeerID[userID]; exists {
		if conn, exists := cb.ipv6Connections[peerID]; exists {
			return cb.sendIPv6Message(conn, message)
		}
	}

	return errors.New("user not connected")
}

// SendToPeer 发送消息到特定对等节点
func (cb *ConnectionBridge) SendToPeer(peerID string, message *models.Message) error {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	// 尝试IPv6连接
	if conn, exists := cb.ipv6Connections[peerID]; exists {
		return cb.sendIPv6Message(conn, message)
	}

	// 尝试IPv4连接
	if userID, exists := cb.peerIDToUserID[peerID]; exists {
		if conn, exists := cb.ipv4Connections[userID]; exists {
			return cb.sendWebSocketMessage(conn, message)
		}
	}

	return errors.New("peer not connected")
}

// GetConnectionStats 获取连接统计
func (cb *ConnectionBridge) GetConnectionStats() ConnectionStats {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	stats := ConnectionStats{
		TotalConnections: len(cb.ipv4Connections) + len(cb.ipv6Connections),
		IPv4Connections:  len(cb.ipv4Connections),
		IPv6Connections:  len(cb.ipv6Connections),
		IPv4Percentage:   0,
		IPv6Percentage:   0,
	}

	if stats.TotalConnections > 0 {
		stats.IPv4Percentage = float64(stats.IPv4Connections) / float64(stats.TotalConnections) * 100
		stats.IPv6Percentage = float64(stats.IPv6Connections) / float64(stats.TotalConnections) * 100
	}

	return stats
}

// MigrateToIPv6 迁移到IPv6连接
func (cb *ConnectionBridge) MigrateToIPv6(peerID string) error {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	// 查找对应的userID
	userID, exists := cb.peerIDToUserID[peerID]
	if !exists {
		return errors.New("peer ID not mapped to any user")
	}

	// 检查是否已有IPv6连接
	if _, exists := cb.ipv6Connections[peerID]; exists {
		return nil // 已经迁移
	}

	// 在实际实现中，这里会：
	// 1. 建立新的IPv6连接
	// 2. 迁移状态
	// 3. 关闭旧的IPv4连接（可选）
	// 4. 更新映射

	cb.stats.MigrationCount++
	return nil
}

// RegisterMapping 注册映射
func (cb *ConnectionBridge) RegisterMapping(userID int, peerID string) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.peerIDToUserID[peerID] = userID
	cb.userIDToPeerID[userID] = peerID
}

// GetPeerIDForUser 获取用户的PeerID
func (cb *ConnectionBridge) GetPeerIDForUser(userID int) (string, bool) {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	peerID, exists := cb.userIDToPeerID[userID]
	return peerID, exists
}

// GetUserForPeerID 获取PeerID对应的用户
func (cb *ConnectionBridge) GetUserForPeerID(peerID string) (int, bool) {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	userID, exists := cb.peerIDToUserID[peerID]
	return userID, exists
}

// GetBridgeStats 获取桥接器统计
func (cb *ConnectionBridge) GetBridgeStats() BridgeStats {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	stats := *cb.stats
	stats.IPv4Connections = len(cb.ipv4Connections)
	stats.IPv6Connections = len(cb.ipv6Connections)
	stats.TotalConnections = stats.IPv4Connections + stats.IPv6Connections
	return stats
}

// CleanupStaleConnections 清理过期连接
func (cb *ConnectionBridge) CleanupStaleConnections(timeout time.Duration) int {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	now := time.Now()
	staleCount := 0

	// 清理IPv4连接
	for userID, conn := range cb.ipv4Connections {
		// 简化实现：检查连接是否已关闭
		// 在实际实现中，需要更好的健康检查
		if err := conn.WriteControl(websocket.PingMessage, []byte{}, now.Add(10*time.Second)); err != nil {
			conn.Close()
			delete(cb.ipv4Connections, userID)
			staleCount++
		}
	}

	// 清理IPv6连接
	for peerID, conn := range cb.ipv6Connections {
		// 检查IPv6连接是否活跃
		if !conn.IsActive() {
			conn.Close()
			delete(cb.ipv6Connections, peerID)
			staleCount++
		}
	}

	cb.updateStats()
	return staleCount
}

// 私有方法

func (cb *ConnectionBridge) sendWebSocketMessage(conn *websocket.Conn, message *models.Message) error {
	// 使用现有的发送逻辑
	// 在实际实现中，需要调用main.go中的sendMessage函数
	return nil
}

func (cb *ConnectionBridge) sendIPv6Message(conn ipv6.Connection, message *models.Message) error {
	// 将消息序列化并发送到IPv6连接
	// 在实际实现中，需要序列化消息并处理加密
	data := []byte("placeholder") // 占位符，实际需要序列化
	_, err := conn.Write(data)
	if err == nil {
		cb.stats.BytesForwarded += len(data)
	}
	return err
}

func (cb *ConnectionBridge) updateStats() {
	cb.stats.IPv4Connections = len(cb.ipv4Connections)
	cb.stats.IPv6Connections = len(cb.ipv6Connections)
	cb.stats.TotalConnections = cb.stats.IPv4Connections + cb.stats.IPv6Connections
}

// ConnectionStats 连接统计
type ConnectionStats struct {
	TotalConnections int     `json:"total_connections"`
	IPv4Connections  int     `json:"ipv4_connections"`
	IPv6Connections  int     `json:"ipv6_connections"`
	IPv4Percentage   float64 `json:"ipv4_percentage"`
	IPv6Percentage   float64 `json:"ipv6_percentage"`
}

// BridgeEvent 桥接器事件
type BridgeEvent struct {
	Type    BridgeEventType `json:"type"`
	Message string          `json:"message"`
	Data    interface{}     `json:"data,omitempty"`
	Time    time.Time       `json:"time"`
}

// BridgeEventType 桥接器事件类型
type BridgeEventType string

const (
	BridgeEventConnectionAdded    BridgeEventType = "connection_added"
	BridgeEventConnectionRemoved  BridgeEventType = "connection_removed"
	BridgeEventMigrationStarted   BridgeEventType = "migration_started"
	BridgeEventMigrationCompleted BridgeEventType = "migration_completed"
	BridgeEventStatsUpdated       BridgeEventType = "stats_updated"
)

// BridgeManager 桥接管理器
type BridgeManager struct {
	bridge    *ConnectionBridge
	eventChan chan BridgeEvent
	stopChan  chan struct{}
}

// NewBridgeManager 创建桥接管理器
func NewBridgeManager(addressList *models.AddressList) *BridgeManager {
	return &BridgeManager{
		bridge:    NewConnectionBridge(addressList),
		eventChan: make(chan BridgeEvent, 100),
		stopChan:  make(chan struct{}),
	}
}

// Start 启动桥接管理器
func (bm *BridgeManager) Start() {
	go bm.monitoringLoop()
	go bm.cleanupLoop()
}

// Stop 停止桥接管理器
func (bm *BridgeManager) Stop() {
	close(bm.stopChan)
}

// GetBridge 获取桥接器
func (bm *BridgeManager) GetBridge() *ConnectionBridge {
	return bm.bridge
}

// GetEvents 获取事件通道
func (bm *BridgeManager) GetEvents() <-chan BridgeEvent {
	return bm.eventChan
}

// 私有方法

func (bm *BridgeManager) monitoringLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			bm.checkConnections()
		case <-bm.stopChan:
			return
		}
	}
}

func (bm *BridgeManager) cleanupLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			staleCount := bm.bridge.CleanupStaleConnections(10 * time.Minute)
			if staleCount > 0 {
				bm.sendEvent(BridgeEvent{
					Type:    BridgeEventStatsUpdated,
					Message: "Cleaned up stale connections",
					Data:    map[string]int{"stale_count": staleCount},
					Time:    time.Now(),
				})
			}
		case <-bm.stopChan:
			return
		}
	}
}

func (bm *BridgeManager) checkConnections() {
	stats := bm.bridge.GetConnectionStats()
	bm.sendEvent(BridgeEvent{
		Type:    BridgeEventStatsUpdated,
		Message: "Connection stats updated",
		Data:    stats,
		Time:    time.Now(),
	})
}

func (bm *BridgeManager) sendEvent(event BridgeEvent) {
	select {
	case bm.eventChan <- event:
		// 成功发送
	default:
		// 通道已满
	}
}