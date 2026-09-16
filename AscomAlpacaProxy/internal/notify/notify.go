// Package notify is a generic, platform-neutral fan-out point for user-facing alerts (a heater
// current-limit engaging/clearing, a low/critical voltage crossing, a serial connection drop -
// and, later, whatever new alert sources get added). Every delivery channel (the Windows systray
// toast today; email/Pushover/etc. later, whenever such a channel gets added) registers itself
// once via Register; every alert source calls Dispatch with a ready-made Title/Message, never
// knowing or caring which channels - if any - are currently listening.
//
// Deliberately not a shared channel with multiple goroutines racing to `range` over it: a plain
// Go channel delivers each value to exactly one receiver, never all of them, so "fan out to
// every registered channel" needs an explicit registry instead.
package notify

import "sync"

// Notification is a generic alert to be delivered through every currently-registered channel.
type Notification struct {
	Title   string
	Message string

	// Severity is an optional hint for channels that can act on urgency differently (e.g. a
	// future Pushover channel mapping this to its own priority levels) - "critical", "warning",
	// or "info". "" (the zero value) means "no particular severity" and is exactly what every
	// existing call site already produces, so adding this field changes no current behavior.
	Severity string
}

var (
	mu        sync.RWMutex
	receivers []func(Notification)
)

// Register adds a delivery channel that every future Dispatch() call fans out to. Safe to call
// multiple times (e.g. once per channel, at that channel's own startup) - each registered
// receiver is independent.
func Register(deliver func(Notification)) {
	mu.Lock()
	defer mu.Unlock()
	receivers = append(receivers, deliver)
}

// Dispatch fans a notification out to every registered channel, each in its own goroutine - a
// slow or blocking channel (e.g. a future network call to an email/Pushover API) never delays
// the others or the caller. A no-op if nothing is registered (e.g. on a platform/build with no
// delivery channel available at all).
func Dispatch(n Notification) {
	mu.RLock()
	defer mu.RUnlock()
	for _, deliver := range receivers {
		go deliver(n)
	}
}
