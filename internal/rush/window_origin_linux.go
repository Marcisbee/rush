//go:build linux && !rush_wpe

package rush

import (
	"errors"
	"fmt"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
)

var gtkGeometry struct {
	once sync.Once
	err  error
	gtk  uintptr
	gdk  uintptr

	binChild             func(uintptr) uintptr
	widgetWindow         func(uintptr) uintptr
	translateCoordinates func(uintptr, uintptr, int32, int32, *int32, *int32) int32
	windowOrigin         func(uintptr, *int32, *int32) int32
}

func nativeContentOrigin(window unsafe.Pointer) (float64, float64, error) {
	gtkGeometry.once.Do(loadGTKGeometry)
	if gtkGeometry.err != nil {
		return 0, 0, gtkGeometry.err
	}
	widget := uintptr(window)
	if widget == 0 {
		return 0, 0, errors.New("trusted native input has no GTK window")
	}
	gdkWindow := gtkGeometry.widgetWindow(widget)
	if gdkWindow == 0 {
		return 0, 0, errors.New("trusted native input GTK window is not realized")
	}
	var rootX, rootY int32
	if gtkGeometry.windowOrigin(gdkWindow, &rootX, &rootY) == 0 {
		return 0, 0, errors.New("resolve trusted input window origin")
	}
	child := gtkGeometry.binChild(widget)
	if child == 0 {
		return float64(rootX), float64(rootY), nil
	}
	var childX, childY int32
	if gtkGeometry.translateCoordinates(child, widget, 0, 0, &childX, &childY) == 0 {
		return 0, 0, errors.New("resolve trusted input WebView origin")
	}
	return float64(rootX + childX), float64(rootY + childY), nil
}

func loadGTKGeometry() {
	gtk, err := purego.Dlopen("libgtk-3.so.0", purego.RTLD_NOW|purego.RTLD_LOCAL)
	if err != nil {
		gtkGeometry.err = fmt.Errorf("load GTK for trusted input coordinates: %w", err)
		return
	}
	gdk, err := purego.Dlopen("libgdk-3.so.0", purego.RTLD_NOW|purego.RTLD_LOCAL)
	if err != nil {
		_ = purego.Dlclose(gtk)
		gtkGeometry.err = fmt.Errorf("load GDK for trusted input coordinates: %w", err)
		return
	}
	gtkGeometry.gtk = gtk
	gtkGeometry.gdk = gdk
	purego.RegisterLibFunc(&gtkGeometry.binChild, gtk, "gtk_bin_get_child")
	purego.RegisterLibFunc(&gtkGeometry.widgetWindow, gtk, "gtk_widget_get_window")
	purego.RegisterLibFunc(&gtkGeometry.translateCoordinates, gtk, "gtk_widget_translate_coordinates")
	purego.RegisterLibFunc(&gtkGeometry.windowOrigin, gdk, "gdk_window_get_origin")
}
