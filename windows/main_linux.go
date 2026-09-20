//go:build linux

// Smugshot for Linux: press Ctrl+Shift+1, drag, paste the path.
// Same gesture and the same files as the Mac and Windows versions. It talks to the X server directly (the
// pure-Go xgb library, no C), so it runs on X11 and on the XWayland side of a Wayland desktop. Not yet here:
// a tray icon, a Settings window, naming the control under the drag, its text, and the browser element.
package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/randr"
	"github.com/jezek/xgb/xproto"
)

// The X connection and what was looked up once at start.
var (
	X       *xgb.Conn
	screen  *xproto.ScreenInfo
	rootWin xproto.Window
	overlay xproto.Window
	cursor  xproto.Cursor
	atoms   = map[string]xproto.Atom{}
	keysyms []xproto.Keysym // the keyboard mapping, keysymsPerKeycode entries per keycode
	perCode int
	minCode xproto.Keycode
)

// One gesture in flight at a time.
var (
	active    bool
	dragging  bool
	start     image.Point
	current   image.Point
	shot      *image.RGBA // the whole desktop as it was when the shortcut was pressed
	dimmed    xproto.Pixmap
	drawGC    xproto.Gcontext
	startedAt time.Time
	lastPath  string
	clipboard string // what this process serves when another app pastes
)

func main() {
	if !lockSingleInstance() {
		fmt.Fprintln(os.Stderr, "Smugshot is already running.")
		return
	}
	var err error
	if X, err = xgb.NewConn(); err != nil {
		fmt.Fprintln(os.Stderr, "Smugshot needs an X display (DISPLAY is not set, or the server refused):", err)
		os.Exit(1)
	}
	defer X.Close()
	setup := xproto.Setup(X)
	screen = setup.DefaultScreen(X)
	rootWin = screen.Root
	randr.Init(X) // optional: without it, one big screen

	ensureSettingsFile() // so the person can find and edit it: there is no Settings window here yet
	os.MkdirAll(root(), 0o700)
	sweep()
	go func() { // short "keepFor" settings need the sweep to run on its own, not only after each shot
		for range time.Tick(30 * time.Second) {
			sweep()
		}
	}()

	for _, name := range []string{"CLIPBOARD", "TARGETS", "UTF8_STRING", "STRING", "ATOM", "_NET_CLIENT_LIST_STACKING",
		"_NET_WM_NAME", "_NET_WM_PID", "WM_NAME", "WM_CLASS", "CARDINAL", "WINDOW"} {
		reply, err := xproto.InternAtom(X, false, uint16(len(name)), name).Reply()
		if err == nil {
			atoms[name] = reply.Atom
		}
	}
	loadKeyboard()
	makeCursor()
	makeOverlay()

	hotkey := loadSettings().Hotkey
	if err := grabHotkey(hotkey); err != nil {
		fmt.Fprintf(os.Stderr, "Smugshot: could not take the shortcut %s: %v\n", hotkey, err)
	}
	notify("Smugshot is running", "Press "+hotkeyLabel(hotkey)+", drag, paste the path.")

	for {
		event, xerr := X.WaitForEvent()
		if event == nil && xerr == nil {
			return // the connection is gone
		}
		if xerr != nil {
			continue
		}
		switch e := event.(type) {
		case xproto.KeyPressEvent:
			handleKey(e)
		case xproto.ButtonPressEvent:
			if !active {
				break
			}
			if e.Detail == 3 { // right button
				cancel()
			} else if e.Detail == 1 {
				dragging = true
				start = image.Pt(int(e.RootX), int(e.RootY))
				current = start
			}
		case xproto.MotionNotifyEvent:
			if dragging {
				current = image.Pt(int(e.RootX), int(e.RootY))
				paint()
			}
		case xproto.ButtonReleaseEvent:
			if dragging && e.Detail == 1 {
				current = image.Pt(int(e.RootX), int(e.RootY))
				finish()
			}
		case xproto.ExposeEvent:
			if active {
				paint()
			}
		case xproto.SelectionRequestEvent:
			serveClipboard(e)
		case xproto.MappingNotifyEvent:
			loadKeyboard()
			grabHotkey(loadSettings().Hotkey)
		}
	}
}

