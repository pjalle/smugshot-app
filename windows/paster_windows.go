//go:build windows

package main

import (
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

// "Paste it for me", like the Mac's Paster: after a smugshot, bring a program to the front and press Ctrl+V
// there, so the path lands in whatever has the cursor. It never presses Enter; the person still sends it.

var (
	pGetForegroundWindow = user32.NewProc("GetForegroundWindow")
	pIsWindow            = user32.NewProc("IsWindow")
	pIsWindowVisible     = user32.NewProc("IsWindowVisible")
	pIsIconic            = user32.NewProc("IsIconic")
	pEnumWindows         = user32.NewProc("EnumWindows")
	pSendInput           = user32.NewProc("SendInput")
	pMapVirtualKey       = user32.NewProc("MapVirtualKeyW")
	pGetCurrentProcessID = kernel32.NewProc("GetCurrentProcessId")
)

var (
	cameFrom      uintptr // the window in front when the shortcut was pressed: where "the app I came from" pastes
	pasteThisShot bool    // the setting, unless the shortcut's key switched it for this smugshot
	pasteKey      uint32  // the shortcut's key, which switches it while the drag layer is up
	pasteHintText string  // the line at the top of the drag layer, or ""
)

// rememberCameFrom notes the window in front before the drag layer takes the front. Our own windows (the
// Settings window) are not somewhere to paste.
func rememberCameFrom() {
	cameFrom, _, _ = pGetForegroundWindow.Call()
	var pid uint32
	pGetWindowThreadPID.Call(cameFrom, uintptr(unsafe.Pointer(&pid)))
	if self, _, _ := pGetCurrentProcessID.Call(); uintptr(pid) == self {
		cameFrom = 0
	}
}

// pasteDestination is the window this smugshot would be pasted into and the name to show for it, or 0 when
// there is none. With the setting off, the shortcut's key pastes into the app you came from, as on the Mac.
func pasteDestination(cfg settings) (hwnd uintptr, name string) {
	if !cfg.pasteOn() || cfg.PasteInto == pastePrevious {
		if cameFrom == 0 {
			return 0, ""
		}
		if ok, _, _ := pIsWindow.Call(cameFrom); ok == 0 {
			return 0, ""
		}
		if app := processName(cameFrom); app != "" {
			return cameFrom, programTitle(app)
		}
		return cameFrom, "the app you came from"
	}
	return findProgramWindow(cfg.PasteInto), programTitle(cfg.PasteInto)
}

// findProgramWindow returns the first visible window with a title that belongs to the program with this
// file name, like WindowsTerminal.exe, or 0. EnumWindows goes front to back, so it is the one used last.
func findProgramWindow(file string) uintptr {
	if enumCallback == 0 { // made once: Windows keeps only so many callbacks for a process
		enumCallback = syscall.NewCallback(func(hwnd, _ uintptr) uintptr {
			if visible, _, _ := pIsWindowVisible.Call(hwnd); visible == 0 {
				return 1
			}
			if n, _, _ := pGetWindowTextLen.Call(hwnd); n == 0 {
				return 1
			}
			if strings.EqualFold(processName(hwnd), enumWanted) {
				enumFound = hwnd
				return 0 // stop
			}
			return 1
		})
	}
	enumWanted, enumFound = file, 0
	pEnumWindows.Call(enumCallback, 0)
	return enumFound
}

var (
	enumCallback uintptr
	enumWanted   string
	enumFound    uintptr
)

// processName is the file name of the program that owns the window, like Code.exe, or "" when Windows will
// not say (a program running as administrator, for one).
func processName(hwnd uintptr) string {
	var pid uint32
	pGetWindowThreadPID.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	h, _, _ := pOpenProcess.Call(0x1000, 0, uintptr(pid)) // PROCESS_QUERY_LIMITED_INFORMATION
	if h == 0 {
		return ""
	}
	defer pCloseHandle.Call(h)
	buf := make([]uint16, 512)
	size := uint32(len(buf))
	if ok, _, _ := pQueryFullImageName.Call(h, 0, uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size))); ok == 0 {
		return ""
	}
	return filepath.Base(syscall.UTF16ToString(buf[:size]))
}

