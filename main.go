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

	"github.com/WetQuill/p2p-chatroom/config"
	"github.com/WetQuill/p2p-chatroom/models"
	"github.com/gorilla/websocket"
	"github.com/sirupsen/logrus"
)

// main.go 全局变量
var (
	uiConn *websocket.Conn // 存储当前打开的网页连接
	uiMu   sync.Mutex      // 保护 uiConn 的并发访问
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true // 允许跨域，生产环境应严格校验
	},
}

func uiHandler(w http.ResponseWriter, r *http.Request, myUser *models.User) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}

	uiMu.Lock()
	uiConn = conn // 注册当前网页连接
	uiMu.Unlock()

	defer func() {
		uiMu.Lock()
		uiConn = nil
		uiMu.Unlock()
	}()

	defer conn.Close()

	for {
		// 1. 接收前端传来的消息
		_, text, err := conn.ReadMessage()
		if err != nil {
			break
		}

		// 封装并广播给所有 P2P 节点
		newMsg := myUser.NewRegularMessage(string(text), time.Now().Format("15:04:05"))

		myUser.AddressList.Mu.Lock()
		for _, pConn := range myUser.AddressList.PeersConn {
			sendMessage(pConn, newMsg) // 借用你已有的发送逻辑
		}
		myUser.AddressList.Mu.Unlock()

		// 同时发回给自己的网页展示
		data, _ := json.Marshal(newMsg)
		conn.WriteMessage(websocket.TextMessage, data)
	}
}

func sendMessage(conn *websocket.Conn, msg *models.Message) error {
	if conn == nil {
		return fmt.Errorf("连接未建立")
	}

	// 1. 序列化：将结构体转为 []byte
	// json.Marshal 返回 (data []byte, err error)
	data, err := json.Marshal(*msg)
	if err != nil {
		return err
	}

	// 2. 发送：此时 data 已经是 []byte 了，直接传入即可
	// 我们选择 TextMessage，因为 JSON 是可读文本
	err = conn.WriteMessage(websocket.TextMessage, data)
	return err
}

func handleIncomingMessages(conn *websocket.Conn, myUser *models.User) {
	defer conn.Close()
	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			// 如果连接断开，可以在这里处理下线逻辑
			return
		}

		var recvMsg models.Message
		if err := json.Unmarshal(data, &recvMsg); err != nil {
			continue
		}

		switch recvMsg.MsgType {
		case models.JoinMessage:
			// 收到他人上线的消息，将对方存入通讯录（复用当前连接，修复冗余问题）
			myUser.AddressList.AppendWithConn(&recvMsg.Sender, conn)
			fmt.Printf("\n[系统] 用户 %s (ID:%d) 已上线\n> ", recvMsg.Sender.UserName, recvMsg.Sender.Id)

		case models.RegularMessage:
			// 收到普通聊天消息
			fmt.Printf("\n[%s]: %s\n> ", recvMsg.Sender.UserName, recvMsg.Content)

		case models.ExitMessage:
			fmt.Printf("\n[系统] 用户 %s 已下线\n> ", recvMsg.Sender.UserName)
			myUser.AddressList.DeleteAddress(recvMsg.Sender.Id)
		}

		// 将消息转发给前端网页
		uiMu.Lock()
		if uiConn != nil {
			uiConn.WriteMessage(websocket.TextMessage, data)
		}
		uiMu.Unlock()
	}
}

