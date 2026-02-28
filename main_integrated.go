package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"bufio"
	"os"
	"strings"

	"github.com/WetQuill/p2p-chatroom/bridge"
	"github.com/WetQuill/p2p-chatroom/config"
	"github.com/WetQuill/p2p-chatroom/models"
	"github.com/WetQuill/p2p-chatroom/pkg/ipv6"
	"github.com/gorilla/websocket"
	"github.com/sirupsen/logrus"
)

// main.go 全局变量
var (
	uiConn *websocket.Conn // 存储当前打开的网页连接
	uiMu   sync.Mutex      // 保护 uiConn 的并发访问

	// IPv6模块相关
	ipv6Module   ipv6.Module          // IPv6模块实例
	ipv6Config   *config.IPv6Config   // IPv6配置
	bridgeMgr    *bridge.BridgeManager // 连接桥接管理器
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true // 允许跨域，生产环境应严格校验
	},
}

// loadIPv6Module 加载IPv6模块
func loadIPv6Module(cfg *config.Config) (ipv6.Module, error) {
	// 加载IPv6配置
	ipv6Cfg := config.DefaultIPv6Config()

	// 根据主配置调整IPv6配置
	if cfg.EnableIPv6 {
		ipv6Cfg.Enabled = true
		ipv6Cfg.Mode = cfg.IPv6Mode
		ipv6Cfg.UDPEnabled = cfg.UDPEnabled
		ipv6Cfg.Encryption = cfg.EncryptionMode
	}

	// 创建模块
	module := ipv6.NewModule()

	// 初始化模块
	if err := module.Init(ipv6Cfg); err != nil {
		return nil, fmt.Errorf("IPv6模块初始化失败: %v", err)
	}

	// 启动模块
	if err := module.Start(); err != nil {
		return nil, fmt.Errorf("IPv6模块启动失败: %v", err)
	}

	return module, nil
}

// runIPv6Discovery 运行IPv6发现
func runIPv6Discovery(module ipv6.Module, user *models.User) {
	logrus.Info("IPv6发现模块启动")

	// 订阅发现事件
	discoverChan := module.Discover()
	eventChan := module.SubscribeEvents()

	for {
		select {
		case peer := <-discoverChan:
			handleDiscoveredPeer(peer, user)
		case event := <-eventChan:
			handleIPv6Event(event, user)
		}
	}
}

// handleDiscoveredPeer 处理发现的节点
func handleDiscoveredPeer(peer *ipv6.PeerInfo, user *models.User) {
	logrus.Infof("发现新节点: %s (%s)", peer.ID, peer.Address)

	// 创建用户信息
	userInfo := &models.UserInfo{
		Id:       generateUserIDFromPeerID(peer.ID),
		Port:     0, // IPv6节点没有端口概念
		UserName: fmt.Sprintf("IPv6-%s", peer.ID[:8]),
	}

	// 添加到地址列表
	user.AddressList.AppendWithConn(userInfo, nil)

	// 通过桥接器发送加入消息
	if bridgeMgr != nil {
		msg := user.NewJoinMessage(userInfo)
		bridgeMgr.GetBridge().Broadcast(msg)
	}
}

// handleIPv6Event 处理IPv6事件
func handleIPv6Event(event *ipv6.Event, user *models.User) {
	switch event.Type {
	case ipv6.EventPeerConnected:
		logrus.Infof("IPv6节点连接: %s", event.Message)
	case ipv6.EventPeerDisconnected:
		logrus.Infof("IPv6节点断开: %s", event.Message)
	case ipv6.EventConnectionFailed:
		logrus.Warnf("IPv6连接失败: %s", event.Message)
	case ipv6.EventAddressChanged:
		logrus.Infof("IPv6地址变更: %s", event.Message)
	}
}

// generateUserIDFromPeerID 从PeerID生成用户ID
func generateUserIDFromPeerID(peerID string) int {
	// 简单哈希算法生成用户ID
	hash := 0
	for _, c := range peerID {
		hash = (hash*31 + int(c)) % 1000
	}
	return hash + 1000 // 从1000开始，避免与IPv4用户ID冲突
}

