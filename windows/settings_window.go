//go:build windows

package main

import (
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"
)

// The Settings window: "Settings…" in the tray menu. Plain Windows controls, one per setting, in the order
// the Mac has them. Every change is saved as it is made, into the same settings.json as before, so there is
// no Save button; the line at the bottom says so, or says what is wrong with a shortcut. Esc or the close
// button closes it. Only one at a time; opening it again brings it to the front.

var (
	pDestroyWindow     = user32.NewProc("DestroyWindow")
	pSetWindowText     = user32.NewProc("SetWindowTextW")
	pGetWindowTextLen  = user32.NewProc("GetWindowTextLengthW")
	pSendMessage       = user32.NewProc("SendMessageW")
	pGetSysColorBrush  = user32.NewProc("GetSysColorBrush")
	pGetSysColor       = user32.NewProc("GetSysColor")
	pSetFocus          = user32.NewProc("SetFocus")
	pGetDpiForSystem   = user32.NewProc("GetDpiForSystem")
	pAdjustWindowRect  = user32.NewProc("AdjustWindowRectExForDpi")
	pSHBrowseForFolder = shell32.NewProc("SHBrowseForFolderW")
	pSHGetPathFromIDL  = shell32.NewProc("SHGetPathFromIDListW")
	ole32              = syscall.NewLazyDLL("ole32.dll")
	pCoInitializeEx    = ole32.NewProc("CoInitializeEx")
	pCoTaskMemFree     = ole32.NewProc("CoTaskMemFree")
)

const (
	wmClose, wmSetFont, wmCtlColorStatic  = 0x0010, 0x0030, 0x0138
	wsChild, wsVisible, wsTabStop         = 0x40000000, 0x10000000, 0x10000
	wsBorder, wsVScroll                   = 0x800000, 0x200000
	wsCaption, wsSysMenu                  = 0xC00000, 0x80000
	esAutoHScroll                         = 0x80
	cbsDropDownList                       = 0x3
	bsAutoCheckBox                        = 0x3
	cbAddString, cbGetCurSel, cbSetCurSel = 0x143, 0x147, 0x14E
	bmGetCheck, bmSetCheck                = 0xF0, 0xF1
	cbnSelChange, enChange, enKillFocus   = 1, 0x300, 0x200
	idCancel                              = 2 // what IsDialogMessage sends for Esc

	// Control ids. Not 1 or 2: IsDialogMessage uses those for Enter and Esc.
	idShortcut, idShortcutDefault, idFolder, idFolderChoose, idFolderDefault = 100, 101, 102, 103, 104
	idKeepFor, idPrefix, idSound, idBanner, idHint                           = 105, 106, 107, 108, 109
)

var (
	settingsWindow  uintptr
	settingsFont    uintptr
	settingsHint    uintptr
	settingsFields  = map[int]uintptr{}
	settingsLoading bool // while the fields are being filled, their change messages are not settings changes
	hintDefault     = "Changes are saved as you make them."
)

type browseInfo struct {
	Owner       uintptr
	Root        uintptr
	DisplayName *uint16
	Title       *uint16
	Flags       uint32
	Callback    uintptr
	Param       uintptr
	Image       int32
}

