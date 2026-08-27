//go:build (!linux && !darwin) || rush_wpe || rush_obscura

package rush

import (
	"errors"
	"unsafe"
)

func nativeContentOrigin(unsafe.Pointer) (float64, float64, error) {
	return 0, 0, errors.New("trusted native input coordinates are not implemented on this platform")
}
