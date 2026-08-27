//go:build linux

package serial

// maxConsecutiveReadFailures is how many commands in a row may fail to get a response before we
// give up on the connection, close the port and let the watchdog reconnect.
//
// On Linux this is deliberately > 1, because closing and reopening the port RESETS the device.
// That is not something this code can avoid: Linux's tty layer asserts DTR and RTS itself on
// every open (tty_port_raise_dtr_rts), and on this hardware the ESP32 is held in reset for as
// long as both lines are asserted - it answers nothing at all until they are cleared again, and
// clearing them is exactly what makes it boot. Verified on a Raspberry Pi against the real box:
// TIOCMGET reports DTR=1 RTS=1 immediately after open with no userspace code having touched
// them, the device stays silent, and the FreeRTOS boot log appears the moment both are cleared.
// A port that is simply held open, by contrast, keeps answering indefinitely with no further
// resets.
//
// So every reconnect costs one device reset - and a reset drops all outputs back to their
// configured startup state, which is dangerous if the user already switched things on. Tearing
// the connection down over a single missed response therefore does far more harm than the
// occasional lost command it was meant to recover from; we only do it once the link really does
// look dead.
const maxConsecutiveReadFailures = 4
