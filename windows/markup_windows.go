//go:build windows

package main

// A marked-up smugshot: press the mark-up hotkey (ctrl+shift+2 unless the settings say otherwise), drag a
// spot, type a note for it in the box that opens beside it, and keep going. Enter saves the note and puts the
// drag layer back for the next spot, Shift+Enter is a new line in the note, Esc in the box drops that spot,
// and Ctrl+Enter, Esc on the drag layer or either hotkey ends the mark-up. Everything lands in one folder,
// crop-01.png and full-01.png onwards, with one shot.md listing the spots and their notes in order, and that
// path goes on the clipboard like any smugshot.

import (
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

const (
	hotkeyShot, hotkeyMarkup                 = 1, 2 // RegisterHotKey ids, which come back as wmHotKey's wParam
	vkReturn, vkShift, vkCtrl                = 0x0D, 0x10, 0x11
	esMultiline, esAutoVScroll, esWantReturn = 0x4, 0x40, 0x1000
)

var (
	pGetKeyState = user32.NewProc("GetKeyState")
	pDwmFlush    = syscall.NewLazyDLL("dwmapi.dll").NewProc("DwmFlush")
)

// The mark-up in flight. Only one at a time, like the gesture.
var markup struct {
	on      bool
	at      time.Time
	dir     string  // made with the first spot, so a mark-up stopped before any spot leaves nothing behind
	spots   []*spot // pointers: the text reader fills in Text while the next spot is being dragged
	reading sync.WaitGroup
}

var (
	noteWindow, noteLabel, noteEdit, noteFont uintptr
	noteOpen                                  bool
)

func startMarkup() {
	markup.on, markup.at, markup.dir, markup.spots = true, time.Now(), "", nil
	begin()
	if !active { // the screen could not be pictured
		resetMarkup()
	}
}

func resetMarkup() {
	markup.on, markup.dir, markup.spots = false, "", nil
}

// finishSpot is the end of one drag while marking up: save the two pictures, start reading the text, and ask
// for the note.
func finishSpot() {
	sel := selection()
	dragging = false
	if shot == nil {
		endMarkup()
		return
	}
	if tooSmall(sel) { // a click: still marking up, drag again
		pInvalidateRect.Call(overlay, 0, 1)
		return
	}
	picture := shot
	active, shot = false, nil
	pShowWindow.Call(overlay, 0)
	hidePasteHint()

	if markup.dir == "" {
		dir, err := newFolder(markup.at)
		if err != nil {
			pMessageBeep.Call(0x10)
			resetMarkup()
			return
		}
		markup.dir = dir
	}
	n := len(markup.spots) + 1
	cropName, fullName := spotFiles(n)
	app, title, region, size, ok := saveCapture(picture, sel, markup.dir, cropName, fullName)
	if !ok {
		pMessageBeep.Call(0x10)
		nextSpot()
		return
	}
	s := &spot{Crop: cropName, Full: fullName, Region: region, Size: size, App: app, Window: title}
	markup.spots = append(markup.spots, s)
	if loadSettings().readTextOn() {
		cropPath := filepath.Join(markup.dir, cropName)
		markup.reading.Add(1)
		go func() {
			defer markup.reading.Done()
			s.Text = readText(cropPath)
		}()
	}
	onScreen := rect{origin.X + sel.Left, origin.Y + sel.Top, origin.X + sel.Right, origin.Y + sel.Bottom}
	openNote(n, onScreen)
}

// nextSpot puts the drag layer back up for another spot, once the note box is off the screen, so the box is
// never in the next picture.
func nextSpot() {
	hideNote()
	pDwmFlush.Call()
	time.Sleep(50 * time.Millisecond)
	begin()
	if !active {
		endMarkup()
	}
}

// endMarkup writes shot.md for the spots so far and hands it over. With no spots there is no smugshot.
func endMarkup() {
	if dragging {
		pReleaseCapture.Call()
	}
	active, dragging, shot = false, false, nil
	pShowWindow.Call(overlay, 0)
	hidePasteHint()
	if noteOpen {
		saveNote()
	}
	hideNote()

	dir, at, spots := markup.dir, markup.at, markup.spots
	markup.reading.Wait() // the text reader takes a second or so per close-up
	resetMarkup()
	if len(spots) == 0 {
		if dir != "" {
			os.Remove(dir) // empty: every spot was dropped
		}
		return
	}
	list := make([]spot, len(spots))
	for i, s := range spots {
		list[i] = *s
	}
	path := filepath.Join(dir, "shot.md")
	os.WriteFile(path, []byte(markupText(dir, at, list)), 0o600)
	deliver(path, loadSettings())
}

// dropSpot is Esc in the note box: the last spot goes, files and all.
func dropSpot() {
	n := len(markup.spots)
	if n == 0 {
		return
	}
	s := markup.spots[n-1]
	markup.spots = markup.spots[:n-1]
	os.Remove(filepath.Join(markup.dir, s.Crop))
	os.Remove(filepath.Join(markup.dir, s.Full))
}

func saveNote() {
	if n := len(markup.spots); n > 0 && noteEdit != 0 {
		markup.spots[n-1].Note = getText(noteEdit)
	}
}

func keyDown(vk uintptr) bool {
	state, _, _ := pGetKeyState.Call(vk)
	return state&0x8000 != 0
}

// noteKey handles the keys that move a mark-up along when they are pressed in the note box. Called from the
// message loop before the message is translated, so Enter never becomes a new line unless Shift is held.
func noteKey(m *msg) bool {
	if !noteOpen || m.Hwnd != noteEdit || m.Message != wmKeyDown {
		return false
	}
	switch m.WParam {
	case vkReturn:
		if keyDown(vkShift) {
			return false // a new line in the note
		}
		saveNote()
		if keyDown(vkCtrl) {
			endMarkup()
		} else {
			nextSpot()
		}
		return true
	case vkEscape:
		dropSpot()
		nextSpot()
		return true
	}
	return false
}

func hideNote() {
	if noteWindow != 0 {
		pShowWindow.Call(noteWindow, 0)
	}
	noteOpen = false
}

// openNote shows the note box for spot n beside the spot: below it, or above when there is no room below,
// kept on the spot's screen.
func openNote(n int, spotRect rect) {
	instance, _, _ := pGetModuleHandle.Call(0)
	dpi, _, _ := pGetDpiForSystem.Call()
	if dpi == 0 {
		dpi = 96
	}
	scale := func(v int) int { return v * int(dpi) / 96 }
	if noteWindow == 0 {
		className, _ := syscall.UTF16PtrFromString("SmugshotNote")
		arrow, _, _ := pLoadCursor.Call(0, 32512)                                                                                               // IDC_ARROW
		class := wndClassEx{WndProc: syscall.NewCallback(noteProc), Instance: instance, Cursor: arrow, Background: 5 + 1, ClassName: className} // COLOR_WINDOW + 1
		class.Size = uint32(unsafe.Sizeof(class))
		pRegisterClassEx.Call(uintptr(unsafe.Pointer(&class)))
		noteWindow, _, _ = pCreateWindowEx.Call(wsExTopmost|wsExToolWindow, uintptr(unsafe.Pointer(className)), 0, wsPopup|wsBorder,
			0, 0, uintptr(scale(380)), uintptr(scale(150)), 0, 0, instance, 0)
		face, _ := syscall.UTF16PtrFromString("Segoe UI")
		noteFont, _, _ = pCreateFont.Call(uintptr(-scale(13)&0xFFFFFFFF), 0, 0, 0, 400, 0, 0, 0, 1, 0, 0, 5, 0, uintptr(unsafe.Pointer(face)))
		child := func(class string, style uintptr, x, y, w, h int) uintptr {
			c, _ := syscall.UTF16PtrFromString(class)
			hwnd, _, _ := pCreateWindowEx.Call(0, uintptr(unsafe.Pointer(c)), 0, wsChild|wsVisible|style,
				uintptr(scale(x)), uintptr(scale(y)), uintptr(scale(w)), uintptr(scale(h)), noteWindow, 0, instance, 0)
			pSendMessage.Call(hwnd, wmSetFont, noteFont, 1)
			return hwnd
		}
		noteLabel = child("STATIC", 0, 10, 6, 360, 36)
		noteEdit = child("EDIT", wsBorder|wsVScroll|esMultiline|esAutoVScroll|esWantReturn, 10, 46, 358, 94)
	}
	setText(noteLabel, noteLabelText(n))
	setText(noteEdit, "")

	w, h := int32(scale(380)), int32(scale(150))
	gap := int32(scale(10))
	mon := monitorInfo{}
	mon.Size = uint32(unsafe.Sizeof(mon))
	centre := point{(spotRect.Left + spotRect.Right) / 2, (spotRect.Top + spotRect.Bottom) / 2}
	m, _, _ := pMonitorFromPoint.Call(uintptr(uint32(centre.X))|uintptr(uint32(centre.Y))<<32, 2) // MONITOR_DEFAULTTONEAREST
	pGetMonitorInfo.Call(m, uintptr(unsafe.Pointer(&mon)))
	work := mon.Work
	x, y := spotRect.Left, spotRect.Bottom+gap
	if y+h > work.Bottom {
		y = spotRect.Top - gap - h
	}
	if y < work.Top { // a spot as tall as the screen: over its bottom edge
		y = work.Bottom - h - gap
	}
	if x+w > work.Right {
		x = work.Right - w - gap
	}
	if x < work.Left {
		x = work.Left + gap
	}
	pSetWindowPos.Call(noteWindow, ^uintptr(0), uintptr(x), uintptr(y), uintptr(w), uintptr(h), 0x0040) // HWND_TOPMOST, SWP_SHOWWINDOW
	pSetForegroundWindow.Call(noteWindow)
	pSetFocus.Call(noteEdit)
	noteOpen = true
}

func noteProc(hwnd uintptr, message uint32, wParam, lParam uintptr) uintptr {
	if message == wmClose { // Alt+F4 in the box: the same as Ctrl+Enter
		if markup.on {
			saveNote()
			endMarkup()
		}
		return 0
	}
	r, _, _ := pDefWindowProc.Call(hwnd, uintptr(message), wParam, lParam)
	return r
}
