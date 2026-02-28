package dht

import (
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"math/big"
	"net"
	"sync"
	"time"
)

// Kademlia Kademlia DHT实现
type Kademlia struct {
	mu          sync.RWMutex
	nodeID      NodeID
	routingTable *RoutingTable
	dataStore   *DataStore
	network     Network
	bootstrapped bool

	// 配置参数
	k              int           // 系统参数，通常为20
	alpha          int           // 并发度，通常为3
	tExpire        time.Duration // 条目过期时间
	tReplicate     time.Duration // 复制间隔
	tRepublish     time.Duration // 重新发布间隔
	bootstrapNodes []string      // 引导节点列表
}

// NodeID 160位节点ID
type NodeID [20]byte

// Contact 联系人信息
type Contact struct {
	ID       NodeID
	Addr     net.Addr
	LastSeen time.Time
	RTT      time.Duration
	Status   ContactStatus
}

// ContactStatus 联系人状态
type ContactStatus string

const (
	ContactStatusGood    ContactStatus = "good"
	ContactStatusQuestionable ContactStatus = "questionable"
	ContactStatusBad     ContactStatus = "bad"
)

// Network 网络接口
type Network interface {
	SendPing(contact *Contact) error
	SendFindNode(contact *Contact, target NodeID) ([]*Contact, error)
	SendFindValue(contact *Contact, key []byte) ([]byte, []*Contact, error)
	SendStore(contact *Contact, key, value []byte) error
}

// DataStore 数据存储接口
type DataStore interface {
	Put(key, value []byte) error
	Get(key []byte) ([]byte, error)
	Delete(key []byte) error
	Keys() [][]byte
}

// NewKademlia 创建Kademlia DHT实例
func NewKademlia(nodeID NodeID, network Network, dataStore DataStore) *Kademlia {
	return &Kademlia{
		nodeID:      nodeID,
		routingTable: NewRoutingTable(nodeID),
		dataStore:   dataStore,
		network:     network,
		k:           20,
		alpha:       3,
		tExpire:     24 * time.Hour,
		tReplicate:  1 * time.Hour,
		tRepublish:  24 * time.Hour,
	}
}

// Join 加入DHT网络
func (k *Kademlia) Join(bootstrapNodes []string) error {
	k.bootstrapNodes = bootstrapNodes

	// 如果没有引导节点，无法加入
	if len(bootstrapNodes) == 0 {
		return errors.New("no bootstrap nodes provided")
	}

	// 连接引导节点
	var initialContacts []*Contact
	for _, addr := range bootstrapNodes {
		contact := k.createContactFromAddress(addr)
		if contact != nil {
			initialContacts = append(initialContacts, contact)
		}
	}

	if len(initialContacts) == 0 {
		return errors.New("failed to contact any bootstrap node")
	}

	// 执行引导过程
	err := k.bootstrap(initialContacts)
	if err != nil {
		return err
	}

	k.bootstrapped = true
	return nil
}

// Store 存储键值对
func (k *Kademlia) Store(key, value []byte) error {
	// 计算最近的k个节点
	contacts := k.routingTable.FindClosest(key, k.k)

	// 并发存储到多个节点
	var wg sync.WaitGroup
	errChan := make(chan error, len(contacts))

	for _, contact := range contacts {
		wg.Add(1)
		go func(c *Contact) {
			defer wg.Done()
			err := k.network.SendStore(c, key, value)
			if err != nil {
				errChan <- err
			}
		}(contact)
	}

	wg.Wait()
	close(errChan)

	// 检查错误
	for err := range errChan {
		if err != nil {
			return err
		}
	}

	// 本地也存储一份
	return k.dataStore.Put(key, value)
}

// FindValue 查找值
func (k *Kademlia) FindValue(key []byte) ([]byte, []*Contact, error) {
	// 先检查本地存储
	value, err := k.dataStore.Get(key)
	if err == nil {
		return value, nil, nil
	}

	// 查找最近的节点
	contacts := k.routingTable.FindClosest(key, k.alpha)
	if len(contacts) == 0 {
		return nil, nil, errors.New("no contacts available")
	}

	// 并行查询
	return k.findValueIterative(key, contacts)
}

// FindNode 查找节点
func (k *Kademlia) FindNode(target NodeID) ([]*Contact, error) {
	// 查找最近的节点
	contacts := k.routingTable.FindClosest(target[:], k.alpha)
	if len(contacts) == 0 {
		return nil, errors.New("no contacts available")
	}

	// 迭代查找
	return k.findNodeIterative(target, contacts)
}

// Ping 测试节点连通性
func (k *Kademlia) Ping(contact *Contact) error {
	err := k.network.SendPing(contact)
	if err == nil {
		k.routingTable.Update(contact)
	}
	return err
}

// UpdateContact 更新联系人
func (k *Kademlia) UpdateContact(contact *Contact) {
	k.routingTable.Update(contact)
}

// GetRoutingTableStats 获取路由表统计
func (k *Kademlia) GetRoutingTableStats() RoutingTableStats {
	return k.routingTable.GetStats()
}

// GetDataStoreStats 获取数据存储统计
func (k *Kademlia) GetDataStoreStats() DataStoreStats {
	return k.dataStore.GetStats()
}