// lockFile stays open for as long as this process lives; the lock goes with it. Kept here so the garbage
// collector does not close it.
var lockFile *os.File

// lockSingleInstance takes the lock. A second copy gets false.
func lockSingleInstance() bool {
	dir := os.Getenv("XDG_RUNTIME_DIR")
	if dir == "" {
		dir = os.TempDir()
	}
	f, err := os.OpenFile(filepath.Join(dir, "smugshot.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return true
	}
	if syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) != nil {
		f.Close()
		return false
	}
	lockFile = f
	return true
}

// MARK: Keyboard

func loadKeyboard() {
	setup := xproto.Setup(X)
	minCode = setup.MinKeycode
	count := byte(setup.MaxKeycode - setup.MinKeycode + 1)
	reply, err := xproto.GetKeyboardMapping(X, minCode, count).Reply()
	if err != nil {
		return
	}
	keysyms, perCode = reply.Keysyms, int(reply.KeysymsPerKeycode)
}

func keycodeFor(sym xproto.Keysym) xproto.Keycode {
	for i := 0; i+perCode <= len(keysyms); i += perCode {
		if keysyms[i] == sym {
			return minCode + xproto.Keycode(i/perCode)
		}
	}
	return 0
}

func keysymOf(code xproto.Keycode) xproto.Keysym {
	i := int(code-minCode) * perCode
	if i < 0 || i >= len(keysyms) {
		return 0
	}
	return keysyms[i]
}

const (
	symEscape = 0xff1b
	symF1     = 0xffbe
)

// hotkeyX11 turns the settings value ("ctrl+shift+1", "win+shift+f9") into X modifier bits and a keysym.
// The parsing is shared with Windows; only the numbers differ.
func hotkeyX11(value string) (mods uint16, sym xproto.Keysym, err error) {
	winMods, vk, err := parseHotkey(value)
	if err != nil {
		return 0, 0, err
	}
	if winMods&0x1 != 0 { // alt
		mods |= xproto.ModMask1
	}
	if winMods&0x2 != 0 { // ctrl
		mods |= xproto.ModMaskControl
	}
	if winMods&0x4 != 0 { // shift
		mods |= xproto.ModMaskShift
	}
	if winMods&0x8 != 0 { // win, the Super key here
		mods |= xproto.ModMask4
	}
	switch {
	case vk >= '0' && vk <= '9':
		sym = xproto.Keysym(vk)
	case vk >= 'A' && vk <= 'Z':
		sym = xproto.Keysym(vk + 0x20) // the lowercase keysym is the one on the key
	case vk >= 0x70 && vk < 0x70+24:
		sym = symF1 + xproto.Keysym(vk-0x70)
	default:
		return 0, 0, fmt.Errorf("unknown key in %s", value)
	}
	return mods, sym, nil
}

var grabbed []struct {
	mods uint16
	code xproto.Keycode
}

// grabHotkey takes the shortcut on the rootWin window, with and without Num Lock and Caps Lock, in place of
// whatever was grabbed before.
func grabHotkey(value string) error {
	for _, g := range grabbed {
		xproto.UngrabKey(X, g.code, rootWin, g.mods)
	}
	grabbed = nil
	mods, sym, err := hotkeyX11(value)
	if err != nil {
		return err
	}
	code := keycodeFor(sym)
	if code == 0 {
		return fmt.Errorf("no key on this keyboard for %s", value)
	}
	var firstErr error
	for _, lock := range []uint16{0, xproto.ModMaskLock, xproto.ModMask2, xproto.ModMaskLock | xproto.ModMask2} {
		err := xproto.GrabKeyChecked(X, true, rootWin, mods|lock, code, xproto.GrabModeAsync, xproto.GrabModeAsync).Check()
		if err != nil && firstErr == nil {
			firstErr = err
		}
		grabbed = append(grabbed, struct {
			mods uint16
			code xproto.Keycode
		}{mods | lock, code})
	}
	return firstErr
}

