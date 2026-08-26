//go:build darwin && cgo

package rush

import (
	"fmt"

	"github.com/Marcisbee/rush/internal/wkwebview"
)

type macOSInput struct{}

func newNativeInput() (nativeInput, error) { return &macOSInput{}, nil }

func (*macOSInput) Do(request NativeInputRequest) error {
	switch request.Action {
	case "click":
		return wkwebview.TrustedClick(request.X, request.Y)
	case "type":
		if err := wkwebview.TrustedClick(request.X, request.Y); err != nil {
			return err
		}
		return wkwebview.TrustedType(request.Text)
	case "press":
		return wkwebview.TrustedPress(request.Key)
	default:
		return fmt.Errorf("unknown native input action %q", request.Action)
	}
}

func (*macOSInput) Close() error { return nil }
