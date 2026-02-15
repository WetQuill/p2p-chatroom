package models

import (
	"sync"

	"github.com/gorilla/websocket"
)

type AddressList struct {
	UserList  map[int]*UserInfo
	PeersConn map[int]*websocket.Conn
	Mu        sync.Mutex
}

// 未建立连接
// func (AL *AddressList) Append(u *UserInfo) {
// 	AL.Mu.Lock()
// 	defer AL.Mu.Unlock()

// 	// 必须初始化 map，否则会 panic
// 	if AL.PeersConn == nil {
// 		AL.PeersConn = make(map[int]*websocket.Conn)
// 	}

// 	// 检查是否已经连接过，避免重复拨号
// 	if _, exists := AL.PeersConn[u.Id]; exists {
// 		return
// 	}

// 	targetUrl := fmt.Sprintf("ws://localhost:%d", u.Port)
// 	var dialer websocket.Dialer
// 	conn, _, err := dialer.Dial(targetUrl, nil)
// 	if err != nil {
// 		fmt.Printf("连接端口:%d失败\n", u.Port)
// 		return
// 	}
// 	AL.PeersConn[u.Id] = conn
// 	AL.UserList[u.Id] = u
// 	// 在main中关闭
// }

// AppendWithConn 用于将已经拨通的连接直接存入通讯录
func (AL *AddressList) AppendWithConn(u *UserInfo, conn *websocket.Conn) {
	AL.Mu.Lock()
	defer AL.Mu.Unlock()

	if AL.PeersConn == nil {
		AL.PeersConn = make(map[int]*websocket.Conn)
	}

	// 存入连接，供后续 scanner 发消息使用
	AL.PeersConn[u.Id] = conn
	AL.UserList[u.Id] = u
}

func (AL *AddressList) DeleteAddress(id int) {
	AL.Mu.Lock()
	defer AL.Mu.Unlock()

	if AL.PeersConn == nil {
		return
	}

	delete(AL.PeersConn, id)
	delete(AL.UserList, id)
}

// GetConnection retrieves a WebSocket connection for a given peer ID
func (AL *AddressList) GetConnection(peerID int) *websocket.Conn {
	AL.Mu.Lock()
	defer AL.Mu.Unlock()
	return AL.PeersConn[peerID]
}