// main 主函数
func main() {
	// 解析命令行参数
	configPath := flag.String("config", "config.json", "配置文件路径")
	ipv6Probe := flag.Bool("ipv6-probe", false, "运行IPv6探测工具")
	flag.Parse()

	// 如果指定了IPv6探测，运行探测工具
	if *ipv6Probe {
		runIPv6ProbeTool()
		return
	}

	// 加载配置
	cfg, err := config.Load(*configPath)
	if err != nil {
		logrus.Warnf("配置文件加载失败，使用默认配置: %v", err)
		cfg = config.Default()
	}

	// 验证配置
	if err := cfg.Validate(); err != nil {
		logrus.Fatalf("配置验证失败: %v", err)
	}

	logrus.Infof("配置加载成功:")
	logrus.Infof("  模式: %s", cfg.Mode)
	logrus.Infof("  用户名: %s", cfg.UserName)
	logrus.Infof("  IPv6支持: %v", cfg.EnableIPv6)

	// 初始化用户
	myUser := models.NewUser(cfg.UserName)
	if cfg.Port != 0 {
		myUser.Port = cfg.Port
	}

	// 端口自动发现
	portInit := 1010
	listener, err := net.Listen("tcp", ":"+strconv.Itoa(myUser.Port))
	for i := 0; i < 10 && err != nil; i++ {
		myUser.Port = portInit + i*10
		listener, err = net.Listen("tcp", ":"+strconv.Itoa(myUser.Port))
	}

	if err != nil {
		logrus.Fatalf("端口自动发现失败: %v", err)
	}

	logrus.Infof("成功接入:%d端口", myUser.Port)

	// IPv6模块条件加载
	if cfg.EnableIPv6 {
		logrus.Info("正在加载IPv6模块...")

		ipv6Module, err = loadIPv6Module(cfg)
		if err != nil {
			logrus.Warnf("IPv6模块加载失败（不影响IPv4功能）: %v", err)
		} else {
			defer ipv6Module.Stop()
			logrus.Info("IPv6模块已成功加载")

			// 启动IPv6发现协程
			go runIPv6Discovery(ipv6Module, myUser)
		}

		// 初始化桥接管理器
		bridgeMgr = bridge.NewBridgeManager(myUser.AddressList)
		bridgeMgr.Start()
		defer bridgeMgr.Stop()
	}

	// 开启HTTP服务
	go func() {
		mux := http.NewServeMux()
		mux.Handle("/", http.FileServer(http.Dir("./static"))) // 托管 HTML
		mux.HandleFunc("/ws-ui", func(w http.ResponseWriter, r *http.Request) {
			uiHandler(w, r, myUser)
		})
		mux.HandleFunc("/ws-p2p", func(w http.ResponseWriter, r *http.Request) {
			conn, err := upgrader.Upgrade(w, r, nil)
			if err != nil {
				return
			}
			go handleIncomingMessages(conn, myUser)
		})
		http.Serve(listener, mux)
	}()

	// 本地模式：连接本地对等节点
	if cfg.EnableLocalMode {
		for j := 1; j <= 10; j++ {
			targetPort := portInit + j*10
			if targetPort == myUser.Port {
				continue
			}

			targetUrl := fmt.Sprintf("ws://localhost:%d/ws-p2p", targetPort)
			var dialer websocket.Dialer
			conn, _, err := dialer.Dial(targetUrl, nil)

			if err == nil {
				myUser.AddressList.AppendWithConn(&models.UserInfo{Id: j, Port: targetPort}, conn)
				go handleIncomingMessages(conn, myUser)

				newJoinMsg := myUser.NewOnlineOffMessage(models.JoinMessage)
				if err := sendMessage(conn, newJoinMsg); err != nil {
					fmt.Printf("连接端口:%d失败\n> ", targetPort)
				}
			}
		}
	}

	// 远程模式：连接到信令服务器
	if cfg.Mode == "remote" && cfg.SignalingServer != "" {
		go connectToSignalingServer(cfg.SignalingServer, myUser)
	}

	// 主循环：读取控制台输入
	runConsoleLoop(scanner, myUser)
}

// runConsoleLoop 运行控制台循环
func runConsoleLoop(scanner *bufio.Scanner, user *models.User) {
	fmt.Print("> ")
	for scanner.Scan() {
		text := scanner.Text()

		if text == "" {
			fmt.Print("> ")
			continue
		}

		// 处理命令
		switch strings.ToLower(text) {
		case "exit":
			handleExitCommand(user)
		case "checkconn":
			handleCheckConnCommand(user)
		case "ipv6-status":
			handleIPv6StatusCommand()
		case "ipv6-stats":
			handleIPv6StatsCommand()
		default:
			handleChatMessage(text, user)
		}

		fmt.Print("> ")
	}
}

// handleExitCommand 处理退出命令
func handleExitCommand(user *models.User) {
	// 发送退出消息
	newExitMsg := user.NewOnlineOffMessage(models.ExitMessage)
	user.AddressList.Mu.Lock()
	for _, conn := range user.AddressList.PeersConn {
		sendMessage(conn, newExitMsg)
	}
	user.AddressList.Mu.Unlock()

	// 通过桥接器广播退出消息
	if bridgeMgr != nil {
		bridgeMgr.GetBridge().Broadcast(newExitMsg)
	}

	os.Exit(0)
}