// handleKey: the shortcut starts a smugshot. While the layer is up, Esc or the shortcut again cancels;
// every other key is swallowed by the keyboard grab.
func handleKey(e xproto.KeyPressEvent) {
	sym := keysymOf(e.Detail)
	_, hotkeySym, _ := hotkeyX11(loadSettings().Hotkey)
	if active {
		if sym == symEscape || sym == hotkeySym {
			cancel()
		}
		return
	}
	begin()
}

// MARK: The drag layer

func makeCursor() {
	font, _ := xproto.NewFontId(X)
	xproto.OpenFont(X, font, uint16(len("cursor")), "cursor")
	cursor, _ = xproto.NewCursorId(X)
	xproto.CreateGlyphCursor(X, cursor, font, font, 34, 35, 0, 0, 0, 0xffff, 0xffff, 0xffff) // XC_crosshair
	xproto.CloseFont(X, font)
}

func makeOverlay() {
	overlay, _ = xproto.NewWindowId(X)
	mask := uint32(xproto.CwBackPixel | xproto.CwOverrideRedirect | xproto.CwEventMask | xproto.CwCursor)
	values := []uint32{0, 1, xproto.EventMaskExposure | xproto.EventMaskButtonPress | xproto.EventMaskButtonRelease |
		xproto.EventMaskPointerMotion | xproto.EventMaskKeyPress, uint32(cursor)}
	xproto.CreateWindow(X, screen.RootDepth, overlay, rootWin, 0, 0, 1, 1, 0, xproto.WindowClassInputOutput,
		screen.RootVisual, mask, values)
	drawGC, _ = xproto.NewGcontextId(X)
	xproto.CreateGC(X, drawGC, xproto.Drawable(overlay), xproto.GcForeground|xproto.GcLineWidth,
		[]uint32{0xFF2D55, 3})
}

// begin pictures the whole desktop first, then puts up the drag layer showing that picture, dimmed, so the
// screen stands still while the user drags and the layer is never in the picture.
func begin() {
	shot = captureRoot()
	if shot == nil {
		notify("Smugshot", "Could not take the picture.")
		return
	}
	w, h := shot.Bounds().Dx(), shot.Bounds().Dy()
	active, dragging, startedAt = true, false, time.Now()

	dimmed, _ = xproto.NewPixmapId(X)
	xproto.CreatePixmap(X, screen.RootDepth, dimmed, xproto.Drawable(rootWin), uint16(w), uint16(h))
	putImage(xproto.Drawable(dimmed), 0, 0, dim(shot))
	xproto.ChangeWindowAttributes(X, overlay, xproto.CwBackPixmap, []uint32{uint32(dimmed)})
	xproto.ConfigureWindow(X, overlay, xproto.ConfigWindowX|xproto.ConfigWindowY|xproto.ConfigWindowWidth|
		xproto.ConfigWindowHeight|xproto.ConfigWindowStackMode, []uint32{0, 0, uint32(w), uint32(h), xproto.StackModeAbove})
	xproto.MapWindow(X, overlay)
	xproto.GrabPointer(X, false, overlay, xproto.EventMaskButtonPress|xproto.EventMaskButtonRelease|xproto.EventMaskPointerMotion,
		xproto.GrabModeAsync, xproto.GrabModeAsync, overlay, cursor, xproto.TimeCurrentTime).Reply()
	xproto.GrabKeyboard(X, false, overlay, xproto.TimeCurrentTime, xproto.GrabModeAsync, xproto.GrabModeAsync).Reply()
}

