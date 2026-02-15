package models

const (
	RegularMessage   = 1
	JoinMessage      = 2
	ExitMessage      = 3
	JoinReplyMessage = 4

	// New: Signaling messages for NAT traversal
	PeerDiscovery  = 5 // Request peer list from server
	PeerList       = 6 // Server responds with peer list
	WebRTCOffer    = 7 // SDP offer
	WebRTCAnswer   = 8 // SDP answer
	ICECandidateMsg = 9 // ICE candidate message type
)

type Message struct {
	MsgType int    `json:"msgType"`
	Content string `json:"content"`
	Time    string `json:"time"`

	Sender UserInfo `json:"userInfo"`

	// New fields for signaling
	TargetID       int           `json:"targetId,omitempty"`
	SDP            *SDPSession   `json:"sdp,omitempty"`
	IceCandidate   *ICECandidate `json:"iceCandidate,omitempty"`
}

type SDPSession struct {
	Type string `json:"type"` // "offer" or "answer"
	SDP  string `json:"sdp"`
}

type ICECandidate struct {
	Candidate     string `json:"candidate"`
	SDPMLineIndex int    `json:"sdpMLineIndex"`
	SDPMid        string `json:"sdpMid"`
}

func (u *User) NewRegularMessage(c string, time string) *Message {
	return &Message{
		MsgType: RegularMessage,
		Sender: UserInfo{
			Id:       u.Id,
			Port:     u.Port,
			UserName: u.UserName,
		},
		Content: c,
		Time:    time,
	}
}

func (u *User) NewOnlineOffMessage(status int) *Message {
	return &Message{
		MsgType: status,
		Sender: UserInfo{
			Id:       u.Id,
			Port:     u.Port,
			UserName: u.UserName,
		},
	}
}

func (u *User) NewJoinReplyMessage() *Message {
	return &Message{
		MsgType: JoinReplyMessage,
		Sender: UserInfo{
			Id:       u.Id,
			Port:     u.Port,
			UserName: u.UserName,
		},
	}
}
