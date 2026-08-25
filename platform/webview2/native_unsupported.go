//go:build !windows

package webview2

import (
	"context"
	"encoding/json"
	"errors"
)

var ErrWindowsRequired = errors.New("webview2: Windows with the Microsoft Edge WebView2 Runtime is required")

type unsupportedDriver struct{}

func newPlatformDriver() nativeDriver { return &unsupportedDriver{} }
func (*unsupportedDriver) start(context.Context, Config, string, func(json.RawMessage)) error {
	return ErrWindowsRequired
}
func (*unsupportedDriver) navigate(context.Context, string) error { return ErrWindowsRequired }
func (*unsupportedDriver) evaluate(context.Context, string) (json.RawMessage, error) {
	return nil, ErrWindowsRequired
}
func (*unsupportedDriver) capturePNG(context.Context) ([]byte, error)      { return nil, ErrWindowsRequired }
func (*unsupportedDriver) trustedMouse(context.Context, MouseAction) error { return ErrWindowsRequired }
func (*unsupportedDriver) trustedKey(context.Context, KeyAction) error     { return ErrWindowsRequired }
func (*unsupportedDriver) close() error                                    { return nil }
