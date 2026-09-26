//go:build darwin

package cmd

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework Cocoa
#include <stdbool.h>
#include <stdlib.h>
void aqShowPreferences(bool notify, bool notifyReset, const char *thresholds, int threshold, const char *pins, int pin, const char *intervals, int interval);
*/
import "C"

import (
	"strings"
	"sync/atomic"
	"unsafe"
)

const hasPrefsWindow = true

var prefsTarget atomic.Pointer[trayApp]

func (a *trayApp) openPreferences() {
	a.mu.Lock()
	v := a.prefsView()
	a.shownPrefs = v
	a.mu.Unlock()
	prefsTarget.Store(a)

	thresholds := C.CString(strings.Join(v.thresholds, "\n"))
	pins := C.CString(strings.Join(v.pins, "\n"))
	intervals := C.CString(strings.Join(v.intervals, "\n"))
	defer C.free(unsafe.Pointer(thresholds))
	defer C.free(unsafe.Pointer(pins))
	defer C.free(unsafe.Pointer(intervals))
	C.aqShowPreferences(C.bool(v.notify), C.bool(v.notifyReset),
		thresholds, C.int(v.threshold), pins, C.int(v.pin), intervals, C.int(v.interval))
}

//export aqPrefChanged
func aqPrefChanged(field, value C.int) {
	if a := prefsTarget.Load(); a != nil {
		go a.applyPref(prefsField(field), int(value))
	}
}
