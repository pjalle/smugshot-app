//go:build windows

// Smugshot for Windows: press Ctrl+Shift+1, drag, paste the path.
// Same gesture and the same files as the Mac version. Not yet on Windows: naming the
// control under the drag, reading its text, and the browser element.
package main

import (
	"encoding/json"
	"image"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"syscall"
	"time"
	"unsafe"
)

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	gdi32    = syscall.NewLazyDLL("gdi32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	shell32  = syscall.NewLazyDLL("shell32.dll")

	pRegisterClassEx     = user32.NewProc("RegisterClassExW")
	pCreateWindowEx      = user32.NewProc("CreateWindowExW")
	pDefWindowProc       = user32.NewProc("DefWindowProcW")
	pGetMessage          = user32.NewProc("GetMessageW")
	pTranslateMessage    = user32.NewProc("TranslateMessage")
	pDispatchMessage     = user32.NewProc("DispatchMessageW")
	pPostQuitMessage     = user32.NewProc("PostQuitMessage")
	pRegisterHotKey      = user32.NewProc("RegisterHotKey")
	pFindWindow          = user32.NewProc("FindWindowW")
	pPostMessage         = user32.NewProc("PostMessageW")
	pShowWindow          = user32.NewProc("ShowWindow")
	pSetWindowPos        = user32.NewProc("SetWindowPos")
	pSetForegroundWindow = user32.NewProc("SetForegroundWindow")
	pSetLayeredAttrs     = user32.NewProc("SetLayeredWindowAttributes")
	pInvalidateRect      = user32.NewProc("InvalidateRect")
	pBeginPaint          = user32.NewProc("BeginPaint")
	pEndPaint            = user32.NewProc("EndPaint")
	pFillRect            = user32.NewProc("FillRect")
	pFrameRect           = user32.NewProc("FrameRect")
	pGetSystemMetrics    = user32.NewProc("GetSystemMetrics")
	pGetDC               = user32.NewProc("GetDC")
	pReleaseDC           = user32.NewProc("ReleaseDC")
	pSetCapture          = user32.NewProc("SetCapture")
	pReleaseCapture      = user32.NewProc("ReleaseCapture")
	pLoadCursor          = user32.NewProc("LoadCursorW")
	pLoadIcon            = user32.NewProc("LoadIconW")
	pWindowFromPoint     = user32.NewProc("WindowFromPoint")
	pGetAncestor         = user32.NewProc("GetAncestor")
	pGetWindowText       = user32.NewProc("GetWindowTextW")
	pGetWindowThreadPID  = user32.NewProc("GetWindowThreadProcessId")
	pMonitorFromPoint    = user32.NewProc("MonitorFromPoint")
	pGetMonitorInfo      = user32.NewProc("GetMonitorInfoW")
	pOpenClipboard       = user32.NewProc("OpenClipboard")
	pEmptyClipboard      = user32.NewProc("EmptyClipboard")
	pSetClipboardData    = user32.NewProc("SetClipboardData")
	pCloseClipboard      = user32.NewProc("CloseClipboard")
	pMessageBeep         = user32.NewProc("MessageBeep")
	pCreatePopupMenu     = user32.NewProc("CreatePopupMenu")
	pAppendMenu          = user32.NewProc("AppendMenuW")
	pTrackPopupMenu      = user32.NewProc("TrackPopupMenu")
	pDestroyMenu         = user32.NewProc("DestroyMenu")
	pGetCursorPos        = user32.NewProc("GetCursorPos")
	pGetClientRect       = user32.NewProc("GetClientRect")
	pSetDpiAwareness     = user32.NewProc("SetProcessDpiAwarenessContext")

	pCreateCompatibleDC     = gdi32.NewProc("CreateCompatibleDC")
	pCreateCompatibleBitmap = gdi32.NewProc("CreateCompatibleBitmap")
	pSelectObject           = gdi32.NewProc("SelectObject")
	pBitBlt                 = gdi32.NewProc("BitBlt")
	pGetDIBits              = gdi32.NewProc("GetDIBits")
	pDeleteObject           = gdi32.NewProc("DeleteObject")
	pDeleteDC               = gdi32.NewProc("DeleteDC")
	pCreateSolidBrush       = gdi32.NewProc("CreateSolidBrush")
	pCreateBitmap           = gdi32.NewProc("CreateBitmap")
	pCreateIconIndirect     = user32.NewProc("CreateIconIndirect")
	pRegGetValue            = syscall.NewLazyDLL("advapi32.dll").NewProc("RegGetValueW")

	pGetModuleHandle    = kernel32.NewProc("GetModuleHandleW")
	pCreateMutex        = kernel32.NewProc("CreateMutexW")
	pGlobalAlloc        = kernel32.NewProc("GlobalAlloc")
	pGlobalLock         = kernel32.NewProc("GlobalLock")
	pGlobalUnlock       = kernel32.NewProc("GlobalUnlock")
	pOpenProcess        = kernel32.NewProc("OpenProcess")
	pCloseHandle        = kernel32.NewProc("CloseHandle")
	pQueryFullImageName = kernel32.NewProc("QueryFullProcessImageNameW")
	pShellNotifyIcon    = shell32.NewProc("Shell_NotifyIconW")
)

