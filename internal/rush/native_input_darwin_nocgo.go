//go:build darwin && !cgo

package rush

import "errors"

type unavailableDarwinInput struct{}

func newNativeInput() (nativeInput, error) {
	return &unavailableDarwinInput{}, errors.New("macOS trusted input requires cgo")
}
func (*unavailableDarwinInput) Do(NativeInputRequest) error {
	return errors.New("macOS trusted input requires cgo")
}
func (*unavailableDarwinInput) Close() error { return nil }
