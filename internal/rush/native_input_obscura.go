//go:build linux && rush_obscura

package rush

import "errors"

type obscuraNativeInput struct{}

func newNativeInput() (nativeInput, error) {
	return &obscuraNativeInput{}, errors.New("trusted native input is unavailable in Obscura no-render")
}

func (*obscuraNativeInput) Do(NativeInputRequest) error {
	return errors.New("trusted native input is unavailable in Obscura no-render")
}

func (*obscuraNativeInput) Close() error { return nil }
