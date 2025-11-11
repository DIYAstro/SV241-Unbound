package alpaca

import (
	"fmt"
	"net"
	"sv241pro-alpaca-proxy/internal/config"
	"sv241pro-alpaca-proxy/internal/logger"
)

// RespondToDiscovery listens for Alpaca discovery packets on UDP port 32227
// and responds with the server's listening port.
func RespondToDiscovery() {
	addr, err := net.ResolveUDPAddr("udp4", "0.0.0.0:32227")
	if err != nil {
		logger.Error("Discovery: Could not resolve UDP address: %v", err)
		return
	}

	conn, err := net.ListenUDP("udp4", addr)
	if err != nil {
		logger.Error("Discovery: Could not listen on UDP port 32227: %v", err)
		logger.Info("HINT: This may be caused by another Alpaca application running, or a permissions issue.")
		return
	}
	defer conn.Close()
	logger.Info("Alpaca discovery responder started on UDP port 32227.")

	discoveryMsg := []byte("alpacadiscovery1")
	buffer := make([]byte, 1024)

	for {
		n, remoteAddr, err := conn.ReadFromUDP(buffer)
		if err != nil {
			logger.Warn("Discovery: Error reading from UDP: %v", err)
			continue
		}

		if string(buffer[:n]) == string(discoveryMsg) {
			logger.Debug("Discovery: Request received from %s", remoteAddr)

			// Get the current network port from the config
			port := config.Get().NetworkPort
			response := fmt.Sprintf(`{"AlpacaPort": %d}`, port)

			_, err := conn.WriteToUDP([]byte(response), remoteAddr)
			if err != nil {
				logger.Error("Discovery: Failed to send response to %s: %v", remoteAddr, err)
			} else {
				logger.Debug("Discovery: Sent response '%s' to %s", response, remoteAddr)
			}
		}
	}
}
