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

	// CurrentLimitStatusChan broadcasts edge transitions of the box-wide heater current-limit
	// ramp (see config.current_limit_enabled/_amps, dew_control.cpp's is_current_limit_active()).
	// The serial manager's periodic cache updater writes to this only on change, not on every
	// poll tick - same "edge, not level" contract as ComPortStatusChan.
	CurrentLimitStatusChan = make(chan bool, 1)

	// once is used to ensure the ComPortStatusChan listener is only started once.
	once sync.Once

	// currentLimitOnce is the CurrentLimitStatusChan equivalent of once above - kept separate
	// so starting one listener doesn't swallow the other (sync.Once.Do only ever runs the first
	// function it's given).
	currentLimitOnce sync.Once
)

// StartListener ensures that any component that needs to react to ComPortStatusChan events can
// do so. It is designed to be called multiple times safely, but the listener function will only
// be executed once.
func StartListener(listener func()) {
	once.Do(listener)
}

// StartCurrentLimitListener is StartListener's equivalent for CurrentLimitStatusChan.
func StartCurrentLimitListener(listener func()) {
	currentLimitOnce.Do(listener)
}
