//go:build windows

package main

import (
	_ "embed"
	"os"
	"path/filepath"
	"syscall"
	"unsafe"
)

// After a smugshot: a small dark banner at the bottom right, "Copied. Paste it into the chat.", which fades
// out after a moment, and a short sound. Each can be switched off in Settings or the tray menu.

var (
	winmm                 = syscall.NewLazyDLL("winmm.dll")
	pPlaySound            = winmm.NewProc("PlaySoundW")
	pSystemParametersInfo = user32.NewProc("SystemParametersInfoW")
	pSetTimer             = user32.NewProc("SetTimer")
	pKillTimer            = user32.NewProc("KillTimer")
	pDrawIconEx           = user32.NewProc("DrawIconEx")
	pDrawText             = user32.NewProc("DrawTextW")
	pGetDpiForWindow      = user32.NewProc("GetDpiForWindow")
	pCreateFont           = gdi32.NewProc("CreateFontW")
	pSetTextColor         = gdi32.NewProc("SetTextColor")
	pSetBkMode            = gdi32.NewProc("SetBkMode")
)

const (
	wmTimer          = 0x0113
	bannerHold, fade = 1, 2 // timer ids: how long it stays, then the fade
	copiedText       = "Copied. Paste it into the chat."
)

var (
	bannerText  = copiedText
	banner      uintptr
	bannerAlpha int
	bannerIcon  uintptr
	bannerFont  uintptr
	bannerBrush uintptr
)

// playSound plays the named sound: the bundled tink, or one of the sounds that ship with Windows. An unknown
// name, or a Windows sound that is not there, gives the tink; "off" gives nothing.
func playSound(name string) {
	if name == string(soundOff) {
		return
	}
	if snd, ok := soundByName(name); ok && snd.File != "" {
		path := filepath.Join(os.Getenv("WINDIR"), "Media", snd.File)
		if _, err := os.Stat(path); err == nil {
			file, _ := syscall.UTF16PtrFromString(path)
			pPlaySound.Call(uintptr(unsafe.Pointer(file)), 0, 0x20000|0x1|0x2) // SND_FILENAME, SND_ASYNC, SND_NODEFAULT
			return
		}
	}
	pPlaySound.Call(uintptr(unsafe.Pointer(&tinkWAV[0])), 0, 0x4|0x1|0x2) // SND_MEMORY, SND_ASYNC, SND_NODEFAULT
}

// makeBanner creates the banner window once, hidden. It is shown and hidden again by showBanner.
func makeBanner(instance uintptr) {
	className, _ := syscall.UTF16PtrFromString("SmugshotBanner")
	arrow, _, _ := pLoadCursor.Call(0, 32512) // IDC_ARROW
	class := wndClassEx{WndProc: syscall.NewCallback(bannerProc), Instance: instance, Cursor: arrow, ClassName: className}
	class.Size = uint32(unsafe.Sizeof(class))
	pRegisterClassEx.Call(uintptr(unsafe.Pointer(&class)))
	const wsExNoActivate = 0x08000000
	banner, _, _ = pCreateWindowEx.Call(wsExTopmost|wsExToolWindow|wsExLayered|wsExNoActivate,
		uintptr(unsafe.Pointer(className)), 0, wsPopup, 0, 0, 0, 0, 0, 0, instance, 0)
	bannerBrush, _, _ = pCreateSolidBrush.Call(0x1E1E1E)
}

