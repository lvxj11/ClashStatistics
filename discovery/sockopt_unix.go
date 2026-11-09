//go:build !windows

package discovery

import "syscall"

// setDHCPSockOpts 在 unix 上给 DHCP 监听套接字设置 SO_REUSEADDR（与本机可能
// 存在的 DHCP 服务共存）和 SO_BROADCAST（接收广播）。
func setDHCPSockOpts(fd uintptr) error {
	if err := syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1); err != nil {
		return err
	}
	return syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_BROADCAST, 1)
}