func openSettingsWindow() {
	if settingsWindow != 0 {
		pShowWindow.Call(settingsWindow, 9) // SW_RESTORE
		pSetForegroundWindow.Call(settingsWindow)
		return
	}
	instance, _, _ := pGetModuleHandle.Call(0)
	className, _ := syscall.UTF16PtrFromString("SmugshotSettings")
	arrow, _, _ := pLoadCursor.Call(0, 32512)                                                                                                   // IDC_ARROW
	class := wndClassEx{WndProc: syscall.NewCallback(settingsProc), Instance: instance, Cursor: arrow, Background: 5 + 1, ClassName: className} // COLOR_WINDOW + 1
	class.Size = uint32(unsafe.Sizeof(class))
	if trayIcon == 0 {
		trayIcon = makeTrayIcon()
	}
	class.Icon = makeIcon(32, true)
	pRegisterClassEx.Call(uintptr(unsafe.Pointer(&class))) // a second registration fails harmlessly

	dpi, _, _ := pGetDpiForSystem.Call()
	if dpi == 0 {
		dpi = 96
	}
	scale := func(v int) int { return v * int(dpi) / 96 }
	const clientW, clientH = 500, 250
	frame := rect{0, 0, int32(scale(clientW)), int32(scale(clientH))}
	pAdjustWindowRect.Call(uintptr(unsafe.Pointer(&frame)), wsCaption|wsSysMenu, 0, 0, dpi)
	w, h := int(frame.Right-frame.Left), int(frame.Bottom-frame.Top)
	var work rect
	pSystemParametersInfo.Call(0x30, 0, uintptr(unsafe.Pointer(&work)), 0) // SPI_GETWORKAREA
	x := int(work.Left) + (int(work.Right-work.Left)-w)/2
	y := int(work.Top) + (int(work.Bottom-work.Top)-h)/3

	title, _ := syscall.UTF16PtrFromString("Smugshot Settings")
	settingsWindow, _, _ = pCreateWindowEx.Call(0, uintptr(unsafe.Pointer(className)), uintptr(unsafe.Pointer(title)), wsCaption|wsSysMenu,
		uintptr(x), uintptr(y), uintptr(w), uintptr(h), 0, 0, instance, 0)
	if settingsWindow == 0 {
		return
	}
	if settingsFont == 0 {
		face, _ := syscall.UTF16PtrFromString("Segoe UI")
		settingsFont, _, _ = pCreateFont.Call(uintptr(-scale(12)&0xFFFFFFFF), 0, 0, 0, 400, 0, 0, 0, 1, 0, 0, 5, 0, uintptr(unsafe.Pointer(face)))
	}

	control := func(class, text string, style uintptr, id, x, y, w, h int) uintptr {
		c, _ := syscall.UTF16PtrFromString(class)
		t, _ := syscall.UTF16PtrFromString(text)
		hwnd, _, _ := pCreateWindowEx.Call(0, uintptr(unsafe.Pointer(c)), uintptr(unsafe.Pointer(t)), wsChild|wsVisible|style,
			uintptr(scale(x)), uintptr(scale(y)), uintptr(scale(w)), uintptr(scale(h)), settingsWindow, uintptr(id), instance, 0)
		pSendMessage.Call(hwnd, wmSetFont, settingsFont, 1)
		if id != 0 {
			settingsFields[id] = hwnd
		}
		return hwnd
	}
	label := func(text string, y int) { control("STATIC", text, 0, 0, 16, y+4, 100, 20) }
	edit := func(id, x, y, w int) uintptr {
		return control("EDIT", "", wsTabStop|wsBorder|esAutoHScroll, id, x, y, w, 23)
	}
	button := func(text string, id, x, y, w int) uintptr {
		return control("BUTTON", text, wsTabStop, id, x, y, w, 23)
	}
	combo := func(id, x, y, w int, titles []string) uintptr {
		hwnd := control("COMBOBOX", "", wsTabStop|wsVScroll|cbsDropDownList, id, x, y, w, 300)
		for _, title := range titles {
			t, _ := syscall.UTF16PtrFromString(title)
			pSendMessage.Call(hwnd, cbAddString, 0, uintptr(unsafe.Pointer(t)))
		}
		return hwnd
	}

	label("Shortcut:", 16)
	edit(idShortcut, 120, 16, 180)
	button("Use Default", idShortcutDefault, 396, 16, 88)

	label("Save to:", 48)
	edit(idFolder, 120, 48, 180)
	button("Choose…", idFolderChoose, 308, 48, 80)
	button("Use Default", idFolderDefault, 396, 48, 88)

	label("Keep for:", 80)
	keepTitles := make([]string, len(keepForChoices))
	for i, c := range keepForChoices {
		keepTitles[i] = c.Title
	}
	combo(idKeepFor, 120, 80, 140, keepTitles)

	label("Clipboard text:", 112)
	edit(idPrefix, 120, 112, 364)

	label("Sound:", 144)
	soundTitles := make([]string, 0, len(sounds)+1)
	for _, snd := range sounds {
		soundTitles = append(soundTitles, snd.Title)
	}
	soundTitles = append(soundTitles, "Off")
	combo(idSound, 120, 144, 140, soundTitles)

	label("Banner:", 176)
	control("BUTTON", "Show a small \"Copied\" banner after a smugshot", wsTabStop|bsAutoCheckBox, idBanner, 120, 176, 364, 23)

	settingsHint = control("STATIC", hintDefault, 0, idHint, 16, 216, 468, 20)

	refreshSettingsWindow()
	pShowWindow.Call(settingsWindow, 5) // SW_SHOW
	pSetForegroundWindow.Call(settingsWindow)
	pSetFocus.Call(settingsFields[idShortcut])
}

