package models

import (
	"fmt"

	"github.com/WetQuill/p2p-chatroom/config"
	"github.com/gorilla/websocket"
)

const portInit int = 1000

type User struct {
	Port        int
	Id          int
	UserName    string
	AddressList AddressList

	// New fields for NAT traversal
	PublicIP string
	Mode     string
	Config   *config.Config
}

type UserInfo struct {
	Port     int    `json:"port"`
	Id       int    `json:"id"`
	UserName string `json:"userName"`
	PublicIP string `json:"publicIP,omitempty"`
}

func NewCreateUser(i int) *User {
	p := portInit + i*10
	return &User{
		Port:     p,
		Id:       i,
		UserName: fmt.Sprintf("User-%d", i),
		AddressList: AddressList{
			PeersConn: make(map[int]*websocket.Conn), // 必须在这里 make
			UserList:  make(map[int]*UserInfo),
		},
	}
}

func NewUserInfo(id int, userName string) *UserInfo {
	p := portInit + id*10
	return &UserInfo{
		Port:     p,
		Id:       id,
		UserName: userName,
	}
}
