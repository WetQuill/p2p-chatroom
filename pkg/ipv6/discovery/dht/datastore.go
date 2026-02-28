package dht

import (
	"errors"
	"sync"
	"time"
)

// DataStore 数据存储实现
type DataStore struct {
	mu    sync.RWMutex
	store map[string]*dataEntry
}

type dataEntry struct {
	value      []byte
	timestamp  time.Time
	expiration time.Time
}

// NewDataStore 创建数据存储
func NewDataStore() *DataStore {
	return &DataStore{
		store: make(map[string]*dataEntry),
	}
}

// Put 存储键值对
func (ds *DataStore) Put(key, value []byte) error {
	if len(key) == 0 {
		return errors.New("key cannot be empty")
	}

	keyStr := string(key)

	ds.mu.Lock()
	defer ds.mu.Unlock()

	ds.store[keyStr] = &dataEntry{
		value:      value,
		timestamp:  time.Now(),
		expiration: time.Now().Add(24 * time.Hour), // 默认24小时过期
	}

	return nil
}

// PutWithExpiration 存储键值对并设置过期时间
func (ds *DataStore) PutWithExpiration(key, value []byte, expiration time.Duration) error {
	if len(key) == 0 {
		return errors.New("key cannot be empty")
	}

	keyStr := string(key)

	ds.mu.Lock()
	defer ds.mu.Unlock()

	ds.store[keyStr] = &dataEntry{
		value:      value,
		timestamp:  time.Now(),
		expiration: time.Now().Add(expiration),
	}

	return nil
}

// Get 获取值
func (ds *DataStore) Get(key []byte) ([]byte, error) {
	if len(key) == 0 {
		return nil, errors.New("key cannot be empty")
	}

	keyStr := string(key)

	ds.mu.RLock()
	entry, exists := ds.store[keyStr]
	ds.mu.RUnlock()

	if !exists {
		return nil, errors.New("key not found")
	}

	// 检查是否过期
	if time.Now().After(entry.expiration) {
		ds.Delete(key)
		return nil, errors.New("key expired")
	}

	// 返回值的副本
	valueCopy := make([]byte, len(entry.value))
	copy(valueCopy, entry.value)

	return valueCopy, nil
}

// Delete 删除键值对
func (ds *DataStore) Delete(key []byte) error {
	if len(key) == 0 {
		return errors.New("key cannot be empty")
	}

	keyStr := string(key)

	ds.mu.Lock()
	defer ds.mu.Unlock()

	delete(ds.store, keyStr)
	return nil
}

// Keys 获取所有键
func (ds *DataStore) Keys() [][]byte {
	ds.mu.RLock()
	defer ds.mu.RUnlock()

	keys := make([][]byte, 0, len(ds.store))
	for key := range ds.store {
		keys = append(keys, []byte(key))
	}
	return keys
}

// GetStats 获取统计信息
func (ds *DataStore) GetStats() DataStoreStats {
	ds.mu.RLock()
	defer ds.mu.RUnlock()

	stats := DataStoreStats{
		TotalEntries:   len(ds.store),
		ExpiredEntries: 0,
	}

	now := time.Now()
	for _, entry := range ds.store {
		if now.After(entry.expiration) {
			stats.ExpiredEntries++
		}
	}

	return stats
}

// CleanupExpired 清理过期条目
func (ds *DataStore) CleanupExpired() int {
	ds.mu.Lock()
	defer ds.mu.Unlock()

	now := time.Now()
	expiredCount := 0

	for key, entry := range ds.store {
		if now.After(entry.expiration) {
			delete(ds.store, key)
			expiredCount++
		}
	}

	return expiredCount
}

// DataStoreStats 数据存储统计
type DataStoreStats struct {
	TotalEntries   int `json:"total_entries"`
	ExpiredEntries int `json:"expired_entries"`
	ValidEntries   int `json:"valid_entries"`
}

// GetStats 计算完整统计
func (dss DataStoreStats) GetStats() DataStoreStats {
	dss.ValidEntries = dss.TotalEntries - dss.ExpiredEntries
	return dss
}

// NetworkImpl 网络实现示例
type NetworkImpl struct {
	// 这里应该包含实际的网络连接
	// 简化实现，只提供接口
}

