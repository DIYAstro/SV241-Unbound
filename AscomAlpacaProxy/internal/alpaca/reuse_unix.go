//go:build !windows

package alpaca

import (
	"syscall"

	"golang.org/x/sys/unix"
)

// setReuseSockopts sets SO_REUSEADDR and SO_REUSEPORT so the discovery port can be shared.
func setReuseSockopts(network, address string, c syscall.RawConn) error {
	var err error
	if cerr := c.Control(func(fd uintptr) {
		if err = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_REUSEADDR, 1); err == nil {
			err = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_REUSEPORT, 1)
		}
	}); cerr != nil {
		return cerr
	}
	return err
}
