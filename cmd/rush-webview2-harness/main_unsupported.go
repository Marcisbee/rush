//go:build !windows

package main

import "fmt"

func main() { fmt.Println("rush-webview2-harness requires Windows and the WebView2 Runtime") }
