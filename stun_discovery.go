package main

import (
	"log"
	"net"
	"time"

	"github.com/pion/stun"
)

// DiscoverPublicIP finds the public IP using STUN
func discoverPublicIPImpl(stunServer string) (string, int, error) {
	// Remove "stun:" prefix if present
	serverAddr := stunServer
	if len(stunServer) > 5 && stunServer[:5] == "stun:" {
		serverAddr = stunServer[5:]
	}

	// Create a connection to the STUN server
	conn, err := net.DialTimeout("udp4", serverAddr, 5*time.Second)
	if err != nil {
		return "", 0, err
	}
	defer conn.Close()

	// Create STUN client with the connection
	c, err := stun.NewClient(conn)
	if err != nil {
		return "", 0, err
	}
	defer c.Close()

	// Build binding request
	message := stun.MustBuild(stun.TransactionID, stun.BindingRequest)

	var resultIP string
	var resultPort int
	var resultErr error

	// Send request and process response
	if err := c.Do(message, func(res stun.Event) {
		if res.Error != nil {
			resultErr = res.Error
			return
		}

		var xorAddr stun.XORMappedAddress
		if err := xorAddr.GetFrom(res.Message); err != nil {
			resultErr = err
			return
		}

		resultIP = xorAddr.IP.String()
		resultPort = xorAddr.Port
	}); err != nil {
		return "", 0, err
	}

	if resultErr != nil {
		return "", 0, resultErr
	}

	if resultIP == "" {
		return "", 0, stun.ErrAttributeNotFound
	}

	return resultIP, resultPort, nil
}

// TrySTUNServers attempts to discover public IP using multiple STUN servers
func TrySTUNServers(servers []string) (string, int, bool) {
	for _, server := range servers {
		ip, port, err := discoverPublicIPImpl(server)
		if err == nil {
			log.Printf("STUN discovery successful via %s: %s:%d", server, ip, port)
			return ip, port, true
		}
		log.Printf("STUN discovery failed via %s: %v", server, err)
	}
	return "", 0, false
}
