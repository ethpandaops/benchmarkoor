//go:build !linux

package blockdev

import "os"

// supported is false: only Linux has sysfs and O_DIRECT.
const supported = false

func statDevice(string) (uint32, uint32, error) {
	return 0, 0, ErrUnsupported
}

func statBlockNode(string) (uint32, uint32, error) {
	return 0, 0, ErrUnsupported
}

func openDirect(string) (*os.File, error) {
	return nil, ErrUnsupported
}

func freeBytes(string) (uint64, error) {
	return 0, ErrUnsupported
}
