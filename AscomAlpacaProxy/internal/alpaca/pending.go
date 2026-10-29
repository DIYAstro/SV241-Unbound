package alpaca

import (
	"sv241pro-alpaca-proxy/internal/config"
	"sv241pro-alpaca-proxy/internal/serial"
)

// maxDelayedActionMinutes is the longest delay the DelayedOn/DelayedOff actions accept: the
// firmware caps every delayed switch at 24 h (MAX_SWITCH_DELAY_S in config_manager.h) and rejects
// anything beyond it, so refusing it here gives the ASCOM client a proper error instead of a
// silently dropped command.
const maxDelayedActionMinutes = 24 * 60

// isPendingOff reports whether the status data shows a delayed switch-off still counting down for
// the switch with this short key (e.g. "d2"). The firmware lists such outputs under "pd" as
// [direction, seconds left] - direction 0 = off, 1 = on - and leaves the key out when nothing is
// pending. Pending switch-ons are deliberately not covered: the output is genuinely off until the
// delay elapses, and reporting "on" early would let a client connect a device that has no power yet.
func isPendingOff(data map[string]interface{}, shortKey string) bool {
	pd, ok := data["pd"].(map[string]interface{})
	if !ok {
		return false
	}
	entry, ok := pd[shortKey].([]interface{})
	if !ok || len(entry) == 0 {
		return false
	}
	direction, ok := entry[0].(float64)
	return ok && direction == 0
}

// pendingOffReportedAsOff is true when ASCOM clients should be told "off" for this switch even
// though the output is still powered: the switch has a delayed switch-off pending and the user
// enabled Config.ReportPendingOffAsOff. Off by default - the reported state is then always the
// actual one. The Web UI always shows the actual state (plus a countdown marker) and ignores this.
// The caller must hold serial.Status.RLock().
func pendingOffReportedAsOff(shortKey string) bool {
	return config.Get().ReportPendingOffAsOff && isPendingOff(serial.Status.Data, shortKey)
}
