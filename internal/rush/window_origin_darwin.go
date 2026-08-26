//go:build darwin && cgo

package rush

import (
	"unsafe"

	"github.com/Marcisbee/rush/internal/wkwebview"
)

func nativeContentOrigin(window unsafe.Pointer) (float64, float64, error) {
	return wkwebview.ContentOrigin(window)
}
