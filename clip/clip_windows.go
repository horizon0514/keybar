//go:build windows

package clip

import (
	"runtime"
	"syscall"
	"unsafe"
)

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")

	getSeq       = user32.NewProc("GetClipboardSequenceNumber")
	openClip     = user32.NewProc("OpenClipboard")
	closeClip    = user32.NewProc("CloseClipboard")
	getData      = user32.NewProc("GetClipboardData")
	available    = user32.NewProc("IsClipboardFormatAvailable")
	registerFmt  = user32.NewProc("RegisterClipboardFormatW")
	globalLock   = kernel32.NewProc("GlobalLock")
	globalUnlock = kernel32.NewProc("GlobalUnlock")
	globalSize   = kernel32.NewProc("GlobalSize")
)

const cfUnicodeText = 13

type win struct{ skipFmts []uintptr }

func system() (Board, bool, bool) {
	if user32.Load() != nil || getSeq.Find() != nil {
		return nil, false, false
	}
	w := win{}
	// What password managers set to keep clipboard history and monitors out.
	for _, name := range []string{"ExcludeClipboardContentFromMonitorProcessing", "Clipboard Viewer Ignore"} {
		p, _ := syscall.UTF16PtrFromString(name)
		if f, _, _ := registerFmt.Call(uintptr(unsafe.Pointer(p))); f != 0 {
			w.skipFmts = append(w.skipFmts, f)
		}
	}
	return w, false, true
}

func (win) Seq() (uint64, bool) {
	n, _, _ := getSeq.Call()
	return uint64(n), true
}

func (w win) Read() (string, bool) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	ok := false
	for range 5 { // another program may have it open a moment
		if r, _, _ := openClip.Call(0); r != 0 {
			ok = true
			break
		}
		syscallSleep()
	}
	if !ok {
		return "", false
	}
	defer closeClip.Call()
	for _, f := range w.skipFmts {
		if r, _, _ := available.Call(f); r != 0 {
			return "", true
		}
	}
	if r, _, _ := available.Call(cfUnicodeText); r == 0 {
		return "", false
	}
	h, _, _ := getData.Call(cfUnicodeText)
	if h == 0 {
		return "", false
	}
	size, _, _ := globalSize.Call(h)
	if size == 0 || size > 2*(Max+1) {
		return "", false
	}
	p, _, _ := globalLock.Call(h)
	if p == 0 {
		return "", false
	}
	defer globalUnlock.Call(h)
	u := unsafe.Slice(*(**uint16)(unsafe.Pointer(&p)), size/2)
	return syscall.UTF16ToString(u), false
}