type keyboardInput struct {
	Vk, Scan    uint16
	Flags, Time uint32
	ExtraInfo   uintptr
}

// input is Windows' INPUT with the keyboard member of its union. The padding makes it as large as the
// largest member, the mouse one, which SendInput checks against the size it is given.
type input struct {
	Type uint32
	Ki   keyboardInput
	_    [8]byte
}

// pasteInto brings the window to the front, waits until it is there, and presses Ctrl+V. False when it never
// came to the front; the path is on the clipboard all the same, and the person pastes it by hand.
func pasteInto(hwnd uintptr) bool {
	if iconic, _, _ := pIsIconic.Call(hwnd); iconic != 0 {
		pShowWindow.Call(hwnd, 9) // SW_RESTORE
	}
	// Windows lets a program hand the front to another only when it had the last input. That is ours: the
	// mouse-up on the drag layer, unless the person has already clicked elsewhere.
	pSetForegroundWindow.Call(hwnd)
	front := false
	for i := 0; i < 30 && !front; i++ { // up to a second and a half
		fg, _, _ := pGetForegroundWindow.Call()
		if front = fg == hwnd; !front {
			time.Sleep(50 * time.Millisecond)
		}
	}
	if !front {
		return false
	}
	// Being in front is not quite enough: a key press that lands while the window is still being switched
	// to is lost. The Mac waits a little longer for the same reason.
	time.Sleep(150 * time.Millisecond)
	key := func(vk uint16, up bool) input {
		// The scan code too, from MAPVK_VK_TO_VSC: some programs read that rather than the key code.
		scan, _, _ := pMapVirtualKey.Call(uintptr(vk), 0)
		in := input{Type: 1, Ki: keyboardInput{Vk: vk, Scan: uint16(scan)}} // INPUT_KEYBOARD
		if up {
			in.Ki.Flags = 0x2 // KEYEVENTF_KEYUP
		}
		return in
	}
	const vkControl, vkV = 0x11, 0x56
	keys := []input{key(vkControl, false), key(vkV, false), key(vkV, true), key(vkControl, true)}
	sent, _, _ := pSendInput.Call(uintptr(len(keys)), uintptr(unsafe.Pointer(&keys[0])), unsafe.Sizeof(keys[0]))
	return int(sent) == len(keys)
}

// The per-shot switch: while the drag layer is up, the shortcut's key on its own switches pasting for this
// smugshot, and a line at the top of the screen says what will happen, as on the Mac. The line is its own
// small window, opaque, above the drag layer: drawn on the layer itself it would take the layer's dimness
// and be hard to read. Mouse events pass through it to the layer, so a drag can start anywhere.

var (
	hint      uintptr // the window with the line, made once, shown while the drag layer is up
	hintFont  uintptr
	hintBrush uintptr
)

func togglePaste() {
	pasteThisShot = !pasteThisShot
	updatePasteHint(loadSettings())
}