func cancel() {
	xproto.UngrabPointer(X, xproto.TimeCurrentTime)
	xproto.UngrabKeyboard(X, xproto.TimeCurrentTime)
	xproto.UnmapWindow(X, overlay)
	if dimmed != 0 {
		xproto.FreePixmap(X, dimmed)
		dimmed = 0
	}
	active, dragging, shot = false, false, nil
	X.Sync()
}

func selection() image.Rectangle {
	return image.Rectangle{start, current}.Canon()
}

// paint shows the dragged part undimmed with a frame around it, on top of the dimmed picture.
func paint() {
	xproto.ClearArea(X, false, overlay, 0, 0, 0, 0)
	if !dragging || shot == nil {
		return
	}
	r := selection().Intersect(shot.Bounds())
	if !r.Empty() {
		putImage(xproto.Drawable(overlay), r.Min.X, r.Min.Y, shot.SubImage(r).(*image.RGBA))
	}
	xproto.PolyRectangle(X, xproto.Drawable(overlay), drawGC, []xproto.Rectangle{{
		X: int16(r.Min.X - 2), Y: int16(r.Min.Y - 2), Width: uint16(r.Dx() + 3), Height: uint16(r.Dy() + 3)}})
}

// dim is the frozen picture at 82%, the same faint dim as on the Mac.
func dim(src *image.RGBA) *image.RGBA {
	out := image.NewRGBA(src.Bounds())
	for i := 0; i+3 < len(src.Pix); i += 4 {
		out.Pix[i] = uint8(uint32(src.Pix[i]) * 82 / 100)
		out.Pix[i+1] = uint8(uint32(src.Pix[i+1]) * 82 / 100)
		out.Pix[i+2] = uint8(uint32(src.Pix[i+2]) * 82 / 100)
		out.Pix[i+3] = 0xFF
	}
	return out
}

// putImage sends an RGBA picture to a drawable as 32-bit ZPixmap rows, in slices small enough for one request.
func putImage(dst xproto.Drawable, x, y int, img *image.RGBA) {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w == 0 || h == 0 {
		return
	}
	rowsPerRequest := 200000 / (w * 4)
	if rowsPerRequest < 1 {
		rowsPerRequest = 1
	}
	for y0 := 0; y0 < h; y0 += rowsPerRequest {
		y1 := y0 + rowsPerRequest
		if y1 > h {
			y1 = h
		}
		data := make([]byte, 0, w*4*(y1-y0))
		for yy := y0; yy < y1; yy++ {
			row := img.Pix[img.PixOffset(b.Min.X, b.Min.Y+yy):]
			for xx := 0; xx < w; xx++ {
				p := row[xx*4 : xx*4+4]
				data = append(data, p[2], p[1], p[0], 0) // the server wants blue, green, red
			}
		}
		xproto.PutImage(X, xproto.ImageFormatZPixmap, dst, drawGC, uint16(w), uint16(y1-y0), int16(x), int16(y+y0), 0,
			screen.RootDepth, data)
	}
}

// captureRoot is the whole desktop, every screen, as the server has it now.
func captureRoot() *image.RGBA {
	w, h := int(screen.WidthInPixels), int(screen.HeightInPixels)
	reply, err := xproto.GetImage(X, xproto.ImageFormatZPixmap, xproto.Drawable(rootWin), 0, 0, uint16(w), uint16(h), 0xffffffff).Reply()
	if err != nil || len(reply.Data) < w*h*4 {
		return nil
	}
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := 0; i+3 < w*h*4; i += 4 { // the server hands back blue, green, red
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = reply.Data[i+2], reply.Data[i+1], reply.Data[i], 0xFF
	}
	return img
}

