//go:build !linux

package server

import (
	"errors"

	"golang.zx2c4.com/wireguard/device"
)

func startUserspaceWG(keys *wgKeys, wgPort int) (*device.Device, error) {
	return nil, errors.New("WireGuard userspace device is only supported on Linux")
}
