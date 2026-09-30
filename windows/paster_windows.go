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
// smugshot, and a line at the top of the screen says what will happen, as on the Mac.

var (
	hintArea rect // the top of the screen the pointer is on, in the drag layer's own coordinates
	hintFont uintptr
)

func togglePaste() {
	pasteThisShot = !pasteThisShot
	updatePasteHint(loadSettings())
	pInvalidateRect.Call(overlay, 0, 1)
}

// updatePasteHint works out the line for the current state. Called when the drag layer goes up and when the
// key switches it.
func updatePasteHint(cfg settings) {
	destination := ""
	if pasteThisShot {
		if hwnd, name := pasteDestination(cfg); hwnd != 0 {
			destination = name
		}
	}
	pasteHintText = pasteHint(pasteThisShot, cfg.pasteOn(), destination, hotkeyKey(cfg.Hotkey))
	var p point
	pGetCursorPos.Call(uintptr(unsafe.Pointer(&p)))
	mon := monitorRect(p)
	hintArea = rect{mon.Left - origin.X, mon.Top - origin.Y, mon.Right - origin.X, mon.Bottom - origin.Y}
}

// drawHint writes the line at the top middle of the screen the pointer is on, in the banner's font.
func drawHint(hdc uintptr) {
	dpi, _, _ := pGetDpiForWindow.Call(overlay)
	if dpi == 0 {
		dpi = 96
	}
	scale := func(v int) int { return v * int(dpi) / 96 }
	if hintFont == 0 {
		face, _ := syscall.UTF16PtrFromString("Segoe UI")
		hintFont, _, _ = pCreateFont.Call(uintptr(-scale(15)&0xFFFFFFFF), 0, 0, 0, 400, 0, 0, 0, 1, 0, 0, 5, 0, uintptr(unsafe.Pointer(face)))
	}
	pSetBkMode.Call(hdc, 1) // TRANSPARENT
	pSetTextColor.Call(hdc, 0xFFFFFF)
	old, _, _ := pSelectObject.Call(hdc, hintFont)
	text, _ := syscall.UTF16FromString(pasteHintText)
	area := hintArea
	area.Top += int32(scale(16))
	pDrawText.Call(hdc, uintptr(unsafe.Pointer(&text[0])), ^uintptr(0), uintptr(unsafe.Pointer(&area)), 0x1|0x20) // DT_CENTER, DT_SINGLELINE
	pSelectObject.Call(hdc, old)
}