const (
	wmDestroy, wmPaint, wmKeyDown, wmHotKey, wmCommand                 = 0x0002, 0x000F, 0x0100, 0x0312, 0x0111
	wmLButtonDown, wmLButtonUp, wmMouseMove, wmRButtonDown             = 0x0201, 0x0202, 0x0200, 0x0204
	wmTray                                                             = 0x0400 + 1
	wmAlreadyRunning                                                   = 0x0400 + 2 // sent by a second copy before it quits
	wsPopup                                                            = 0x80000000
	wsExTopmost, wsExToolWindow, wsExLayered                           = 0x8, 0x80, 0x80000
	modNoRepeat                                                        = 0x4000
	vkEscape                                                           = 0x1B
	smXVirtual, smYVirtual, smCXVirtual, smCYVirtual                   = 76, 77, 78, 79
	srcCopy, captureBlt                                                = 0x00CC0020, 0x40000000
	cfUnicodeText, gmemMoveable                                        = 13, 0x2
	menuOpen, menuSettings, menuQuit, menuAgain, menuSound, menuBanner = 1, 2, 3, 4, 5, 6
	menuUpdates                                                        = 7
	menuRecent                                                         = 10 // + the index in the recent list
	mfSeparator, mfPopup, mfChecked, mfGrayed                          = 0x800, 0x10, 0x8, 0x1
)

type point struct{ X, Y int32 }
type rect struct{ Left, Top, Right, Bottom int32 }
type msg struct {
	Hwnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      point
}
type wndClassEx struct {
	Size       uint32
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   uintptr
	Icon       uintptr
	Cursor     uintptr
	Background uintptr
	MenuName   *uint16
	ClassName  *uint16
	IconSm     uintptr
}
type paintStruct struct {
	Hdc       uintptr
	Erase     int32
	RcPaint   rect
	Restore   int32
	IncUpdate int32
	Reserved  [32]byte
}
type bitmapInfoHeader struct {
	Size          uint32
	Width, Height int32
	Planes        uint16
	BitCount      uint16
	Compression   uint32
	SizeImage     uint32
	XPels, YPels  int32
	ClrUsed       uint32
	ClrImportant  uint32
}
type monitorInfo struct {
	Size    uint32
	Monitor rect
	Work    rect
	Flags   uint32
}
type notifyIconData struct {
	Size            uint32
	Hwnd            uintptr
	ID              uint32
	Flags           uint32
	CallbackMessage uint32
	Icon            uintptr
	Tip             [128]uint16
	State           uint32
	StateMask       uint32
	Info            [256]uint16
	Version         uint32
	InfoTitle       [64]uint16
	InfoFlags       uint32
	GuidItem        [16]byte
	BalloonIcon     uintptr
}

