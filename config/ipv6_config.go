package config

// IPv6Config 独立配置结构，与主配置解耦
type IPv6Config struct {
	// 启用开关
	Enabled       bool   `json:"enabled"`           // 是否启用IPv6模块
	ModuleVersion string `json:"moduleVersion"`     // 模块版本

	// 网络模式
	Mode      string `json:"mode"`      // "preferred"（优先IPv6）、"only"、"fallback"、"disabled"
	DualStack bool   `json:"dualStack"` // 启用双栈

	// 地址配置
	ListenAddress   string `json:"listenAddress"`   // 监听地址，如"[::]:0"
	PreferredPrefix string `json:"preferredPrefix"` // 首选地址前缀

	// 传输协议
	UDPEnabled      bool `json:"udpEnabled"`      // 启用UDP
	UDPPortStart    int  `json:"udpPortStart"`    // UDP起始端口
	UDPPortEnd      int  `json:"udpPortEnd"`      // UDP结束端口
	TCPEnabled      bool `json:"tcpEnabled"`      // 启用TCP
	WebSocketCompat bool `json:"webSocketCompat"` // WebSocket兼容模式

	// 发现机制
	DHTEnabled     bool     `json:"dhtEnabled"`     // 启用DHT
	BootstrapNodes []string `json:"bootstrapNodes"` // DHT引导节点
	MDNSEnabled    bool     `json:"mdnsEnabled"`    // mDNS发现

	// 安全配置
	Encryption   string `json:"encryption"`   // "none"、"noise"
	NoisePattern string `json:"noisePattern"` // 如"XX_25519_ChaChaPoly_BLAKE2s"
	EnableAuth   bool   `json:"enableAuth"`   // 启用身份认证
	KeyRotation  int    `json:"keyRotation"`  // 密钥轮换周期（小时）

	// 性能调优
	KeepAliveInterval int `json:"keepAliveInterval"` // 保活间隔（秒）
	ConnectionTimeout int `json:"connectionTimeout"` // 连接超时（秒）
	MaxRetries        int `json:"maxRetries"`        // 最大重试次数
}

// DefaultIPv6Config 返回默认IPv6配置
func DefaultIPv6Config() *IPv6Config {
	return &IPv6Config{
		Enabled:         false,
		ModuleVersion:   "1.0.0",
		Mode:            "disabled",
		DualStack:       false,
		ListenAddress:   "[::]:0",
		PreferredPrefix: "",
		UDPEnabled:      false,
		UDPPortStart:    10100,
		UDPPortEnd:      10200,
		TCPEnabled:      true,
		WebSocketCompat: true,
		DHTEnabled:      false,
		BootstrapNodes: []string{
			"bootstrap.ipv6.p2p.example.com:9000",
		},
		MDNSEnabled:        false,
		Encryption:         "none",
		NoisePattern:       "XX_25519_ChaChaPoly_BLAKE2s",
		EnableAuth:         false,
		KeyRotation:        24,
		KeepAliveInterval:  30,
		ConnectionTimeout:  10,
		MaxRetries:         3,
	}
}