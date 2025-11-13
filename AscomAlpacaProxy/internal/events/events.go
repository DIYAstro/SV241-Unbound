package events

import "sync"

// ComPortStatus represents the connection status of the COM port.
type ComPortStatus bool

const (
	// Connected indicates the COM port is connected.
	Connected ComPortStatus = true
	// Disconnected indicates the COM port is disconnected.
	Disconnected ComPortStatus = false
)

var (
	// ComPortStatusChan is a channel that broadcasts the connection status of the COM port.
	// The serial manager will write to this channel, and other parts of the application (like systray) can listen to it.
	ComPortStatusChan = make(chan ComPortStatus, 1)

	// once is used to ensure the listener is only started once.
	once sync.Once
)

// StartListener ensures that any component that needs to react to events can do so.
// It is designed to be called multiple times safely, but the listener function will only be executed once.
func StartListener(listener func()) {
	once.Do(listener)
}
