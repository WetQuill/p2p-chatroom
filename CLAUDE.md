# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

A decentralized P2P chatroom application in Go. Each node runs as both a server and client, communicating via WebSockets. Nodes auto-discover each other on ports 1010-1100 and support both console-based and web-based interfaces.

## Build and Run

```bash
# Build executable
go build -o p2p-chatroom.exe .

# Or build with default name
go build

# Run directly
go run main.go
```

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

## Console Commands

- `exit`: Gracefully shutdown and send ExitMessage to all peers
- `checkconn`: Display current connection status (ID, port, username, remote address)

## Important Patterns

**Message Flow**:
1. Console input → `NewRegularMessage` → broadcast to all `PeersConn`
2. Incoming P2P message → `handleIncomingMessages` → switch on `MsgType` → update state/forward to UI
3. Web UI input → `uiHandler` → broadcast as `RegularMessage`

**Concurrency**: Both `AddressList.Mu` and `uiMu` mutexes protect shared state. Always lock before accessing `PeersConn`, `UserList`, or `uiConn`.

**Connection Cleanup**: Failed sends result in peer removal from `AddressList` via `DeleteAddress`.