// showBanner puts the banner above the taskbar, bottom right, and starts the clock. width is in 96-dpi
// pixels and hold in milliseconds: "Copied" is short and brief, the start-up message is longer and stays longer.
func showBanner(text string, width, hold int) {
	if banner == 0 {
		return
	}
	bannerText = text
	dpi, _, _ := pGetDpiForWindow.Call(banner)
	if dpi == 0 {
		dpi = 96
	}
	scale := func(v int) int { return v * int(dpi) / 96 }
	w, h, margin := scale(width), scale(48), scale(16)

	if bannerFont == 0 || bannerIcon == 0 {
		face, _ := syscall.UTF16PtrFromString("Segoe UI")
		bannerFont, _, _ = pCreateFont.Call(uintptr(-scale(15)&0xFFFFFFFF), 0, 0, 0, 400, 0, 0, 0, 1, 0, 0, 5, 0, uintptr(unsafe.Pointer(face)))
		bannerIcon = makeIcon(scale(22), false)
	}

	var work rect
	pSystemParametersInfo.Call(0x30, 0, uintptr(unsafe.Pointer(&work)), 0) // SPI_GETWORKAREA: the screen minus the taskbar
	x, y := int(work.Right)-w-margin, int(work.Bottom)-h-margin

	bannerAlpha = 255
	pKillTimer.Call(banner, bannerHold)
	pKillTimer.Call(banner, fade)
	pSetLayeredAttrs.Call(banner, 0, 255, 0x2)
	pSetWindowPos.Call(banner, ^uintptr(0), uintptr(x), uintptr(y), uintptr(w), uintptr(h), 0x0010|0x0040) // HWND_TOPMOST, SWP_NOACTIVATE, SWP_SHOWWINDOW
	pInvalidateRect.Call(banner, 0, 1)
	pSetTimer.Call(banner, bannerHold, uintptr(hold), 0)
}

func bannerProc(hwnd uintptr, message uint32, wParam, lParam uintptr) uintptr {
	switch message {
	case wmPaint:
		var ps paintStruct
		hdc, _, _ := pBeginPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
		dpi, _, _ := pGetDpiForWindow.Call(hwnd)
		if dpi == 0 {
			dpi = 96
		}
		scale := func(v int) int { return v * int(dpi) / 96 }
		var client rect
		pGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&client)))
		pFillRect.Call(hdc, uintptr(unsafe.Pointer(&client)), bannerBrush)
		accent := rect{0, 0, int32(scale(4)), client.Bottom}
		pFillRect.Call(hdc, uintptr(unsafe.Pointer(&accent)), redBrush)
		icon := scale(22)
		pDrawIconEx.Call(hdc, uintptr(scale(16)), uintptr((int(client.Bottom)-icon)/2), bannerIcon, uintptr(icon), uintptr(icon), 0, 0, 3) // DI_NORMAL
		pSetBkMode.Call(hdc, 1)                                                                                                            // TRANSPARENT
		pSetTextColor.Call(hdc, 0xFFFFFF)
		old, _, _ := pSelectObject.Call(hdc, bannerFont)
		text, _ := syscall.UTF16FromString(bannerText)
		area := rect{int32(scale(16 + 22 + 12)), 0, client.Right - int32(scale(12)), client.Bottom}
		pDrawText.Call(hdc, uintptr(unsafe.Pointer(&text[0])), ^uintptr(0), uintptr(unsafe.Pointer(&area)), 0x20|0x4|0x8000) // DT_SINGLELINE, DT_VCENTER, DT_END_ELLIPSIS
		pSelectObject.Call(hdc, old)
		pEndPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
		return 0
	case wmTimer:
		switch wParam {
		case bannerHold:
			pKillTimer.Call(hwnd, bannerHold)
			pSetTimer.Call(hwnd, fade, 16, 0)
		case fade:
			bannerAlpha -= 24
			if bannerAlpha <= 0 {
				pKillTimer.Call(hwnd, fade)
				pShowWindow.Call(hwnd, 0)
			} else {
				pSetLayeredAttrs.Call(hwnd, 0, uintptr(bannerAlpha), 0x2)
			}
		}
		return 0
	}
	r, _, _ := pDefWindowProc.Call(hwnd, uintptr(message), wParam, lParam)
	return r
}

// showCopied is the banner after a smugshot.
func showCopied() { showBanner(copiedText, 300, 1400) }

// showRunning says that Smugshot is there and how to use it. Without it, starting Smugshot shows nothing but a
// small icon in the tray (often hidden behind the ^), and people start it again and again.
func showRunning(already bool) {
	text := "Smugshot is running. Press " + hotkeyLabel(loadSettings().Hotkey) + ", then drag."
	if already {
		text = "Smugshot is already running. Press " + hotkeyLabel(loadSettings().Hotkey) + ", then drag."
	}
	showBanner(text, 440, 5000)
}