// One gesture in flight at a time.
var (
	overlay    uintptr
	active     bool
	dragging   bool
	start      point
	current    point
	origin     point // top-left of the whole desktop, which can be negative with several screens
	shot       *image.RGBA
	startedAt  time.Time
	redBrush   uintptr
	lastPath   string   // the shot.md most recently put on the clipboard
	recentDirs []string // as listed in the tray menu, for the menu ids
)

func main() {
	runtime.LockOSThread()
	// Coordinates in real pixels on every screen, whatever its scaling.
	pSetDpiAwareness.Call(^uintptr(3)) // DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2 = -4

	// One Smugshot at a time. A second copy asks the first to say "already running" and quits; ten copies would
	// fight over the hotkey and fill the tray.
	className, _ := syscall.UTF16PtrFromString("SmugshotWindow")
	mutexName, _ := syscall.UTF16PtrFromString("Local\\SmugshotSingleInstance")
	if _, _, err := pCreateMutex.Call(0, 0, uintptr(unsafe.Pointer(mutexName))); err == syscall.Errno(183) { // ERROR_ALREADY_EXISTS
		if first, _, _ := pFindWindow.Call(uintptr(unsafe.Pointer(className)), 0); first != 0 {
			pPostMessage.Call(first, wmAlreadyRunning, 0, 0)
		}
		return
	}

	os.MkdirAll(root(), 0o700)
	sweep()
	go func() { // short "keepFor" settings need the sweep to run on its own, not only after each shot
		for range time.Tick(30 * time.Second) {
			sweep()
		}
	}()

	instance, _, _ := pGetModuleHandle.Call(0)
	cross, _, _ := pLoadCursor.Call(0, 32515) // IDC_CROSS
	black, _, _ := pCreateSolidBrush.Call(0x000000)
	redBrush, _, _ = pCreateSolidBrush.Call(0x552DFF) // #FF2D55 as BGR
	class := wndClassEx{WndProc: syscall.NewCallback(wndProc), Instance: instance, Cursor: cross, Background: black, ClassName: className}
	class.Size = uint32(unsafe.Sizeof(class))
	pRegisterClassEx.Call(uintptr(unsafe.Pointer(&class)))

	overlay, _, _ = pCreateWindowEx.Call(wsExTopmost|wsExToolWindow|wsExLayered, uintptr(unsafe.Pointer(className)), 0, wsPopup,
		0, 0, 0, 0, 0, 0, instance, 0)
	pSetLayeredAttrs.Call(overlay, 0, 70, 0x2) // LWA_ALPHA: a faint dim while waiting for the drag
	makeBanner(instance)

	hotkey := loadSettings().Hotkey
	modifiers, key, _ := parseHotkey(hotkey) // loadSettings already fell back to the default if it did not parse
	if ok, _, _ := pRegisterHotKey.Call(overlay, 1, uintptr(modifiers|modNoRepeat), uintptr(key)); ok == 0 {
		pMessageBeep.Call(0x10) // the hotkey is taken by something else
	}
	addTrayIcon(hotkey)
	showRunning(false)

	var m msg
	for {
		r, _, _ := pGetMessage.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			break
		}
		pTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		pDispatchMessage.Call(uintptr(unsafe.Pointer(&m)))
	}
	removeTrayIcon()
}