// handleCheckConnCommand 处理检查连接命令
func handleCheckConnCommand(user *models.User) {
	user.AddressList.Mu.Lock()

	fmt.Println("\n======= 当前连接情况 =======")
	if len(user.AddressList.PeersConn) == 0 {
		fmt.Println("状态: 暂无活跃连接")
	} else {
		fmt.Printf("当前在线人数: %d\n", len(user.AddressList.PeersConn))
		fmt.Println("ID\t端口\t用户名\t\t远程地址\t类型")
		fmt.Println("--\t----\t------\t\t--------\t----")

		for id, conn := range user.AddressList.PeersConn {
			userName := "Unknown"
			targetPort := 0
			connType := "IPv4"

			if u, exists := user.AddressList.UserList[id]; exists {
				userName = u.UserName
				targetPort = u.Port
			}

			remoteAddr := "N/A"
			if conn != nil {
				remoteAddr = conn.RemoteAddr().String()
			}

			// 检查是否是IPv6连接
			if id >= 1000 {
				connType = "IPv6"
			}

			fmt.Printf("%d\t%d\t%-12s\t%s\t%s\n", id, targetPort, userName, remoteAddr, connType)
		}
	}

	// 显示IPv6状态
	if ipv6Module != nil {
		status := ipv6Module.GetStatus()
		fmt.Printf("\nIPv6模块状态: %s\n", status.State)
		fmt.Printf("IPv6地址: %v\n", status.Addresses)
		fmt.Printf("IPv6连接数: %d\n", status.Connections)
	}

	user.AddressList.Mu.Unlock()
}

// handleIPv6StatusCommand 处理IPv6状态命令
func handleIPv6StatusCommand() {
	if ipv6Module == nil {
		fmt.Println("IPv6模块未启用")
		return
	}

	status := ipv6Module.GetStatus()
	metrics := ipv6Module.GetMetrics()

	fmt.Println("\n======= IPv6模块状态 =======")
	fmt.Printf("状态: %s\n", status.State)
	fmt.Printf("运行时间: %v\n", status.Uptime)
	fmt.Printf("健康分数: %d/100\n", status.HealthScore)
	fmt.Printf("本地地址: %v\n", status.Addresses)
	fmt.Printf("连接数: %d\n", status.Connections)
	fmt.Printf("对等节点: %d\n", status.Peers)

	fmt.Println("\n======= IPv6性能指标 =======")
	fmt.Printf("连接建立: %d\n", metrics.ConnectionsEstablished)
	fmt.Printf("连接失败: %d\n", metrics.ConnectionsFailed)
	fmt.Printf("发送字节: %d\n", metrics.BytesSent)
	fmt.Printf("接收字节: %d\n", metrics.BytesReceived)
	fmt.Printf("平均延迟: %.2fms\n", metrics.AvgLatency)
	fmt.Printf("丢包率: %.2f%%\n", metrics.PacketLossRate)
}

// handleIPv6StatsCommand 处理IPv6统计命令
func handleIPv6StatsCommand() {
	if bridgeMgr == nil {
		fmt.Println("桥接管理器未启用")
		return
	}

	stats := bridgeMgr.GetBridge().GetBridgeStats()
	connStats := bridgeMgr.GetBridge().GetConnectionStats()

	fmt.Println("\n======= IPv6桥接统计 =======")
	fmt.Printf("总连接数: %d\n", stats.TotalConnections)
	fmt.Printf("IPv4连接: %d\n", stats.IPv4Connections)
	fmt.Printf("IPv6连接: %d\n", stats.IPv6Connections)
	fmt.Printf("转发消息: %d\n", stats.MessagesForwarded)
	fmt.Printf("转发字节: %d\n", stats.BytesForwarded)
	fmt.Printf("迁移次数: %d\n", stats.MigrationCount)

	fmt.Println("\n======= 连接分布 =======")
	fmt.Printf("IPv4占比: %.1f%%\n", connStats.IPv4Percentage)
	fmt.Printf("IPv6占比: %.1f%%\n", connStats.IPv6Percentage)
}

// handleChatMessage 处理聊天消息
func handleChatMessage(text string, user *models.User) {
	newMsg := user.NewRegularMessage(text, time.Now().Format("15:04:05"))

	// 发送到所有IPv4连接
	user.AddressList.Mu.Lock()
	for _, conn := range user.AddressList.PeersConn {
		sendMessage(conn, newMsg)
	}
	user.AddressList.Mu.Unlock()

	// 通过桥接器广播到所有连接（包括IPv6）
	if bridgeMgr != nil {
		bridgeMgr.GetBridge().Broadcast(newMsg)
	}

	// 发送到Web界面
	uiMu.Lock()
	if uiConn != nil {
		data, _ := json.Marshal(newMsg)
		uiConn.WriteMessage(websocket.TextMessage, data)
	}
	uiMu.Unlock()
}

// runIPv6ProbeTool 运行IPv6探测工具
func runIPv6ProbeTool() {
	fmt.Println("运行IPv6探测工具...")
	// 这里可以调用cmd/ipv6-probe/main.go的功能
	// 简化实现：提示用户运行独立工具
	fmt.Println("请运行: go run ./cmd/ipv6-probe/main.go")
	fmt.Println("或: ./ipv6-probe.exe")
}

// 注意：原有的uiHandler、sendMessage、handleIncomingMessages、
// connectToSignalingServer等函数保持不变，需要从原始main.go复制过来