//go:build !linux

package server

import (
	"errors"
	"os"
)

func createRawTUNFile(name string) (*os.File, error) {
	return nil, errors.New("TUN interface is only supported on Linux")
}
