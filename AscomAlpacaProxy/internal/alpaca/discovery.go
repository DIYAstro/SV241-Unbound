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
	if !config.Get().EnableAlpacaDiscovery {
		logger.Info("Alpaca Discovery Responder disabled via config.")
		return
	}

	listenAddr := config.Get().ListenAddress
	udpAddress := fmt.Sprintf("%s:32227", listenAddr)

	addr, err := net.ResolveUDPAddr("udp4", udpAddress)
	if err != nil {
		logger.Error("Discovery: Could not resolve UDP address '%s': %v", udpAddress, err)
		return
	}

	conn, err := net.ListenUDP("udp4", addr)
	if err != nil {
		logger.Error("Discovery: Could not listen on UDP address '%s': %v", udpAddress, err)
		logger.Info("HINT: This may be caused by another Alpaca application running, or a permissions issue.")
		return
	}
	defer conn.Close()
	logger.Info("Alpaca discovery responder started on UDP address '%s'.", udpAddress)

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
