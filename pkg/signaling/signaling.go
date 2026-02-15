package signaling

import (
	"encoding/json"
	"log"
	"sync"

	"github.com/WetQuill/p2p-chatroom/models"
	"github.com/gorilla/websocket"
)

type SignalingServer struct {
	peers map[int]*PeerInfo
	mu    sync.RWMutex
}

type PeerInfo struct {
	ID       int    `json:"id"`
	Port     int    `json:"port"`
	UserName string `json:"userName"`
	PublicIP string `json:"publicIP"`
	Conn     *websocket.Conn
	mu       sync.Mutex // Protects Conn writes
}

// NewSignalingServer creates a new signaling server instance
func NewSignalingServer() *SignalingServer {
	return &SignalingServer{
		peers: make(map[int]*PeerInfo),
	}
}

// HandleConnection processes WebSocket connections from peers
func (ss *SignalingServer) HandleConnection(conn *websocket.Conn) {
	defer conn.Close()

	// Wait for registration (JoinMessage)
	var firstMsg models.Message
	_, data, err := conn.ReadMessage()
	if err != nil {
		log.Printf("Failed to read initial message: %v", err)
		return
	}

	if err := json.Unmarshal(data, &firstMsg); err != nil {
		log.Printf("Failed to unmarshal initial message: %v", err)
		return
	}

	if firstMsg.MsgType != models.JoinMessage {
		log.Printf("Expected JoinMessage, got type %d", firstMsg.MsgType)
		return
	}

	peerID := firstMsg.Sender.Id
	peerInfo := &PeerInfo{
		ID:       peerID,
		Port:     firstMsg.Sender.Port,
		UserName: firstMsg.Sender.UserName,
		PublicIP: "", // Will be updated if peer provides it
		Conn:     conn,
	}

	ss.RegisterPeer(peerInfo)
	log.Printf("Peer %s (ID:%d) registered", peerInfo.UserName, peerID)

	// Main message handling loop
	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			log.Printf("Peer %s (ID:%d) disconnected: %v", peerInfo.UserName, peerID, err)
			ss.UnregisterPeer(peerID)
			return
		}

		var msg models.Message
		if err := json.Unmarshal(data, &msg); err != nil {
			log.Printf("Failed to unmarshal message from peer %d: %v", peerID, err)
			continue
		}

		switch msg.MsgType {
		case models.PeerDiscovery:
			// Send current peer list to requesting peer
			peerList := ss.GetPeerList()
			peerListJSON, _ := json.Marshal(peerList)

			response := &models.Message{
				MsgType: models.PeerList,
				Content: string(peerListJSON),
				Sender: models.UserInfo{
					Id: 0, // Server ID
					Port: 0,
					UserName: "Server",
				},
			}

			peerInfo.mu.Lock()
			ss.SendMessage(conn, response)
			peerInfo.mu.Unlock()

		case models.RegularMessage:
			// Regular chat messages are handled as-is
			// In a production system, you might want to relay or store these
			log.Printf("Chat message from %s: %s", peerInfo.UserName, msg.Content)

		case models.WebRTCOffer, models.WebRTCAnswer, models.ICECandidateMsg:
			// Relay signaling messages between peers
			if msg.TargetID > 0 {
				ss.RelayMessage(msg.TargetID, &msg)
			} else {
				log.Printf("Received signaling message without target ID from peer %d", peerID)
			}

		default:
			log.Printf("Unknown message type %d from peer %d", msg.MsgType, peerID)
		}
	}
}

// RegisterPeer adds a new peer to the registry
func (ss *SignalingServer) RegisterPeer(info *PeerInfo) {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	ss.peers[info.ID] = info
}

// UnregisterPeer removes a peer from the registry
func (ss *SignalingServer) UnregisterPeer(peerID int) {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	delete(ss.peers, peerID)
}

// GetPeerList returns all registered peers
func (ss *SignalingServer) GetPeerList() []PeerInfo {
	ss.mu.RLock()
	defer ss.mu.RUnlock()

	list := make([]PeerInfo, 0, len(ss.peers))
	for _, p := range ss.peers {
		list = append(list, PeerInfo{
			ID:       p.ID,
			Port:     p.Port,
			UserName: p.UserName,
			PublicIP: p.PublicIP,
		})
	}
	return list
}

// RelayMessage forwards signaling messages between peers
func (ss *SignalingServer) RelayMessage(targetID int, msg *models.Message) error {
	ss.mu.RLock()
	target, exists := ss.peers[targetID]
	ss.mu.RUnlock()

	if !exists {
		return nil // Target peer not found, silently ignore
	}

	target.mu.Lock()
	defer target.mu.Unlock()

	return ss.SendMessage(target.Conn, msg)
}

// SendMessage sends a message through a WebSocket connection
func (ss *SignalingServer) SendMessage(conn *websocket.Conn, msg *models.Message) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	return conn.WriteMessage(websocket.TextMessage, data)
}
