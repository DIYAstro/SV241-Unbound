//go:build windows

package alpaca

import "syscall"

// setReuseSockopts sets SO_REUSEADDR so the discovery port can be shared.
func setReuseSockopts(network, address string, c syscall.RawConn) error {
	var err error
	if cerr := c.Control(func(fd uintptr) {
		err = syscall.SetsockoptInt(syscall.Handle(fd), syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1)
	}); cerr != nil {
		return cerr
	}
	return err
}
