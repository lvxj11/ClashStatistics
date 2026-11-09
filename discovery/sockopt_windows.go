//go:build windows

package discovery

import "syscall"

// setDHCPSockOpts 在 Windows 上给 DHCP 监听套接字设置 SO_REUSEADDR 和
// SO_BROADCAST。Windows 的 syscall.SetsockoptInt 第一个参数是 syscall.Handle
// 而非 int，因此单独实现，让交叉编译通过（行为与 unix 版一致）。
func setDHCPSockOpts(fd uintptr) error {
	if err := syscall.SetsockoptInt(syscall.Handle(fd), syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1); err != nil {
		return err
	}
	return syscall.SetsockoptInt(syscall.Handle(fd), syscall.SOL_SOCKET, syscall.SO_BROADCAST, 1)
}