func wndProc(hwnd uintptr, message uint32, wParam, lParam uintptr) uintptr {
	switch message {
	case wmHotKey:
		if active {
			cancel()
		} else {
			begin()
		}
		return 0
	case wmKeyDown:
		if wParam == vkEscape {
			cancel()
		}
		return 0
	case wmRButtonDown:
		cancel()
		return 0
	case wmLButtonDown:
		if active {
			dragging = true
			start = lParamPoint(lParam)
			current = start
			pSetCapture.Call(hwnd)
		}
		return 0
	case wmMouseMove:
		if dragging {
			current = lParamPoint(lParam)
			pInvalidateRect.Call(hwnd, 0, 1)
		}
		return 0
	case wmLButtonUp:
		if dragging {
			current = lParamPoint(lParam)
			pReleaseCapture.Call()
			finish()
		}
		return 0
	case wmPaint:
		var ps paintStruct
		hdc, _, _ := pBeginPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
		if dragging {
			r := selection()
			for i := int32(0); i < 3; i++ { // a three-pixel frame
				f := rect{r.Left - i, r.Top - i, r.Right + i, r.Bottom + i}
				pFrameRect.Call(hdc, uintptr(unsafe.Pointer(&f)), redBrush)
			}
		}
		pEndPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
		return 0
	case wmAlreadyRunning:
		showRunning(true)
		return 0
	case wmTray:
		if lParam == wmRButtonDown+1 || lParam == wmLButtonUp { // right-button up, or a left click
			showTrayMenu(hwnd)
		}
		return 0
	case wmCommand:
		switch wParam & 0xFFFF {
		case menuOpen:
			os.MkdirAll(root(), 0o700)
			exec.Command("explorer", root()).Start()
		case menuSettings:
			exec.Command("notepad", ensureSettingsFile()).Start()
		case menuUpdates:
			// Smugshot never goes online itself: the address is handed to the browser.
			exec.Command("rundll32", "url.dll,FileProtocolHandler", "https://smugshot.io").Start()
		case menuAgain:
			if lastPath != "" {
				setClipboard(*loadSettings().Prefix + lastPath)
			}
		case menuSound:
			toggleSetting("sound", loadSettings().soundOn())
		case menuBanner:
			toggleSetting("banner", loadSettings().bannerOn())
		default:
			if id := int(wParam&0xFFFF) - menuRecent; id >= 0 && id < len(recentDirs) {
				lastPath = filepath.Join(recentDirs[id], "shot.md")
				setClipboard(*loadSettings().Prefix + lastPath)
			}
		case menuQuit:
			pPostQuitMessage.Call(0)
		}
		return 0
	case wmDestroy:
		pPostQuitMessage.Call(0)
		return 0
	}
	r, _, _ := pDefWindowProc.Call(hwnd, uintptr(message), wParam, lParam)
	return r
}

func lParamPoint(l uintptr) point {
	return point{int32(int16(l & 0xFFFF)), int32(int16((l >> 16) & 0xFFFF))}
}

func selection() rect {
	r := rect{start.X, start.Y, current.X, current.Y}
	if r.Left > r.Right {
		r.Left, r.Right = r.Right, r.Left
	}
	if r.Top > r.Bottom {
		r.Top, r.Bottom = r.Bottom, r.Top
	}
	return r
}

// begin pictures the whole desktop first, then puts up the drag layer, so the layer is never in the picture.
func begin() {
	x, _, _ := pGetSystemMetrics.Call(smXVirtual)
	y, _, _ := pGetSystemMetrics.Call(smYVirtual)
	w, _, _ := pGetSystemMetrics.Call(smCXVirtual)
	h, _, _ := pGetSystemMetrics.Call(smCYVirtual)
	origin = point{int32(x), int32(y)}
	shot = captureDesktop(int32(x), int32(y), int32(w), int32(h))
	if shot == nil {
		pMessageBeep.Call(0x10)
		return
	}
	active, dragging, startedAt = true, false, time.Now()
	pSetWindowPos.Call(overlay, ^uintptr(0), uintptr(int32(x)), uintptr(int32(y)), w, h, 0x0040) // HWND_TOPMOST, SWP_SHOWWINDOW
	pSetForegroundWindow.Call(overlay)                                                           // so Esc reaches us
}

func cancel() {
	if dragging {
		pReleaseCapture.Call()
	}
	active, dragging, shot = false, false, nil
	pShowWindow.Call(overlay, 0)
}

