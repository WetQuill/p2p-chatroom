package dht

import (
	"container/list"
	"crypto/sha1"
	"encoding/hex"
	"math/big"
	"net"
	"sort"
	"sync"
	"time"
)

// RoutingTable 路由表实现
type RoutingTable struct {
	mu       sync.RWMutex
	kbuckets [160]*KBucket
	selfID   NodeID
	contacts map[string]*Contact
}

// NewRoutingTable 创建路由表
func NewRoutingTable(selfID NodeID) *RoutingTable {
	rt := &RoutingTable{
		selfID:   selfID,
		contacts: make(map[string]*Contact),
	}

	for i := 0; i < 160; i++ {
		rt.kbuckets[i] = NewKBucket()
	}

	return rt
}

// Update 更新或添加联系人
func (rt *RoutingTable) Update(contact *Contact) {
	if contact == nil {
		return
	}

	// 不添加自身
	if contact.ID == rt.selfID {
		return
	}

	rt.mu.Lock()
	defer rt.mu.Unlock()

	distance := CalculateDistance(rt.selfID, contact.ID)
	bucketIndex := rt.getBucketIndex(distance)

	// 更新最后联系时间
	contact.LastSeen = time.Now()
	contactID := hex.EncodeToString(contact.ID[:])

	// 检查是否已存在
	if existing, exists := rt.contacts[contactID]; exists {
		// 更新现有联系人
		existing.Addr = contact.Addr
		existing.LastSeen = contact.LastSeen
		existing.RTT = contact.RTT
		existing.Status = ContactStatusGood

		// 移动到桶的前面
		rt.kbuckets[bucketIndex].MoveToFront(contactID)
		return
	}

	// 添加新联系人
	rt.contacts[contactID] = contact
	if !rt.kbuckets[bucketIndex].Add(contactID) {
		// 桶已满，需要处理
		rt.handleFullBucket(bucketIndex, contact)
	}
}

// Remove 移除联系人
func (rt *RoutingTable) Remove(contact *Contact) {
	if contact == nil {
		return
	}

	rt.mu.Lock()
	defer rt.mu.Unlock()

	contactID := hex.EncodeToString(contact.ID[:])
	delete(rt.contacts, contactID)

	distance := CalculateDistance(rt.selfID, contact.ID)
	bucketIndex := rt.getBucketIndex(distance)
	rt.kbuckets[bucketIndex].Remove(contactID)
}

// FindClosest 查找最近的k个节点
func (rt *RoutingTable) FindClosest(target []byte, count int) []*Contact {
	if len(target) != 20 {
		return nil
	}

	var targetID NodeID
	copy(targetID[:], target)

	rt.mu.RLock()
	defer rt.mu.RUnlock()

	// 获取所有联系人
	allContacts := make([]*Contact, 0, len(rt.contacts))
	for _, contact := range rt.contacts {
		allContacts = append(allContacts, contact)
	}

	// 按距离排序
	sort.Slice(allContacts, func(i, j int) bool {
		distI := CalculateDistance(targetID, allContacts[i].ID)
		distJ := CalculateDistance(targetID, allContacts[j].ID)
		return distI.Cmp(distJ) < 0
	})

	// 返回最近的count个
	if len(allContacts) > count {
		return allContacts[:count]
	}
	return allContacts
}

// Size 获取路由表大小
func (rt *RoutingTable) Size() int {
	rt.mu.RLock()
	defer rt.mu.RUnlock()

	return len(rt.contacts)
}

// GetContacts 获取所有联系人
func (rt *RoutingTable) GetContacts() []*Contact {
	rt.mu.RLock()
	defer rt.mu.RUnlock()

	contacts := make([]*Contact, 0, len(rt.contacts))
	for _, contact := range rt.contacts {
		contacts = append(contacts, contact)
	}
	return contacts
}

// GetStats 获取路由表统计信息
func (rt *RoutingTable) GetStats() RoutingTableStats {
	rt.mu.RLock()
	defer rt.mu.RUnlock()

	stats := RoutingTableStats{
		TotalContacts: len(rt.contacts),
		BucketCounts:  make([]int, 160),
	}

	// 统计每个桶的联系人数量
	for i := 0; i < 160; i++ {
		stats.BucketCounts[i] = rt.kbuckets[i].Size()
	}

	// 统计联系人状态
	for _, contact := range rt.contacts {
		switch contact.Status {
		case ContactStatusGood:
			stats.GoodContacts++
		case ContactStatusQuestionable:
			stats.QuestionableContacts++
		case ContactStatusBad:
			stats.BadContacts++
		}
	}

	return stats
}