func main() {
	// Parse command line flags
	configPath := flag.String("config", "config.json", "Path to configuration file")
	flag.Parse()

	// Load configuration
	cfg, err := config.Load(*configPath)
	if err != nil {
		logrus.Warn("Failed to load config, using defaults: ", err)
		cfg = config.Default()
	}

	// Validate configuration
	if err := cfg.Validate(); err != nil {
		logrus.Fatal("Invalid configuration: ", err)
	}

	logrus.Info("Configuration loaded:")
	logrus.Info("  Mode: ", cfg.Mode)
	logrus.Info("  UserName: ", cfg.UserName)
	if cfg.Mode == "remote" {
		logrus.Info("  SignalingServer: ", cfg.SignalingServer)
	}

	// 1. 确定自己所在端口
	const portInit int = 1000
	var (
		myUser   *models.User
		listener net.Listener
		listenErr error
	)

	// var portOccupiedList []int
	// 尝试连接每一个端口
	for i := 1; i <= 10; i++ {
		port := portInit + i*10
		address := ":" + strconv.Itoa(port)
		listener, listenErr = net.Listen("tcp", address)
		if listenErr == nil {
			myUser = models.NewCreateUser(i)
			fmt.Printf("成功接入:%d端口\n", portInit+i*10)
			break
		}
		fmt.Printf("%s已被占用, 正在尝试下一个端口...\n", address)
	}

	if listener == nil {
		fmt.Println("无空余端口...")
		return
	}

	// Update User creation to use config values
	myUser.Port = myUser.Port // Already set by port discovery
	if cfg.UserName != "" {
		myUser.UserName = cfg.UserName
	}
	myUser.Mode = cfg.Mode
	myUser.Config = cfg

	// STUN discovery for remote mode
	if cfg.Mode == "remote" && len(cfg.STUNServers) > 0 {
		logrus.Info("Attempting STUN discovery...")
		if ip, port, ok := TrySTUNServers(cfg.STUNServers); ok {
			myUser.PublicIP = ip
			logrus.Infof("Discovered public IP: %s:%d", ip, port)
		} else {
			logrus.Warn("STUN discovery failed, continuing without public IP")
		}
	}

	// 2. 开启监听自身端口 接收消息
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

	// 3. 向所有端口转发自己上线的消息 每个节点都需要回应JoinMessage
	// Local mode: Connect to localhost peers (backward compatible)
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

	// Remote mode: Connect to signaling server
	if cfg.Mode == "remote" && cfg.SignalingServer != "" {
		go connectToSignalingServer(cfg.SignalingServer, myUser)
	}

	// 4. 循环读取控制台输入并发送
	scanner := bufio.NewScanner(os.Stdin)
	fmt.Print("> ")
	for scanner.Scan() {
		text := scanner.Text()

		if text == "" {
			fmt.Print("> ")
			continue
		}

		if strings.ToLower(text) == "exit" {
			// 这里可以补充发送 ExitMessage 的逻辑
			newExitMsg := myUser.NewOnlineOffMessage(models.ExitMessage)
			myUser.AddressList.Mu.Lock()
			for _, conn := range myUser.AddressList.PeersConn {
				sendMessage(conn, newExitMsg)
			}
			myUser.AddressList.Mu.Unlock()
			os.Exit(0)
		}

		if strings.ToLower(text) == "checkconn" {
			myUser.AddressList.Mu.Lock() // 必须加锁，防止遍历时后台协程写入导致崩溃

			fmt.Println("\n======= 当前连接情况 =======")
			if len(myUser.AddressList.PeersConn) == 0 {
				fmt.Println("状态: 暂无活跃连接")
			} else {
				fmt.Printf("当前在线人数: %d\n", len(myUser.AddressList.PeersConn))
				fmt.Println("ID\t端口\t用户名\t\t远程地址")
				fmt.Println("--\t----\t------\t\t--------")

				// 遍历通讯录中的连接
				for id, conn := range myUser.AddressList.PeersConn {
					// 尝试从 UserList 中寻找更详细的信息
					userName := "Unknown"
					targetPort := portInit + id*10 // 根据你的 portInit 规则计算

					if u, exists := myUser.AddressList.UserList[id]; exists {
						userName = u.UserName
						targetPort = u.Port
					}

					remoteAddr := "N/A"
					if conn != nil {
						remoteAddr = conn.RemoteAddr().String()
					}

					fmt.Printf("%d\t%d\t%-12s\t%s\n", id, targetPort, userName, remoteAddr)
				}
				fmt.Printf("> ")
			}
			fmt.Println("================================")

			myUser.AddressList.Mu.Unlock() // 遍历结束，立即解锁
			fmt.Print("> ")
			continue // 跳过后续的消息发送逻辑
		}

		newMsg := myUser.NewRegularMessage(text, time.Now().Format("15:04:05"))

		// 定义一个切片来存放发送失败的 ID
		var failedIDs []int

		myUser.AddressList.Mu.Lock()
		for id, conn := range myUser.AddressList.PeersConn {
			err := sendMessage(conn, newMsg)
			// 如果发送失败 则认定对方下线 从通讯录中删除
			if err != nil {
				fmt.Printf("向 ID:%d 发送失败\n> ", id)
				failedIDs = append(failedIDs, id)
			}
		}
		myUser.AddressList.Mu.Unlock()

		for _, id := range failedIDs {
			myUser.AddressList.DeleteAddress(id)
		}
	}
}