func finish() {
	sel := selection()
	picture := shot
	at := startedAt
	cancel()
	if picture == nil || sel.Dx() < 4 || sel.Dy() < 4 {
		return
	}

	// The app and window under the middle of the drag, now that our layer is out of the way.
	centre := image.Pt((sel.Min.X+sel.Max.X)/2, (sel.Min.Y+sel.Max.Y)/2)
	app, title := windowAt(centre)

	// Only the screen the drag was on, like the Mac version.
	screenRect := monitorRect(centre).Intersect(picture.Bounds())
	flat := image.NewRGBA(image.Rect(0, 0, screenRect.Dx(), screenRect.Dy()))
	for yy := 0; yy < screenRect.Dy(); yy++ {
		s := picture.PixOffset(screenRect.Min.X, screenRect.Min.Y+yy)
		copy(flat.Pix[flat.PixOffset(0, yy):], picture.Pix[s:s+screenRect.Dx()*4])
	}
	region := sel.Sub(screenRect.Min).Intersect(flat.Bounds())

	dir, err := newFolder(at)
	if err != nil {
		notify("Smugshot", "Could not write the smugshot: "+err.Error())
		return
	}
	fullImg, fullRegion := full(flat, region)
	if writePNG(fullImg, filepath.Join(dir, "full.png")) != nil || writePNG(crop(flat, region), filepath.Join(dir, "crop.png")) != nil {
		notify("Smugshot", "Could not write the pictures.")
		return
	}
	path := filepath.Join(dir, "shot.md")
	os.WriteFile(path, []byte(strings.ReplaceAll(shotText(dir, at, app, title, fullRegion, fullImg.Bounds().Size()), "\r\n", "\n")), 0o600)
	cfg := loadSettings()
	setClipboard(*cfg.Prefix + path)
	lastPath = path
	if cfg.soundOn() {
		playTink()
	}
	if cfg.bannerOn() {
		notify("Smugshot", "Copied. Paste it into the chat.")
	}
	sweep()
}

// monitorRect is the screen that holds p, from RandR, or the whole desktop when RandR is not there.
func monitorRect(p image.Point) image.Rectangle {
	whole := image.Rect(0, 0, int(screen.WidthInPixels), int(screen.HeightInPixels))
	reply, err := randr.GetMonitors(X, rootWin, true).Reply()
	if err != nil {
		return whole
	}
	for _, m := range reply.Monitors {
		r := image.Rect(int(m.X), int(m.Y), int(m.X)+int(m.Width), int(m.Y)+int(m.Height))
		if p.In(r) {
			return r
		}
	}
	return whole
}

// MARK: The window under the drag

// windowAt names the top window under p: the program (from its process) and the window title.
// It walks the window manager's stacking list, top first; without a window manager, the rootWin's children.
func windowAt(p image.Point) (app, title string) {
	var candidates []xproto.Window
	if list, err := xproto.GetProperty(X, false, rootWin, atoms["_NET_CLIENT_LIST_STACKING"], atoms["WINDOW"], 0, 1<<16).Reply(); err == nil && list.ValueLen > 0 {
		for i := int(list.ValueLen) - 1; i >= 0; i-- { // the list is bottom to top
			candidates = append(candidates, xproto.Window(binary.LittleEndian.Uint32(list.Value[i*4:])))
		}
	} else if tree, err := xproto.QueryTree(X, rootWin).Reply(); err == nil {
		for i := len(tree.Children) - 1; i >= 0; i-- {
			candidates = append(candidates, tree.Children[i])
		}
	}
	for _, w := range candidates {
		if w == overlay {
			continue
		}
		attrs, err := xproto.GetWindowAttributes(X, w).Reply()
		if err != nil || attrs.MapState != xproto.MapStateViewable {
			continue
		}
		geo, err := xproto.GetGeometry(X, xproto.Drawable(w)).Reply()
		if err != nil {
			continue
		}
		pos, err := xproto.TranslateCoordinates(X, w, rootWin, 0, 0).Reply()
		if err != nil {
			continue
		}
		r := image.Rect(int(pos.DstX), int(pos.DstY), int(pos.DstX)+int(geo.Width), int(pos.DstY)+int(geo.Height))
		if !p.In(r) {
			continue
		}
		title = textProperty(w, "_NET_WM_NAME", "UTF8_STRING")
		if title == "" {
			title = textProperty(w, "WM_NAME", "STRING")
		}
		if pid := cardinalProperty(w, "_NET_WM_PID"); pid > 0 {
			if comm, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/comm"); err == nil {
				app = strings.TrimSpace(string(comm))
			}
		}
		if app == "" {
			if class := textProperty(w, "WM_CLASS", "STRING"); class != "" {
				parts := strings.Split(class, "\x00") // instance, then class
				app = parts[len(parts)-1]
				if app == "" && len(parts) > 1 {
					app = parts[len(parts)-2]
				}
			}
		}
		return app, title
	}
	return "", ""
}

