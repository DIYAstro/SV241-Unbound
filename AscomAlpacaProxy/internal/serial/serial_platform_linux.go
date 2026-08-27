//go:build linux

package serial

// maxConsecutiveReadFailures is how many commands in a row may fail to get a response before we
// give up on the connection, close the port and let the watchdog reconnect.
//
// A reconnect on Linux is now cheap in the common case - see ch340_linux.go, which talks to the
// CH340 chip directly over USB instead of going through the kernel's ch341 tty driver, and so
// normally reconnects without resetting the device at all. It still isn't completely free
// (closing and reclaiming the USB interface again costs a little time), so a few transient
// misses are still worth tolerating before tearing the connection down, same as before.
const maxConsecutiveReadFailures = 4