// updatePasteHint works out the line for the current state and shows it at the top of the screen the pointer
// is on, or hides it when there is nothing to say. Called when the drag layer goes up and when the key
// switches pasting.
func updatePasteHint(cfg settings) {
	destination := ""
	if pasteThisShot {
		if hwnd, name := pasteDestination(cfg); hwnd != 0 {
			destination = name
		}
	}
	pasteHintText = pasteHint(pasteThisShot, cfg.pasteOn(), destination, hotkeyKey(cfg.Hotkey))
	if pasteHintText == "" {
		hidePasteHint()
		return
	}
	if hint == 0 {
		makeHint()
	}
	dpi, _, _ := pGetDpiForWindow.Call(hint)
	if dpi == 0 {
		dpi = 96
	}
	scale := func(v int) int { return v * int(dpi) / 96 }
	var p point
	pGetCursorPos.Call(uintptr(unsafe.Pointer(&p)))
	mon := monitorRect(p)
	w, h := scale(560), scale(36)
	x, y := int(mon.Left)+(int(mon.Right-mon.Left)-w)/2, int(mon.Top)+scale(12)
	pSetWindowPos.Call(hint, ^uintptr(0), uintptr(x), uintptr(y), uintptr(w), uintptr(h), 0x0010|0x0040) // HWND_TOPMOST, SWP_NOACTIVATE, SWP_SHOWWINDOW
	pInvalidateRect.Call(hint, 0, 1)
}

func hidePasteHint() {
	if hint != 0 {
		pShowWindow.Call(hint, 0)
	}
}

func makeHint() {
	instance, _, _ := pGetModuleHandle.Call(0)
	className, _ := syscall.UTF16PtrFromString("SmugshotHint")
	arrow, _, _ := pLoadCursor.Call(0, 32512) // IDC_ARROW
	class := wndClassEx{WndProc: syscall.NewCallback(hintProc), Instance: instance, Cursor: arrow, ClassName: className}
	class.Size = uint32(unsafe.Sizeof(class))
	pRegisterClassEx.Call(uintptr(unsafe.Pointer(&class)))
	const wsExNoActivate, wsExTransparent = 0x08000000, 0x20 // never takes the keyboard from the drag layer; clicks fall through
	hint, _, _ = pCreateWindowEx.Call(wsExTopmost|wsExToolWindow|wsExNoActivate|wsExTransparent,
		uintptr(unsafe.Pointer(className)), 0, wsPopup, 0, 0, 0, 0, 0, 0, instance, 0)
	hintBrush, _, _ = pCreateSolidBrush.Call(0x1E1E1E)
}

func hintProc(hwnd uintptr, message uint32, wParam, lParam uintptr) uintptr {
	if message == wmPaint {
		var ps paintStruct
		hdc, _, _ := pBeginPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
		dpi, _, _ := pGetDpiForWindow.Call(hwnd)
		if dpi == 0 {
			dpi = 96
		}
		scale := func(v int) int { return v * int(dpi) / 96 }
		if hintFont == 0 {
			face, _ := syscall.UTF16PtrFromString("Segoe UI")
			hintFont, _, _ = pCreateFont.Call(uintptr(-scale(15)&0xFFFFFFFF), 0, 0, 0, 400, 0, 0, 0, 1, 0, 0, 5, 0, uintptr(unsafe.Pointer(face)))
		}
		var client rect
		pGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&client)))
		pFillRect.Call(hdc, uintptr(unsafe.Pointer(&client)), hintBrush)
		accent := rect{0, 0, int32(scale(4)), client.Bottom}
		pFillRect.Call(hdc, uintptr(unsafe.Pointer(&accent)), redBrush)
		pSetBkMode.Call(hdc, 1) // TRANSPARENT
		pSetTextColor.Call(hdc, 0xFFFFFF)
		old, _, _ := pSelectObject.Call(hdc, hintFont)
		text, _ := syscall.UTF16FromString(pasteHintText)
		area := rect{int32(scale(12)), 0, client.Right - int32(scale(12)), client.Bottom}
		pDrawText.Call(hdc, uintptr(unsafe.Pointer(&text[0])), ^uintptr(0), uintptr(unsafe.Pointer(&area)), 0x1|0x20|0x4|0x8000) // DT_CENTER, DT_SINGLELINE, DT_VCENTER, DT_END_ELLIPSIS
		pSelectObject.Call(hdc, old)
		pEndPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
		return 0
	}
	r, _, _ := pDefWindowProc.Call(hwnd, uintptr(message), wParam, lParam)
	return r
}
