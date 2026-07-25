//go:build !windows

package voipinfra

import "syscall"

func syscallSetRcvBuf(fd, size int) error {
	return syscall.SetsockoptInt(fd, syscall.SOL_SOCKET, syscall.SO_RCVBUF, size)
}

func syscallGetRcvBuf(fd int) int {
	v, err := syscall.GetsockoptInt(fd, syscall.SOL_SOCKET, syscall.SO_RCVBUF)
	if err != nil {
		return 0
	}
	return v
}
