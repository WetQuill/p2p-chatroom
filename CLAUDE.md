# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

A decentralized P2P chatroom application in Go with NAT traversal support. Each node runs as both a server and client, communicating via WebSockets. Supports dual modes: local mode for LAN/localhost testing and remote mode with STUN/signaling servers for cross-network communication. Provides both console and web-based interfaces.

For comprehensive user documentation, see `使用说明.md` (Chinese) for detailed setup, troubleshooting, and deployment guides.

## Build and Run

### Main Application
```bash
# Build executable
go build -o p2p-chatroom.exe .

# Or build with default name
go build

# Run directly
go run main.go

# Run with custom configuration
go run main.go -config path/to/config.json
```

### Signaling Server (for remote mode)
```bash
# Build signaling server
cd server && go build -o signaling-server.exe .

# Run signaling server
cd server && go run main.go
```

### Development Workflow
```bash
# Check dependencies
go mod tidy

# Format code
gofmt -w .

# View module dependencies
go mod graph

# Download all dependencies
go mod download
```

### Testing Notes
This project currently has no test files (`*_test.go`). When adding new features:
1. Create appropriate test files in the same directory
2. Use `go test ./...` to run all tests
3. Use `go test -v ./package/name` for verbose output

To test P2P functionality, run multiple instances simultaneously - they will auto-discover available ports.

## Architecture

### Node Communication Model

- **Port Discovery**: Each node attempts to bind to ports 1010, 1020, 1030, ..., 1100. The first available port is used.
- **P2P Mesh**: Every node maintains WebSocket connections to all other active nodes. On startup, it attempts to connect to every port in the 1010-1100 range.
- **Message Broadcasting**: Messages are sent to all connected peers simultaneously.

### Core Components

**main.go** (entry point):
- `uiHandler`: WebSocket endpoint `/ws-ui` for web UI communication
- `handleIncomingMessages`: Processes incoming P2P WebSocket messages via `/ws-p2p`
- `sendMessage`: Serializes and sends Message structs as JSON
- Port auto-discovery loop (lines 139-149)
- P2P connection establishment loop (lines 174-193)
- Console input handler with special commands

**models/User**: Represents a node with ID, port, username, and address book

**models/AddressList**: Thread-safe peer registry
- `PeersConn`: Map of user ID to WebSocket connections
- `UserList`: Map of user ID to UserInfo
- `AppendWithConn`: Stores an established connection
- `DeleteAddress`: Removes a disconnected peer

**models/Message**: Message protocol
- `MsgType`: Integer type (1=Regular, 2=Join, 3=Exit, 4=JoinReply)
- `Content`: Message text
- `Time`: Timestamp
- `Sender`: UserInfo (id, port, userName)

### WebSocket Endpoints

- `/ws-p2p`: Inter-node P2P communication
- `/ws-ui`: Web UI communication (served at `/` from `static/` directory)

### Web UI

`static/index.html` provides a browser-based chat interface that connects to `/ws-ui`. The JavaScript automatically detects the hostname and port.

## Project Structure

### Key Directories and Files

- `main.go` - Application entry point with port discovery and connection management
- `stun_discovery.go` - STUN-based public IP discovery for NAT traversal
- `config/config.go` - Configuration management supporting dual modes (local/remote)
- `models/` - Core data structures
  - `user.go` - User models and identity management
  - `message.go` - Message protocol with 9 message types (1-9)
  - `AddressList.go` - Thread-safe peer registry with mutex protection
- `server/` - Signaling server implementation
  - `main.go` - Signaling server entry point for remote mode coordination
- `pkg/signaling/signaling.go` - WebSocket signaling server logic
- `static/index.html` - Web interface with auto-detection of host/port

### Configuration Management

The application supports configuration via `config.json`:
- `mode`: "local" (localhost testing) or "remote" (cross-network with signaling server)
- `port`: 0 for auto-discovery or specific port
- `signalingServer`: WebSocket URL for signaling server (remote mode)
- `stunServers`: List of STUN servers for public IP discovery
- `enableLocalMode`: Boolean to enable local port scanning (1010-1100) alongside remote connections

Default configuration template: `config.json.example`

### Dependencies and Go Version

- **Go Version**: 1.25.6
- **Main Dependencies**:
  - `github.com/gorilla/websocket` - WebSocket communication
  - `github.com/pion/*` - WebRTC ecosystem for STUN/NAT traversal (planned feature)
  - `github.com/sirupsen/logrus` - Structured logging

See `go.mod` for complete dependency list with indirect dependencies for WebRTC support.

## Console Commands

- `exit`: Gracefully shutdown and send ExitMessage to all peers
- `checkconn`: Display current connection status (ID, port, username, remote address)

## Important Patterns

### Message Flow

1. Console input → `NewRegularMessage` → broadcast to all `PeersConn`
2. Incoming P2P message → `handleIncomingMessages` → switch on `MsgType` → update state/forward to UI
3. Web UI input → `uiHandler` → broadcast as `RegularMessage`

### Concurrency Model

- **Primary Mutexes**:
  - `AddressList.Mu` - Protects `PeersConn` and `UserList` maps in models/AddressList.go
  - `uiMu` - Protects Web UI WebSocket connection in main.go
- Always lock before accessing `PeersConn`, `UserList`, or `uiConn`
- Each WebSocket connection runs in its own goroutine
- Message sending is non-blocking; failed sends trigger peer removal

### Connection Management

**Port Discovery**: Sequential attempt on ports 1010, 1020, ..., 1100. First available port is used.

**Peer Connection**: On startup, attempt to connect to all ports in 1010-1100 range (local mode) OR connect via signaling server (remote mode).

**Cleanup**: Failed sends result in peer removal from `AddressList` via `DeleteAddress`. All nodes receive `ExitMessage` when a peer disconnects.

### Message Types (models/message.go)

| Type ID | Name | Purpose |
|---------|------|---------|
| 1 | RegularMessage | Standard chat messages |
| 2 | JoinMessage | Node online announcement |
| 3 | ExitMessage | Node offline announcement |
| 4 | JoinReplyMessage | Response to join request |
| 5 | PeerDiscovery | Request for peer list |
| 6 | PeerList | Response with peer list |
| 7 | WebRTCOffer | WebRTC SDP offer (future feature) |
| 8 | WebRTCAnswer | WebRTC SDP answer (future feature) |
| 9 | ICECandidate | WebRTC ICE candidate (future feature) |

## Development Guidelines

### Adding New Features

1. **Message Types**: Extend `MsgType` enum in `models/message.go` with appropriate handler in `main.go:handleIncomingMessages`
2. **Configuration**: Add fields to `Config` struct in `config/config.go` and update JSON parsing
3. **State Management**: Use existing `AddressList` for thread-safe peer state; add new mutex-protected structures if needed

### Code Organization

- Keep WebSocket handling in `main.go`
- Move reusable logic to appropriate packages (`models/`, `pkg/`)
- Configuration-related code in `config/`
- Signaling server code in `server/` and `pkg/signaling/`

### Error Handling Patterns

- WebSocket send failures: Remove peer from `AddressList`
- Connection failures: Log error and continue (retry logic for signaling server)
- Configuration errors: Fall back to defaults with clear logging

## Future Development Roadmap

Referenced from `使用说明.md`:

- WebRTC data channel support for better NAT traversal
- TURN server support for symmetric NATs
- DHT-based peer discovery (decentralized)
- DTLS encryption for secure communication
- File transfer functionality
- Group chat features
- Message persistence
- User authentication

When implementing these features, maintain backward compatibility with existing message protocol.