// IsBootstrapped 检查是否已引导
func (k *Kademlia) IsBootstrapped() bool {
	return k.bootstrapped
}

// 私有方法

func (k *Kademlia) bootstrap(initialContacts []*Contact) error {
	// 将初始联系人加入路由表
	for _, contact := range initialContacts {
		k.routingTable.Update(contact)
	}

	// 查找自身节点ID的k个最近节点
	// 这有助于快速填充路由表
	_, err := k.FindNode(k.nodeID)
	return err
}

func (k *Kademlia) findValueIterative(key []byte, initialContacts []*Contact) ([]byte, []*Contact, error) {
	queried := make(map[string]bool)
	closest := NewContactList(k.nodeID)
	toQuery := NewContactList(k.nodeID)

	// 添加初始联系人
	for _, contact := range initialContacts {
		toQuery.Add(contact)
	}

	for toQuery.Len() > 0 {
		// 选择alpha个最近的未查询节点
		batch := toQuery.GetClosest(k.alpha)
		if len(batch) == 0 {
			break
		}

		// 并行查询
		results := make(chan findValueResult, len(batch))
		var wg sync.WaitGroup

		for _, contact := range batch {
			contactID := hex.EncodeToString(contact.ID[:])
			if queried[contactID] {
				continue
			}
			queried[contactID] = true

			wg.Add(1)
			go func(c *Contact) {
				defer wg.Done()
				value, contacts, err := k.network.SendFindValue(c, key)
				results <- findValueResult{
					value:    value,
					contacts: contacts,
					err:      err,
				}
			}(contact)
		}

		wg.Wait()
		close(results)

		// 处理结果
		for result := range results {
			if result.err != nil {
				continue
			}

			// 如果找到值，立即返回
			if result.value != nil {
				return result.value, nil, nil
			}

			// 添加新发现的联系人
			for _, contact := range result.contacts {
				if !queried[hex.EncodeToString(contact.ID[:])] {
					toQuery.Add(contact)
					closest.Add(contact)
				}
			}
		}

		// 更新最近的k个节点
		closest.Trim(k.k)
	}

	// 没有找到值，返回最近的节点
	return nil, closest.GetContacts(), errors.New("value not found")
}

func (k *Kademlia) findNodeIterative(target NodeID, initialContacts []*Contact) ([]*Contact, error) {
	queried := make(map[string]bool)
	closest := NewContactList(target[:])
	toQuery := NewContactList(target[:])

	// 添加初始联系人
	for _, contact := range initialContacts {
		toQuery.Add(contact)
	}

	for toQuery.Len() > 0 {
		// 选择alpha个最近的未查询节点
		batch := toQuery.GetClosest(k.alpha)
		if len(batch) == 0 {
			break
		}

		// 并行查询
		results := make(chan findNodeResult, len(batch))
		var wg sync.WaitGroup

		for _, contact := range batch {
			contactID := hex.EncodeToString(contact.ID[:])
			if queried[contactID] {
				continue
			}
			queried[contactID] = true

			wg.Add(1)
			go func(c *Contact) {
				defer wg.Done()
				contacts, err := k.network.SendFindNode(c, target)
				results <- findNodeResult{
					contacts: contacts,
					err:      err,
				}
			}(contact)
		}

		wg.Wait()
		close(results)

		// 处理结果
		for result := range results {
			if result.err != nil {
				continue
			}

			// 添加新发现的联系人
			for _, contact := range result.contacts {
				if !queried[hex.EncodeToString(contact.ID[:])] {
					toQuery.Add(contact)
					closest.Add(contact)
				}
			}
		}

		// 如果没有发现新节点，停止迭代
		if toQuery.Len() == 0 {
			break
		}
	}

	// 返回最近的k个节点
	closest.Trim(k.k)
	return closest.GetContacts(), nil
}

func (k *Kademlia) createContactFromAddress(addr string) *Contact {
	// 简化实现，实际需要网络连接
	nodeID := k.generateNodeID(addr)
	return &Contact{
		ID:       nodeID,
		Addr:     &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 9000},
		LastSeen: time.Now(),
		Status:   ContactStatusGood,
	}
}

func (k *Kademlia) generateNodeID(addr string) NodeID {
	hash := sha1.Sum([]byte(addr))
	var nodeID NodeID
	copy(nodeID[:], hash[:])
	return nodeID
}

// 辅助结构体
type findValueResult struct {
	value    []byte
	contacts []*Contact
	err      error
}

type findNodeResult struct {
	contacts []*Contact
	err      error
}

// CalculateDistance 计算两个节点ID之间的距离（XOR）
func CalculateDistance(a, b NodeID) *big.Int {
	var result big.Int
	var aInt, bInt big.Int

	aInt.SetBytes(a[:])
	bInt.SetBytes(b[:])

	result.Xor(&aInt, &bInt)
	return &result
}

// GenerateNodeID 生成随机节点ID
func GenerateNodeID() NodeID {
	var nodeID NodeID
	// 在实际实现中应该使用加密安全的随机数
	// 这里使用时间戳的简化实现
	timestamp := time.Now().UnixNano()
	hash := sha1.Sum([]byte(string(timestamp)))
	copy(nodeID[:], hash[:])
	return nodeID
}