// connectToSignalingServer connects to the signaling server for remote mode
func connectToSignalingServer(serverURL string, myUser *models.User) {
	var dialer websocket.Dialer
	conn, _, err := dialer.Dial(serverURL, nil)
	if err != nil {
		logrus.Errorf("Failed to connect to signaling server: %v", err)
		return
	}
	defer conn.Close()

	logrus.Info("Connected to signaling server: ", serverURL)

	// Send registration message with public IP if available
	registerMsg := myUser.NewOnlineOffMessage(models.JoinMessage)
	if myUser.PublicIP != "" {
		registerMsg.Content = myUser.PublicIP
	}
	if err := sendMessage(conn, registerMsg); err != nil {
		logrus.Errorf("Failed to send registration: %v", err)
		return
	}

	// Request peer list
	discoveryMsg := &models.Message{
		MsgType: models.PeerDiscovery,
	}
	sendMessage(conn, discoveryMsg)

	// Handle incoming messages
	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			logrus.Info("Disconnected from signaling server")
			return
		}

		var msg models.Message
		if err := json.Unmarshal(data, &msg); err != nil {
			logrus.Warnf("Failed to unmarshal message from signaling server: %v", err)
			continue
		}

		switch msg.MsgType {
		case models.PeerList:
			handlePeerList(msg, myUser)

		case models.WebRTCOffer, models.WebRTCAnswer, models.ICECandidateMsg:
			logrus.Infof("Received signaling message type %d (WebRTC not yet implemented)", msg.MsgType)

		case models.RegularMessage:
			fmt.Printf("\n[%s]: %s\n> ", msg.Sender.UserName, msg.Content)

			// Forward to web UI
			uiMu.Lock()
			if uiConn != nil {
				uiConn.WriteMessage(websocket.TextMessage, data)
			}
			uiMu.Unlock()

		case models.JoinMessage:
			// New peer joined
			fmt.Printf("\n[系统] 用户 %s (ID:%d) 已上线\n> ", msg.Sender.UserName, msg.Sender.Id)
			myUser.AddressList.UserList[msg.Sender.Id] = &msg.Sender

		case models.ExitMessage:
			// Peer left
			fmt.Printf("\n[系统] 用户 %s 已下线\n> ", msg.Sender.UserName)
			myUser.AddressList.DeleteAddress(msg.Sender.Id)
		}
	}
}

// handlePeerList processes peer list from signaling server and initiates connections
func handlePeerList(msg models.Message, myUser *models.User) {
	if msg.Content == "" {
		return
	}

	var peers []models.UserInfo
	if err := json.Unmarshal([]byte(msg.Content), &peers); err != nil {
		logrus.Errorf("Failed to parse peer list: %v", err)
		return
	}

	logrus.Infof("Received peer list with %d peers", len(peers))

	for _, peer := range peers {
		if peer.Id == myUser.Id {
			continue // Skip self
		}

		// Check if already connected
		if conn := myUser.AddressList.GetConnection(peer.Id); conn != nil {
			continue
		}

		// For Phase 1, we'll try to connect via WebSocket
		// In the future, this will initiate WebRTC connections
		targetURL := fmt.Sprintf("ws://%s:%d/ws-p2p", peer.PublicIP, peer.Port)
		if targetURL == "ws://:0/ws-p2p" || targetURL == "ws:///:0/ws-p2p" {
			continue // Skip invalid addresses
		}

		logrus.Infof("Attempting to connect to peer %s at %s", peer.UserName, targetURL)

		var dialer websocket.Dialer
		conn, _, err := dialer.Dial(targetURL, nil)
		if err == nil {
			myUser.AddressList.AppendWithConn(&peer, conn)
			go handleIncomingMessages(conn, myUser)

			// Send join message
			newJoinMsg := myUser.NewOnlineOffMessage(models.JoinMessage)
			sendMessage(conn, newJoinMsg)

			fmt.Printf("\n[系统] 已连接到 %s (ID:%d)\n> ", peer.UserName, peer.Id)
		} else {
			logrus.Warnf("Failed to connect to %s: %v", targetURL, err)
		}
	}
}