func (ni *NetworkImpl) SendPing(contact *Contact) error {
	// 实现PING消息发送
	return nil
}

func (ni *NetworkImpl) SendFindNode(contact *Contact, target NodeID) ([]*Contact, error) {
	// 实现FIND_NODE消息发送
	return nil, nil
}

func (ni *NetworkImpl) SendFindValue(contact *Contact, key []byte) ([]byte, []*Contact, error) {
	// 实现FIND_VALUE消息发送
	return nil, nil, nil
}

func (ni *NetworkImpl) SendStore(contact *Contact, key, value []byte) error {
	// 实现STORE消息发送
	return nil
}

// Message 消息结构
type Message struct {
	Type      MessageType   `json:"type"`
	Sender    NodeID        `json:"sender"`
	Receiver  NodeID        `json:"receiver,omitempty"`
	Data      []byte        `json:"data,omitempty"`
	Contacts  []*Contact    `json:"contacts,omitempty"`
	Timestamp time.Time     `json:"timestamp"`
}

// MessageType 消息类型
type MessageType string

const (
	MessageTypePING       MessageType = "ping"
	MessageTypePONG       MessageType = "pong"
	MessageTypeFIND_NODE  MessageType = "find_node"
	MessageTypeFIND_VALUE MessageType = "find_value"
	MessageTypeSTORE      MessageType = "store"
	MessageTypeERROR      MessageType = "error"
)

// CreatePingMessage 创建PING消息
func CreatePingMessage(sender NodeID) *Message {
	return &Message{
		Type:      MessageTypePING,
		Sender:    sender,
		Timestamp: time.Now(),
	}
}

// CreateFindNodeMessage 创建FIND_NODE消息
func CreateFindNodeMessage(sender NodeID, target NodeID) *Message {
	return &Message{
		Type:      MessageTypeFIND_NODE,
		Sender:    sender,
		Data:      target[:],
		Timestamp: time.Now(),
	}
}

// CreateFindValueMessage 创建FIND_VALUE消息
func CreateFindValueMessage(sender NodeID, key []byte) *Message {
	return &Message{
		Type:      MessageTypeFIND_VALUE,
		Sender:    sender,
		Data:      key,
		Timestamp: time.Now(),
	}
}

// CreateStoreMessage 创建STORE消息
func CreateStoreMessage(sender NodeID, key, value []byte) *Message {
	data := make([]byte, len(key)+len(value))
	copy(data, key)
	copy(data[len(key):], value)

	return &Message{
		Type:      MessageTypeSTORE,
		Sender:    sender,
		Data:      data,
		Timestamp: time.Now(),
	}
}

// CreateErrorMessage 创建ERROR消息
func CreateErrorMessage(sender NodeID, errMsg string) *Message {
	return &Message{
		Type:      MessageTypeERROR,
		Sender:    sender,
		Data:      []byte(errMsg),
		Timestamp: time.Now(),
	}
}

// ContactManager 联系人管理器
type ContactManager struct {
	dht      *Kademlia
	interval time.Duration
	stopChan chan struct{}
}

// NewContactManager 创建联系人管理器
func NewContactManager(dht *Kademlia) *ContactManager {
	return &ContactManager{
		dht:      dht,
		interval: 5 * time.Minute, // 每5分钟刷新一次
		stopChan: make(chan struct{}),
	}
}

// Start 启动联系人管理
func (cm *ContactManager) Start() {
	go cm.manageLoop()
}

// Stop 停止联系人管理
func (cm *ContactManager) Stop() {
	close(cm.stopChan)
}

func (cm *ContactManager) manageLoop() {
	ticker := time.NewTicker(cm.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			cm.refreshContacts()
		case <-cm.stopChan:
			return
		}
	}
}

func (cm *ContactManager) refreshContacts() {
	// 随机选择一个桶进行刷新
	// 简化实现：刷新所有联系人的状态
	contacts := cm.dht.routingTable.GetContacts()
	for _, contact := range contacts {
		// 检查是否需要刷新
		if time.Since(contact.LastSeen) > 15*time.Minute {
			go cm.dht.Ping(contact)
		}
	}
}