func finish() {
	sel := selection()
	picture := shot
	at := startedAt
	cancel()
	if picture == nil || sel.Right-sel.Left < 4 || sel.Bottom-sel.Top < 4 {
		return
	}

	// The app and window under the middle of the drag, now that our layer is out of the way.
	centre := point{origin.X + (sel.Left+sel.Right)/2, origin.Y + (sel.Top+sel.Bottom)/2}
	app, title := windowAt(centre)

	// Only the screen the drag was on, like the Mac version.
	mon := monitorRect(centre)
	screenRect := image.Rect(int(mon.Left-origin.X), int(mon.Top-origin.Y), int(mon.Right-origin.X), int(mon.Bottom-origin.Y)).Intersect(picture.Bounds())
	screen := picture.SubImage(screenRect).(*image.RGBA)
	flat := image.NewRGBA(image.Rect(0, 0, screenRect.Dx(), screenRect.Dy()))
	for yy := 0; yy < screenRect.Dy(); yy++ {
		s := screen.PixOffset(screenRect.Min.X, screenRect.Min.Y+yy)
		copy(flat.Pix[flat.PixOffset(0, yy):], screen.Pix[s:s+screenRect.Dx()*4])
	}
	region := image.Rect(int(sel.Left), int(sel.Top), int(sel.Right), int(sel.Bottom)).Sub(screenRect.Min).Intersect(flat.Bounds())

	dir, err := newFolder(at)
	if err != nil {
		pMessageBeep.Call(0x10)
		return
	}
	fullImg, fullRegion := full(flat, region)
	if writePNG(fullImg, filepath.Join(dir, "full.png")) != nil || writePNG(crop(flat, region), filepath.Join(dir, "crop.png")) != nil {
		pMessageBeep.Call(0x10)
		return
	}
	path := filepath.Join(dir, "shot.md")
	os.WriteFile(path, []byte(shotText(dir, at, app, title, fullRegion, fullImg.Bounds().Size())), 0o600)
	// The text in front matters less on Windows (paths start with a drive letter), but agents key on it.
	cfg := loadSettings()
	setClipboard(*cfg.Prefix + path)
	lastPath = path
	if cfg.soundOn() {
		playTink()
	}
	if cfg.bannerOn() {
		showCopied()
	}
	sweep()
}

// toggleSetting flips one true/false key in the settings file, keeping the rest of the file as it is.
func toggleSetting(key string, current bool) {
	path := ensureSettingsFile()
	var raw map[string]any
	if json.Unmarshal(readFile(path), &raw) != nil || raw == nil {
		raw = map[string]any{}
	}
	raw[key] = !current
	if data, err := json.MarshalIndent(raw, "", "  "); err == nil {
		os.WriteFile(path, append(data, '\n'), 0o600)
	}
}

func captureDesktop(x, y, w, h int32) *image.RGBA {
	screenDC, _, _ := pGetDC.Call(0)
	defer pReleaseDC.Call(0, screenDC)
	memDC, _, _ := pCreateCompatibleDC.Call(screenDC)
	defer pDeleteDC.Call(memDC)
	bitmap, _, _ := pCreateCompatibleBitmap.Call(screenDC, uintptr(w), uintptr(h))
	defer pDeleteObject.Call(bitmap)
	old, _, _ := pSelectObject.Call(memDC, bitmap)
	ok, _, _ := pBitBlt.Call(memDC, 0, 0, uintptr(w), uintptr(h), screenDC, uintptr(x), uintptr(y), srcCopy|captureBlt)
	pSelectObject.Call(memDC, old)
	if ok == 0 {
		return nil
	}
	header := bitmapInfoHeader{Width: w, Height: -h, Planes: 1, BitCount: 32} // negative height: rows from the top
	header.Size = uint32(unsafe.Sizeof(header))
	img := image.NewRGBA(image.Rect(0, 0, int(w), int(h)))
	got, _, _ := pGetDIBits.Call(memDC, bitmap, 0, uintptr(h), uintptr(unsafe.Pointer(&img.Pix[0])), uintptr(unsafe.Pointer(&header)), 0)
	if got == 0 {
		return nil
	}
	for i := 0; i < len(img.Pix); i += 4 { // Windows hands back blue-green-red
		img.Pix[i], img.Pix[i+2], img.Pix[i+3] = img.Pix[i+2], img.Pix[i], 0xFF
	}
	return img
}

