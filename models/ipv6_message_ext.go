package models

import (
	"encoding/json"
	"fmt"
)

// IPv6MessageExtension IPv6消息扩展
type IPv6MessageExtension struct {
	// 网络信息
	IPv6Address    string   `json:"ipv6,omitempty"`
	PeerID         string   `json:"peerId,omitempty"`
	Transport      string   `json:"transport,omitempty"`  // "udp", "tcp", "ws"

	// DHT相关
	DHTKey         string   `json:"dhtKey,omitempty"`
	DHTValue       string   `json:"dhtValue,omitempty"`
	DHTNodes       []string `json:"dhtNodes,omitempty"`

	// 安全相关
	HandshakeData  []byte   `json:"handshake,omitempty"`
	PublicKey      []byte   `json:"pubKey,omitempty"`
	Signature      []byte   `json:"sig,omitempty"`

	// 性能指标
	RTT            int      `json:"rtt,omitempty"`      // 往返时间（ms）
	Bandwidth      int      `json:"bw,omitempty"`       // 估计带宽（kbps）
	LossRate       float64  `json:"loss,omitempty"`     // 丢包率

	// 扩展字段
	ExtensionData  map[string]interface{} `json:"ext,omitempty"`
}

// ExtractIPv6Extension 从消息中提取IPv6扩展信息
func ExtractIPv6Extension(msg *Message) (*IPv6MessageExtension, bool) {
	if msg == nil {
		return nil, false
	}

	// 尝试从现有字段解析扩展信息
	// 方法1：检查是否有扩展字段
	if msg.Content != "" {
		var ext IPv6MessageExtension
		if err := json.Unmarshal([]byte(msg.Content), &ext); err == nil {
			// 检查是否包含IPv6相关信息
			if ext.IPv6Address != "" || ext.PeerID != "" || ext.PublicKey != nil {
				return &ext, true
			}
		}
	}

	// 方法2：检查发送者信息中是否包含IPv6信息
	if msg.Sender != nil {
		// 可以在这里检查UserInfo是否有IPv6字段
		// 目前UserInfo没有IPv6字段，需要扩展
	}

	return nil, false
}

// CreateIPv6Message 创建包含IPv6扩展信息的消息
func CreateIPv6Message(base *Message, ext *IPv6MessageExtension) *Message {
	if base == nil {
		return nil
	}

	// 创建消息副本
	msg := *base

	// 序列化扩展信息
	extData, err := json.Marshal(ext)
	if err != nil {
		// 序列化失败，返回原始消息
		return base
	}

	// 将扩展信息添加到内容中
	// 在实际实现中，可能需要更复杂的编码方式
	msg.Content = string(extData)

	return &msg
}

// CreateIPv6RegularMessage 创建IPv6常规消息
func CreateIPv6RegularMessage(sender *UserInfo, content string, ext *IPv6MessageExtension) *Message {
	baseMsg := NewRegularMessage(sender, content, GetCurrentTime())
	return CreateIPv6Message(baseMsg, ext)
}

// CreateIPv6JoinMessage 创建IPv6加入消息
func CreateIPv6JoinMessage(sender *UserInfo, ext *IPv6MessageExtension) *Message {
	baseMsg := NewJoinMessage(sender, GetCurrentTime())
	return CreateIPv6Message(baseMsg, ext)
}

// CreateIPv6ExitMessage 创建IPv6退出消息
func CreateIPv6ExitMessage(sender *UserInfo, ext *IPv6MessageExtension) *Message {
	baseMsg := NewExitMessage(sender, GetCurrentTime())
	return CreateIPv6Message(baseMsg, ext)
}

// IPv6MessageTypes IPv6特定消息类型
const (
	// 扩展现有消息类型范围 (100-199)
	IPv6Discovery   = 100 // IPv6发现请求
	IPv6PeerList    = 101 // IPv6对等列表
	DHTQuery        = 102 // DHT查询
	DHTResponse     = 103 // DHT响应
	HandshakeRequest = 104 // 加密握手请求
	HandshakeResponse = 105 // 加密握手响应
	IPv6Ping        = 106 // IPv6 Ping请求
	IPv6Pong        = 107 // IPv6 Pong响应
	KeyExchange     = 108 // 密钥交换
	SessionUpdate   = 109 // 会话更新
)

// NewIPv6Message 创建IPv6特定消息
func NewIPv6Message(msgType int, sender *UserInfo, content string, ext *IPv6MessageExtension) *Message {
	msg := &Message{
		MsgType: msgType,
		Content: content,
		Time:    GetCurrentTime(),
		Sender:  sender,
	}

	if ext != nil {
		return CreateIPv6Message(msg, ext)
	}

	return msg
}

// CreateIPv6DiscoveryMessage 创建IPv6发现消息
func CreateIPv6DiscoveryMessage(sender *UserInfo, peerID string, ipv6Addr string) *Message {
	ext := &IPv6MessageExtension{
		PeerID:      peerID,
		IPv6Address: ipv6Addr,
		Transport:   "udp",
	}

	return NewIPv6Message(IPv6Discovery, sender, "", ext)
}

