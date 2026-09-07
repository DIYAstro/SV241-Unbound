package events

// ComPortStatus represents the connection status of the COM port.
type ComPortStatus bool

const (
	// Connected indicates the COM port is connected.
	Connected ComPortStatus = true
	// Disconnected indicates the COM port is disconnected.
	Disconnected ComPortStatus = false
)