func monitorRect(p point) rect {
	mon, _, _ := pMonitorFromPoint.Call(uintptr(uint32(p.X))|uintptr(uint32(p.Y))<<32, 2) // MONITOR_DEFAULTTONEAREST
	info := monitorInfo{}
	info.Size = uint32(unsafe.Sizeof(info))
	pGetMonitorInfo.Call(mon, uintptr(unsafe.Pointer(&info)))
	return info.Monitor
}

func windowAt(p point) (app, title string) {
	hwnd, _, _ := pWindowFromPoint.Call(uintptr(uint32(p.X)) | uintptr(uint32(p.Y))<<32)
	if hwnd == 0 {
		return "", ""
	}
	if top, _, _ := pGetAncestor.Call(hwnd, 2); top != 0 { // GA_ROOT
		hwnd = top
	}
	buf := make([]uint16, 512)
	pGetWindowText.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	title = syscall.UTF16ToString(buf)

	var pid uint32
	pGetWindowThreadPID.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	if h, _, _ := pOpenProcess.Call(0x1000, 0, uintptr(pid)); h != 0 { // PROCESS_QUERY_LIMITED_INFORMATION
		defer pCloseHandle.Call(h)
		size := uint32(len(buf))
		if ok, _, _ := pQueryFullImageName.Call(h, 0, uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size))); ok != 0 {
			app = filepath.Base(syscall.UTF16ToString(buf[:size]))
		}
	}
	return app, title
}

func setClipboard(text string) {
	data, _ := syscall.UTF16FromString(text)
	if ok, _, _ := pOpenClipboard.Call(overlay); ok == 0 {
		return
	}
	defer pCloseClipboard.Call()
	pEmptyClipboard.Call()
	mem, _, _ := pGlobalAlloc.Call(gmemMoveable, uintptr(len(data)*2))
	if mem == 0 {
		return
	}
	ptr, _, _ := pGlobalLock.Call(mem)
	copy(unsafe.Slice((*uint16)(unsafe.Pointer(ptr)), len(data)), data)
	pGlobalUnlock.Call(mem)
	pSetClipboardData.Call(cfUnicodeText, mem) // the clipboard owns the memory from here
}

func trayData() *notifyIconData {
	d := &notifyIconData{Hwnd: overlay, ID: 1}
	d.Size = uint32(unsafe.Sizeof(*d))
	return d
}

func addTrayIcon(hotkey string) {
	d := trayData()
	d.Flags = 0x1 | 0x2 | 0x4 // message, icon, tip
	d.CallbackMessage = wmTray
	if trayIcon == 0 {
		trayIcon = makeTrayIcon()
	}
	d.Icon = trayIcon
	if d.Icon == 0 {
		d.Icon, _, _ = pLoadIcon.Call(0, 32512) // the stock application icon, if ours could not be made
	}
	tip, _ := syscall.UTF16FromString("Smugshot: press " + hotkey + ", drag, paste the path")
	copy(d.Tip[:], tip)
	pShellNotifyIcon.Call(0, uintptr(unsafe.Pointer(d)))
}

var trayIcon uintptr // kept for the life of the process; the tray only borrows it

type iconInfo struct {
	Icon  int32
	X, Y  uint32
	Mask  uintptr
	Color uintptr
}

// makeTrayIcon builds an icon from the embedded glyph, at the size the tray uses on this screen.
func makeTrayIcon() uintptr {
	size, _, _ := pGetSystemMetrics.Call(49) // SM_CXSMICON, already scaled for the screen
	if size == 0 {
		size = 16
	}
	return makeIcon(int(size), taskbarIsLight())
}