func textProperty(w xproto.Window, name, kind string) string {
	reply, err := xproto.GetProperty(X, false, w, atoms[name], atoms[kind], 0, 1<<16).Reply()
	if err != nil || reply.ValueLen == 0 {
		return ""
	}
	return strings.TrimRight(string(reply.Value), "\x00")
}

func cardinalProperty(w xproto.Window, name string) int {
	reply, err := xproto.GetProperty(X, false, w, atoms[name], atoms["CARDINAL"], 0, 1).Reply()
	if err != nil || reply.ValueLen == 0 || len(reply.Value) < 4 {
		return 0
	}
	return int(binary.LittleEndian.Uint32(reply.Value))
}

// MARK: Clipboard

// setClipboard makes this process the owner of the clipboard. X has no clipboard store: the owner hands the
// text over each time another program pastes, for as long as it runs.
func setClipboard(text string) {
	clipboard = text
	xproto.SetSelectionOwner(X, overlay, atoms["CLIPBOARD"], xproto.TimeCurrentTime)
	X.Sync()
}

func serveClipboard(e xproto.SelectionRequestEvent) {
	property := e.Property
	if property == xproto.AtomNone {
		property = e.Target
	}
	switch e.Target {
	case atoms["TARGETS"]:
		var data []byte
		for _, a := range []xproto.Atom{atoms["TARGETS"], atoms["UTF8_STRING"], atoms["STRING"]} {
			data = binary.LittleEndian.AppendUint32(data, uint32(a))
		}
		xproto.ChangeProperty(X, xproto.PropModeReplace, e.Requestor, property, atoms["ATOM"], 32, uint32(len(data)/4), data)
	case atoms["UTF8_STRING"], atoms["STRING"]:
		xproto.ChangeProperty(X, xproto.PropModeReplace, e.Requestor, property, e.Target, 8, uint32(len(clipboard)), []byte(clipboard))
	default:
		property = xproto.AtomNone
	}
	notifyEvent := xproto.SelectionNotifyEvent{Time: e.Time, Requestor: e.Requestor, Selection: e.Selection, Target: e.Target, Property: property}
	xproto.SendEvent(X, false, e.Requestor, 0, string(notifyEvent.Bytes()))
	X.Sync()
}

// MARK: Sound and messages

// playTink plays the bundled sound through whichever player the desktop has. Silent when there is none.
func playTink() {
	path := filepath.Join(os.TempDir(), "smugshot-tink.wav")
	if data, err := os.ReadFile(path); err != nil || !bytes.Equal(data, tinkWAV) {
		os.WriteFile(path, tinkWAV, 0o600)
	}
	for _, player := range [][]string{{"paplay", path}, {"pw-play", path}, {"aplay", "-q", path}} {
		if _, err := exec.LookPath(player[0]); err == nil {
			exec.Command(player[0], player[1:]...).Start()
			return
		}
	}
}

// notify shows a desktop notification, the Linux stand-in for the Windows banner. Nothing when notify-send is missing.
func notify(title, body string) {
	if _, err := exec.LookPath("notify-send"); err == nil {
		exec.Command("notify-send", "-a", "Smugshot", "-t", "2500", title, body).Start()
	}
}