// refreshSettingsWindow fills the fields from the settings file. Also called after a change from the tray menu.
func refreshSettingsWindow() {
	if settingsWindow == 0 {
		return
	}
	settingsLoading = true
	defer func() { settingsLoading = false }()
	cfg := loadSettings()
	setText(settingsFields[idShortcut], cfg.Hotkey)
	setText(settingsFields[idFolder], cfg.Folder)
	setText(settingsFields[idPrefix], *cfg.Prefix)
	keep := 0
	for i, c := range keepForChoices {
		if c.Value == cfg.KeepFor {
			keep = i
		}
	}
	pSendMessage.Call(settingsFields[idKeepFor], cbSetCurSel, uintptr(keep), 0)
	snd := len(sounds) // "Off", the last entry
	if cfg.soundOn() {
		for i, s := range sounds {
			if s.Name == cfg.soundName() {
				snd = i
			}
		}
	}
	pSendMessage.Call(settingsFields[idSound], cbSetCurSel, uintptr(snd), 0)
	check := uintptr(0)
	if cfg.bannerOn() {
		check = 1
	}
	pSendMessage.Call(settingsFields[idBanner], bmSetCheck, check, 0)
}

func settingsProc(hwnd uintptr, message uint32, wParam, lParam uintptr) uintptr {
	switch message {
	case wmCommand:
		if settingsLoading {
			return 0
		}
		id, code := int(wParam&0xFFFF), int(wParam>>16)
		switch {
		case id == idCancel:
			pSendMessage.Call(hwnd, wmClose, 0, 0)
		case id == idShortcut && (code == enChange || code == enKillFocus):
			applyShortcut(strings.TrimSpace(getText(settingsFields[idShortcut])), code == enKillFocus)
		case id == idShortcutDefault:
			setText(settingsFields[idShortcut], defaultHotkey)
			applyShortcut(defaultHotkey, true)
		case id == idFolder && code == enKillFocus:
			applyFolder(strings.TrimSpace(getText(settingsFields[idFolder])))
		case id == idFolderChoose:
			if dir := chooseFolder(hwnd); dir != "" {
				setText(settingsFields[idFolder], dir)
				applyFolder(dir)
			}
		case id == idFolderDefault:
			setText(settingsFields[idFolder], defaultFolder())
			applyFolder("")
		case id == idKeepFor && code == cbnSelChange:
			if i, _, _ := pSendMessage.Call(settingsFields[idKeepFor], cbGetCurSel, 0, 0); int(i) >= 0 && int(i) < len(keepForChoices) {
				setSetting("keepFor", keepForChoices[i].Value)
				sweep()
			}
		case id == idPrefix && code == enChange:
			setSetting("prefix", getText(settingsFields[idPrefix]))
		case id == idSound && code == cbnSelChange:
			i, _, _ := pSendMessage.Call(settingsFields[idSound], cbGetCurSel, 0, 0)
			name := string(soundOff)
			if int(i) >= 0 && int(i) < len(sounds) {
				name = sounds[i].Name
			}
			setSetting("sound", name)
			playSound(name)
		case id == idBanner:
			on, _, _ := pSendMessage.Call(settingsFields[idBanner], bmGetCheck, 0, 0)
			setSetting("banner", on == 1)
		}
		return 0
	case wmCtlColorStatic: // labels on the window's own background, not the grey of a dialog
		if lParam == settingsHint {
			grey, _, _ := pGetSysColor.Call(17) // COLOR_GRAYTEXT
			pSetTextColor.Call(wParam, grey)
		}
		pSetBkMode.Call(wParam, 1)               // TRANSPARENT
		brush, _, _ := pGetSysColorBrush.Call(5) // COLOR_WINDOW
		return brush
	case wmClose:
		applyShortcut(strings.TrimSpace(getText(settingsFields[idShortcut])), true)
		applyFolder(strings.TrimSpace(getText(settingsFields[idFolder])))
		pDestroyWindow.Call(hwnd)
		return 0
	case wmDestroy:
		settingsWindow, settingsHint = 0, 0
		settingsFields = map[int]uintptr{}
		return 0
	}
	r, _, _ := pDefWindowProc.Call(hwnd, uintptr(message), wParam, lParam)
	return r
}

