//go:build !linux

package serial

// maxConsecutiveReadFailures is how many commands in a row may fail to get a response before we
// give up on the connection, close the port and let the watchdog reconnect.
//
// 1 keeps the long-standing Windows behavior exactly as it was: disconnect as soon as a command
// exhausts its retries. Windows does not have the Linux reset-on-open problem that makes
// reconnecting expensive there (see serial_platform_linux.go), and this path has been reliable
// in practice, so it is left alone on purpose.
const maxConsecutiveReadFailures = 1