// 私有方法

func (rt *RoutingTable) getBucketIndex(distance *big.Int) int {
	// 计算距离的对数，确定桶的索引
	// 距离为0表示自身，放到最后一个桶
	if distance.Sign() == 0 {
		return 159
	}

	// 计算最高有效位的位置
	bitLength := distance.BitLen()
	if bitLength > 159 {
		return 0
	}
	return 159 - bitLength
}

func (rt *RoutingTable) handleFullBucket(bucketIndex int, newContact *Contact) {
	bucket := rt.kbuckets[bucketIndex]

	// 尝试替换不活跃的联系人
	oldestID := bucket.GetOldest()
	if oldestID == "" {
		return
	}

	oldestContact, exists := rt.contacts[oldestID]
	if !exists {
		return
	}

	// 检查最旧联系人是否仍然活跃
	if time.Since(oldestContact.LastSeen) > 15*time.Minute {
		// 移除不活跃联系人
		delete(rt.contacts, oldestID)
		bucket.Remove(oldestID)

		// 添加新联系人
		rt.contacts[hex.EncodeToString(newContact.ID[:])] = newContact
		bucket.Add(hex.EncodeToString(newContact.ID[:]))
	}
}

// KBucket K桶实现
type KBucket struct {
	mu              sync.RWMutex
	contacts        *list.List
	contactMap      map[string]*list.Element
	lastChanged     time.Time
	replacementCache []string
	maxSize         int
}

// NewKBucket 创建K桶
func NewKBucket() *KBucket {
	return &KBucket{
		contacts:        list.New(),
		contactMap:      make(map[string]*list.Element),
		lastChanged:     time.Now(),
		replacementCache: make([]string, 0),
		maxSize:         20, // Kademlia标准K值
	}
}

// Add 添加联系人到桶
func (kb *KBucket) Add(contactID string) bool {
	kb.mu.Lock()
	defer kb.mu.Unlock()

	// 检查是否已存在
	if _, exists := kb.contactMap[contactID]; exists {
		return false
	}

	// 检查是否已满
	if kb.contacts.Len() >= kb.maxSize {
		return false
	}

	// 添加到前面（LRU策略）
	element := kb.contacts.PushFront(contactID)
	kb.contactMap[contactID] = element
	kb.lastChanged = time.Now()

	return true
}

// Remove 从桶中移除联系人
func (kb *KBucket) Remove(contactID string) bool {
	kb.mu.Lock()
	defer kb.mu.Unlock()

	element, exists := kb.contactMap[contactID]
	if !exists {
		return false
	}

	kb.contacts.Remove(element)
	delete(kb.contactMap, contactID)
	kb.lastChanged = time.Now()

	return true
}

// MoveToFront 将联系人移动到前面
func (kb *KBucket) MoveToFront(contactID string) bool {
	kb.mu.Lock()
	defer kb.mu.Unlock()

	element, exists := kb.contactMap[contactID]
	if !exists {
		return false
	}

	kb.contacts.MoveToFront(element)
	kb.lastChanged = time.Now()

	return true
}

// Contains 检查是否包含联系人
func (kb *KBucket) Contains(contactID string) bool {
	kb.mu.RLock()
	defer kb.mu.RUnlock()

	_, exists := kb.contactMap[contactID]
	return exists
}

// Size 获取桶大小
func (kb *KBucket) Size() int {
	kb.mu.RLock()
	defer kb.mu.RUnlock()

	return kb.contacts.Len()
}

// GetContacts 获取所有联系人ID
func (kb *KBucket) GetContacts() []string {
	kb.mu.RLock()
	defer kb.mu.RUnlock()

	contacts := make([]string, 0, kb.contacts.Len())
	for element := kb.contacts.Front(); element != nil; element = element.Next() {
		if contactID, ok := element.Value.(string); ok {
			contacts = append(contacts, contactID)
		}
	}
	return contacts
}