// applyShortcut saves and registers a shortcut that parses, and says so at the bottom of the window. While
// the person is still typing (final is false) a shortcut that does not parse yet says nothing.
func applyShortcut(value string, final bool) {
	if _, _, err := parseHotkey(value); err != nil {
		if final {
			setHint("A shortcut is modifiers plus one key, like ctrl+shift+1, ctrl+alt+s or win+shift+f9.")
		}
		return
	}
	value = strings.ToLower(strings.ReplaceAll(value, " ", ""))
	if value == loadSettings().Hotkey && !final {
		return
	}
	setSetting("hotkey", value)
	if !registerHotkey(value) {
		setHint("That shortcut is taken by another program. Pick another.")
		return
	}
	setTrayTip(value)
	setHint(hintDefault)
}

// applyFolder saves the folder; the default folder, or nothing, is saved as "" so the default can move with
// the home folder.
func applyFolder(dir string) {
	if dir == "" || strings.EqualFold(filepath.Clean(dir), filepath.Clean(defaultFolder())) {
		dir = ""
	}
	setSetting("folder", dir)
}

// chooseFolder shows the folder picker and returns the choice, or "" for cancel.
func chooseFolder(owner uintptr) string {
	pCoInitializeEx.Call(0, 0x2) // COINIT_APARTMENTTHREADED; the new-style dialog needs it
	title, _ := syscall.UTF16PtrFromString("Where smugshots go")
	name := make([]uint16, 260)
	info := browseInfo{Owner: owner, DisplayName: &name[0], Title: title, Flags: 0x1 | 0x40} // BIF_RETURNONLYFSDIRS, BIF_NEWDIALOGSTYLE
	pidl, _, _ := pSHBrowseForFolder.Call(uintptr(unsafe.Pointer(&info)))
	if pidl == 0 {
		return ""
	}
	defer pCoTaskMemFree.Call(pidl)
	path := make([]uint16, 1024)
	if ok, _, _ := pSHGetPathFromIDL.Call(pidl, uintptr(unsafe.Pointer(&path[0]))); ok == 0 {
		return ""
	}
	return syscall.UTF16ToString(path)
}

func setHint(text string) {
	if settingsHint != 0 {
		setText(settingsHint, text)
	}
}

func setText(hwnd uintptr, text string) {
	t, _ := syscall.UTF16PtrFromString(text)
	pSetWindowText.Call(hwnd, uintptr(unsafe.Pointer(t)))
}

func getText(hwnd uintptr) string {
	n, _, _ := pGetWindowTextLen.Call(hwnd)
	buf := make([]uint16, n+1)
	pGetWindowText.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return syscall.UTF16ToString(buf)
}