// makeIcon builds an icon from the embedded glyph at any size, black instead of white with dark set.
func makeIcon(size int, dark bool) uintptr {
	pix := iconPixels(size, dark)
	if pix == nil {
		return 0
	}
	color, _, _ := pCreateBitmap.Call(uintptr(size), uintptr(size), 1, 32, uintptr(unsafe.Pointer(&pix[0])))
	mask, _, _ := pCreateBitmap.Call(uintptr(size), uintptr(size), 1, 1, 0) // all clear; the alpha channel does the shaping
	defer pDeleteObject.Call(color)
	defer pDeleteObject.Call(mask)
	info := iconInfo{Icon: 1, Mask: mask, Color: color}
	icon, _, _ := pCreateIconIndirect.Call(uintptr(unsafe.Pointer(&info)))
	return icon
}

// taskbarIsLight reads the Windows theme setting the system's own tray glyphs follow: white glyphs on
// the default dark taskbar, black on a light one.
func taskbarIsLight() bool {
	key, _ := syscall.UTF16PtrFromString(`Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`)
	name, _ := syscall.UTF16PtrFromString("SystemUsesLightTheme")
	var value, length uint32 = 0, 4
	r, _, _ := pRegGetValue.Call(0x80000001, uintptr(unsafe.Pointer(key)), uintptr(unsafe.Pointer(name)), // HKEY_CURRENT_USER
		0x10, 0, uintptr(unsafe.Pointer(&value)), uintptr(unsafe.Pointer(&length))) // RRF_RT_REG_DWORD
	return r == 0 && value == 1
}

func removeTrayIcon() { pShellNotifyIcon.Call(2, uintptr(unsafe.Pointer(trayData()))) }

func showTrayMenu(hwnd uintptr) {
	menu, _, _ := pCreatePopupMenu.Call()
	item := func(flags uintptr, id uintptr, text string) {
		t, _ := syscall.UTF16PtrFromString(text)
		pAppendMenu.Call(menu, flags, id, uintptr(unsafe.Pointer(t)))
	}
	cfg := loadSettings()
	recentDirs = recent(cfg.Folder, 5)
	if lastPath == "" && len(recentDirs) > 0 {
		lastPath = filepath.Join(recentDirs[0], "shot.md")
	}
	again := uintptr(0)
	if lastPath == "" {
		again = mfGrayed
	}
	item(again, menuAgain, "Copy last smugshot again")
	if len(recentDirs) > 0 {
		sub, _, _ := pCreatePopupMenu.Call() // destroyed with its parent
		for i, dir := range recentDirs {
			t, _ := syscall.UTF16PtrFromString(summary(dir))
			pAppendMenu.Call(sub, 0, uintptr(menuRecent+i), uintptr(unsafe.Pointer(t)))
		}
		t, _ := syscall.UTF16PtrFromString("Recent smugshots")
		pAppendMenu.Call(menu, mfPopup, sub, uintptr(unsafe.Pointer(t)))
	}
	checked := func(on bool) uintptr {
		if on {
			return mfChecked
		}
		return 0
	}
	item(checked(cfg.soundOn()), menuSound, "Sound")
	item(checked(cfg.bannerOn()), menuBanner, "Banner")
	pAppendMenu.Call(menu, mfSeparator, 0, 0)
	item(0, menuSettings, "Settings…")
	item(0, menuOpen, "Open smugshots folder")
	item(0, menuUpdates, "Check for updates…")
	pAppendMenu.Call(menu, mfSeparator, 0, 0)
	item(0, menuQuit, "Quit Smugshot")
	var p point
	pGetCursorPos.Call(uintptr(unsafe.Pointer(&p)))
	pSetForegroundWindow.Call(hwnd) // or the menu will not close when clicking elsewhere
	pTrackPopupMenu.Call(menu, 0, uintptr(p.X), uintptr(p.Y), 0, hwnd, 0)
	pDestroyMenu.Call(menu)
}
