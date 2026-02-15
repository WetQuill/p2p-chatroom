package main

import (
	"log"
	"net/http"

	"github.com/WetQuill/p2p-chatroom/pkg/signaling"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow cross-origin connections
	},
}

func main() {
	ss := signaling.NewSignalingServer()

	mux := http.NewServeMux()
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Printf("WebSocket upgrade failed: %v", err)
			return
		}
		go ss.HandleConnection(conn)
	})

	log.Println("Signaling server starting on :9000...")
	if err := http.ListenAndServe(":9000", mux); err != nil {
		log.Fatalf("Server failed to start: %v", err)
	}
}
