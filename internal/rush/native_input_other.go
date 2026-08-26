//go:build (!linux && !darwin) || rush_wpe

package rush

import (
	"errors"
)

type unavailableNativeInput struct{}

func newNativeInput() (nativeInput, error) {
	return &unavailableNativeInput{}, nil
}

func (*unavailableNativeInput) Do(NativeInputRequest) error {
	return errors.New("trusted native input is not implemented on this platform")
}

func (*unavailableNativeInput) Close() error { return nil }