// GetOldest 获取最旧的联系人ID
func (kb *KBucket) GetOldest() string {
	kb.mu.RLock()
	defer kb.mu.RUnlock()

	if kb.contacts.Len() == 0 {
		return ""
	}

	oldest := kb.contacts.Back()
	if oldest == nil {
		return ""
	}

	if contactID, ok := oldest.Value.(string); ok {
		return contactID
	}
	return ""
}

// AddToReplacementCache 添加到替换缓存
func (kb *KBucket) AddToReplacementCache(contactID string) {
	kb.mu.Lock()
	defer kb.mu.Unlock()

	// 避免重复
	for _, id := range kb.replacementCache {
		if id == contactID {
			return
		}
	}

	kb.replacementCache = append(kb.replacementCache, contactID)
	// 限制缓存大小
	if len(kb.replacementCache) > kb.maxSize {
		kb.replacementCache = kb.replacementCache[1:]
	}
}

// ContactList 联系人列表（用于迭代查找）
type ContactList struct {
	target   []byte
	contacts []*Contact
	mu       sync.RWMutex
}

// NewContactList 创建联系人列表
func NewContactList(target []byte) *ContactList {
	return &ContactList{
		target:   target,
		contacts: make([]*Contact, 0),
	}
}

// Add 添加联系人
func (cl *ContactList) Add(contact *Contact) {
	cl.mu.Lock()
	defer cl.mu.Unlock()

	// 避免重复
	for _, c := range cl.contacts {
		if c.ID == contact.ID {
			return
		}
	}

	cl.contacts = append(cl.contacts, contact)
}

// Len 获取联系人数量
func (cl *ContactList) Len() int {
	cl.mu.RLock()
	defer cl.mu.RUnlock()

	return len(cl.contacts)
}

// GetContacts 获取所有联系人
func (cl *ContactList) GetContacts() []*Contact {
	cl.mu.RLock()
	defer cl.mu.RUnlock()

	return cl.contacts
}

// GetClosest 获取最近的n个联系人
func (cl *ContactList) GetClosest(n int) []*Contact {
	cl.mu.Lock()
	defer cl.mu.Unlock()

	if len(cl.contacts) == 0 {
		return nil
	}

	// 按距离排序
	var targetID NodeID
	if len(cl.target) == 20 {
		copy(targetID[:], cl.target)
	}

	sort.Slice(cl.contacts, func(i, j int) bool {
		distI := CalculateDistance(targetID, cl.contacts[i].ID)
		distJ := CalculateDistance(targetID, cl.contacts[j].ID)
		return distI.Cmp(distJ) < 0
	})

	if len(cl.contacts) <= n {
		return cl.contacts
	}
	return cl.contacts[:n]
}

// Trim 修剪到最近的n个联系人
func (cl *ContactList) Trim(n int) {
	cl.mu.Lock()
	defer cl.mu.Unlock()

	if len(cl.contacts) <= n {
		return
	}

	// 按距离排序
	var targetID NodeID
	if len(cl.target) == 20 {
		copy(targetID[:], cl.target)
	}

	sort.Slice(cl.contacts, func(i, j int) bool {
		distI := CalculateDistance(targetID, cl.contacts[i].ID)
		distJ := CalculateDistance(targetID, cl.contacts[j].ID)
		return distI.Cmp(distJ) < 0
	})

	cl.contacts = cl.contacts[:n]
}

// RoutingTableStats 路由表统计
type RoutingTableStats struct {
	TotalContacts        int     `json:"total_contacts"`
	GoodContacts         int     `json:"good_contacts"`
	QuestionableContacts int     `json:"questionable_contacts"`
	BadContacts          int     `json:"bad_contacts"`
	BucketCounts         []int   `json:"bucket_counts"`
	AvgContactsPerBucket float64 `json:"avg_contacts_per_bucket"`
}

// GetStats 获取统计信息
func (rts RoutingTableStats) GetStats() RoutingTableStats {
	if len(rts.BucketCounts) == 0 {
		rts.AvgContactsPerBucket = 0
	} else {
		sum := 0
		for _, count := range rts.BucketCounts {
			sum += count
		}
		rts.AvgContactsPerBucket = float64(sum) / float64(len(rts.BucketCounts))
	}
	return rts
}