// CreateDHTPeerListMessage 创建DHT对等列表消息
func CreateDHTPeerListMessage(sender *UserInfo, peers []string) *Message {
	ext := &IPv6MessageExtension{
		DHTNodes: peers,
	}

	return NewIPv6Message(IPv6PeerList, sender, "", ext)
}

// CreateHandshakeRequestMessage 创建握手请求消息
func CreateHandshakeRequestMessage(sender *UserInfo, handshakeData []byte, publicKey []byte) *Message {
	ext := &IPv6MessageExtension{
		HandshakeData: handshakeData,
		PublicKey:     publicKey,
	}

	return NewIPv6Message(HandshakeRequest, sender, "", ext)
}

// CreateKeyExchangeMessage 创建密钥交换消息
func CreateKeyExchangeMessage(sender *UserInfo, publicKey []byte, signature []byte) *Message {
	ext := &IPv6MessageExtension{
		PublicKey: publicKey,
		Signature: signature,
	}

	return NewIPv6Message(KeyExchange, sender, "", ext)
}

// IPv6UserInfo 扩展的用户信息（包含IPv6信息）
type IPv6UserInfo struct {
	UserInfo
	IPv6Address    string   `json:"ipv6Address,omitempty"`
	PeerID         string   `json:"peerId,omitempty"`
	PublicKey      []byte   `json:"publicKey,omitempty"`
	IsIPv6Enabled  bool     `json:"isIPv6Enabled"`
	ConnectionType string   `json:"connectionType,omitempty"` // "ipv4", "ipv6", "dual"
}

// ToUserInfo 转换为标准UserInfo
func (ui *IPv6UserInfo) ToUserInfo() *UserInfo {
	return &UserInfo{
		Id:       ui.Id,
		Port:     ui.Port,
		UserName: ui.UserName,
	}
}

// FromUserInfo 从标准UserInfo创建IPv6UserInfo
func FromUserInfo(user *UserInfo) *IPv6UserInfo {
	if user == nil {
		return nil
	}

	return &IPv6UserInfo{
		UserInfo:      *user,
		IsIPv6Enabled: false,
	}
}

// IPv6AddressList IPv6地址列表（扩展版本）
type IPv6AddressList struct {
	AddressList
	IPv6Peers map[string]*IPv6UserInfo // PeerID -> IPv6用户信息
	mu        sync.RWMutex
}

// NewIPv6AddressList 创建IPv6地址列表
func NewIPv6AddressList() *IPv6AddressList {
	return &IPv6AddressList{
		AddressList: *NewAddressList(),
		IPv6Peers:   make(map[string]*IPv6UserInfo),
	}
}

// AddIPv6Peer 添加IPv6对等节点
func (al *IPv6AddressList) AddIPv6Peer(peer *IPv6UserInfo) {
	al.mu.Lock()
	defer al.mu.Unlock()

	al.IPv6Peers[peer.PeerID] = peer

	// 同时添加到基础地址列表
	al.AppendWithConn(peer.ToUserInfo(), nil) // 连接需要单独设置
}

// RemoveIPv6Peer 移除IPv6对等节点
func (al *IPv6AddressList) RemoveIPv6Peer(peerID string) {
	al.mu.Lock()
	defer al.mu.Unlock()

	if peer, exists := al.IPv6Peers[peerID]; exists {
		// 从基础地址列表移除
		al.DeleteAddress(peer.Id)
		delete(al.IPv6Peers, peerID)
	}
}

// GetIPv6Peer 获取IPv6对等节点
func (al *IPv6AddressList) GetIPv6Peer(peerID string) *IPv6UserInfo {
	al.mu.RLock()
	defer al.mu.RUnlock()

	return al.IPv6Peers[peerID]
}

// GetAllIPv6Peers 获取所有IPv6对等节点
func (al *IPv6AddressList) GetAllIPv6Peers() []*IPv6UserInfo {
	al.mu.RLock()
	defer al.mu.RUnlock()

	peers := make([]*IPv6UserInfo, 0, len(al.IPv6Peers))
	for _, peer := range al.IPv6Peers {
		peers = append(peers, peer)
	}
	return peers
}

// FindPeerByIPv6Address 通过IPv6地址查找对等节点
func (al *IPv6AddressList) FindPeerByIPv6Address(ipv6Addr string) *IPv6UserInfo {
	al.mu.RLock()
	defer al.mu.RUnlock()

	for _, peer := range al.IPv6Peers {
		if peer.IPv6Address == ipv6Addr {
			return peer
		}
	}
	return nil
}

// IPv6MessageHandler IPv6消息处理器接口
type IPv6MessageHandler interface {
	HandleIPv6Discovery(msg *Message, ext *IPv6MessageExtension) error
	HandleIPv6PeerList(msg *Message, ext *IPv6MessageExtension) error
	HandleDHTQuery(msg *Message, ext *IPv6MessageExtension) error
	HandleDHTResponse(msg *Message, ext *IPv6MessageExtension) error
	HandleHandshakeRequest(msg *Message, ext *IPv6MessageExtension) error
	HandleHandshakeResponse(msg *Message, ext *IPv6MessageExtension) error
	HandleKeyExchange(msg *Message, ext *IPv6MessageExtension) error
}

