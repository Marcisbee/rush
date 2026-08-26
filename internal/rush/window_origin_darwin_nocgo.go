//go:build darwin && !cgo

package rush

import (
	"errors"
	"unsafe"
)

func nativeContentOrigin(unsafe.Pointer) (float64, float64, error) {
	return 0, 0, errors.New("macOS trusted input requires cgo")
}