// DefaultIPv6MessageHandler 默认IPv6消息处理器
type DefaultIPv6MessageHandler struct {
	addressList *IPv6AddressList
}

// NewDefaultIPv6MessageHandler 创建默认IPv6消息处理器
func NewDefaultIPv6MessageHandler(addressList *IPv6AddressList) *DefaultIPv6MessageHandler {
	return &DefaultIPv6MessageHandler{
		addressList: addressList,
	}
}

// HandleIPv6Discovery 处理IPv6发现消息
func (h *DefaultIPv6MessageHandler) HandleIPv6Discovery(msg *Message, ext *IPv6MessageExtension) error {
	if ext == nil || msg.Sender == nil {
		return fmt.Errorf("invalid IPv6 discovery message")
	}

	// 创建IPv6用户信息
	peer := &IPv6UserInfo{
		UserInfo:      *msg.Sender,
		IPv6Address:   ext.IPv6Address,
		PeerID:        ext.PeerID,
		PublicKey:     ext.PublicKey,
		IsIPv6Enabled: true,
		ConnectionType: ext.Transport,
	}

	// 添加到地址列表
	h.addressList.AddIPv6Peer(peer)

	return nil
}

// HandleIPv6PeerList 处理IPv6对等列表消息
func (h *DefaultIPv6MessageHandler) HandleIPv6PeerList(msg *Message, ext *IPv6MessageExtension) error {
	if ext == nil {
		return fmt.Errorf("invalid IPv6 peer list message")
	}

	// 处理对等列表
	// 在实际实现中，这会更新本地的对等节点列表
	return nil
}

// HandleDHTQuery 处理DHT查询消息
func (h *DefaultIPv6MessageHandler) HandleDHTQuery(msg *Message, ext *IPv6MessageExtension) error {
	// 处理DHT查询
	return nil
}

// HandleDHTResponse 处理DHT响应消息
func (h *DefaultIPv6MessageHandler) HandleDHTResponse(msg *Message, ext *IPv6MessageExtension) error {
	// 处理DHT响应
	return nil
}

// HandleHandshakeRequest 处理握手请求消息
func (h *DefaultIPv6MessageHandler) HandleHandshakeRequest(msg *Message, ext *IPv6MessageExtension) error {
	if ext == nil {
		return fmt.Errorf("invalid handshake request message")
	}

	// 处理握手请求
	// 在实际实现中，这会启动或响应握手过程
	return nil
}

// HandleHandshakeResponse 处理握手响应消息
func (h *DefaultIPv6MessageHandler) HandleHandshakeResponse(msg *Message, ext *IPv6MessageExtension) error {
	if ext == nil {
		return fmt.Errorf("invalid handshake response message")
	}

	// 处理握手响应
	return nil
}

// HandleKeyExchange 处理密钥交换消息
func (h *DefaultIPv6MessageHandler) HandleKeyExchange(msg *Message, ext *IPv6MessageExtension) error {
	if ext == nil {
		return fmt.Errorf("invalid key exchange message")
	}

	// 验证签名
	// 在实际实现中，这会验证公钥和签名
	return nil
}

// IPv6MessageRouter IPv6消息路由器
type IPv6MessageRouter struct {
	handlers map[int]IPv6MessageHandler
}

// NewIPv6MessageRouter 创建IPv6消息路由器
func NewIPv6MessageRouter() *IPv6MessageRouter {
	return &IPv6MessageRouter{
		handlers: make(map[int]IPv6MessageHandler),
	}
}

// RegisterHandler 注册处理器
func (r *IPv6MessageRouter) RegisterHandler(msgType int, handler IPv6MessageHandler) {
	r.handlers[msgType] = handler
}

// RouteMessage 路由消息
func (r *IPv6MessageRouter) RouteMessage(msg *Message) error {
	if msg == nil {
		return fmt.Errorf("nil message")
	}

	// 提取IPv6扩展信息
	ext, hasExt := ExtractIPv6Extension(msg)

	// 查找处理器
	handler, exists := r.handlers[msg.MsgType]
	if !exists {
		return fmt.Errorf("no handler for message type %d", msg.MsgType)
	}

	// 根据消息类型调用相应的处理器
	switch msg.MsgType {
	case IPv6Discovery:
		return handler.HandleIPv6Discovery(msg, ext)
	case IPv6PeerList:
		return handler.HandleIPv6PeerList(msg, ext)
	case DHTQuery:
		return handler.HandleDHTQuery(msg, ext)
	case DHTResponse:
		return handler.HandleDHTResponse(msg, ext)
	case HandshakeRequest:
		return handler.HandleHandshakeRequest(msg, ext)
	case HandshakeResponse:
		return handler.HandleHandshakeResponse(msg, ext)
	case KeyExchange:
		return handler.HandleKeyExchange(msg, ext)
	default:
		return fmt.Errorf("unsupported IPv6 message type: %d", msg.MsgType)
	}
}