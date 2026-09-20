//go:build linux

// Smugshot on Wayland. Wayland deliberately keeps programs apart: one program cannot grab a shortcut for the
// whole desktop, cannot take a picture of the screen on its own, and cannot see other programs' windows. So the
// Wayland version works differently from the X11 one:
//
//   - One run is one smugshot. There is no background program holding a hotkey; you bind `smugshot` to a key in
//     your own desktop settings (the README says how for GNOME, KDE, sway and Hyprland).
//   - The picture comes from the compositor: the wlroots screencopy protocol where there is one (sway, Hyprland),
//     and otherwise the desktop's own screenshot service over D-Bus (GNOME, KDE). See capture_linux.go.
//   - The drag layer is a surface of ours per screen: the layer-shell one where there is one (sway, Hyprland,
//     KDE), and otherwise a plain fullscreen window (GNOME).
//   - shot.md has no "App" or "Window" line, because no Wayland program may ask what is under the pointer.
//
// After the drag the program stays alive for a while, because on Wayland the clipboard is served by whoever put
// it there: quitting at once would leave nothing to paste.
package main

import (
	"errors"
	"fmt"
	"image"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	wlos "github.com/neurlang/wayland/os"
	"github.com/neurlang/wayland/wl"
	"github.com/neurlang/wayland/wlclient"
	xdgshell "github.com/neurlang/wayland/xdg"

	"github.com/pjalle/smugshot/windows/wlproto/layershell"
	"github.com/pjalle/smugshot/windows/wlproto/screencopy"
)

// How long the program lingers after a smugshot so that the paste still works. It ends sooner if something
// else takes the clipboard.
const clipboardLinger = 10 * time.Minute

// escapeKey is Escape as Wayland reports it: the Linux input code (1) plus 8.
const escapeKey = 9

type wlScreen struct {
	output        *wl.Output
	name          uint32
	x, y          int32 // where this screen sits in the desktop, in logical points
	width, height int32 // logical size
	scale         int32
	shot          *image.RGBA // the frozen picture of this screen, in real pixels

	surface *wl.Surface
	layer   *layershell.ZwlrLayerSurfaceV1
	xdgSurf *xdgshell.Surface
	xdgTop  *xdgshell.Toplevel
	buffers [2]*wlBuffer
	next    int
	ready   bool
}

type wlBuffer struct {
	buffer *wl.Buffer
	data   []byte
	busy   bool
	drawn  image.Rectangle // the selection this buffer currently shows, for repairing it
}

func (b *wlBuffer) HandleBufferRelease(wl.BufferReleaseEvent) { b.busy = false }

type wlApp struct {
	display    *wl.Display
	registry   *wl.Registry
	compositor *wl.Compositor
	shm        *wl.Shm
	seat       *wl.Seat
	pointer    *wl.Pointer
	keyboard   *wl.Keyboard
	wmBase     *xdgshell.WmBase
	layerShell *layershell.ZwlrLayerShellV1
	copyMgr    *screencopy.ZwlrScreencopyManagerV1
	ddm        *wl.DataDeviceManager
	dataDevice *wl.DataDevice
	source     *wl.DataSource
	screens    []*wlScreen

	// The drag.
	on       *wlScreen // the screen the pointer is on
	dragging bool
	start    image.Point // in real pixels on `on`
	current  image.Point
	serial   uint32 // the newest input serial, needed to set the clipboard

	clipboard string
	done      bool
	cancelled bool
	failed    string
}

// runWayland takes one smugshot and then serves the clipboard for a while.
func runWayland() {
	app := &wlApp{}
	if err := app.connect(); err != nil {
		fmt.Fprintln(os.Stderr, "Smugshot:", err)
		notify("Smugshot", err.Error())
		os.Exit(1)
	}
	defer wlclient.DisplayDisconnect(app.display)

	ensureSettingsFile()
	os.MkdirAll(root(), 0o700)
	sweep()

	if err := app.capture(); err != nil {
		fmt.Fprintln(os.Stderr, "Smugshot:", err)
		notify("Smugshot could not take the picture", err.Error())
		os.Exit(1)
	}
	app.showOverlay()
	for !app.done && app.failed == "" {
		if !dispatch(app.display) {
			break
		}
	}
	if app.failed != "" {
		app.hideOverlay()
		fmt.Fprintln(os.Stderr, "Smugshot:", app.failed)
		notify("Smugshot", app.failed)
		os.Exit(1)
	}
	if app.cancelled {
		app.hideOverlay()
		return
	}
	// The files are written, and the clipboard is set, while the overlay is still up: the compositor only
	// accepts the clipboard from a program that has just been used, and our drag is what makes that true.
	if !app.finish() {
		app.hideOverlay()
		return
	}
	app.hideOverlay()
	// From here on this run only waits to hand the clipboard over, so the next smugshot may start.
	releaseLock()
	app.serveClipboard()
}

// dispatch handles one round of events. An event for an object that has already been let go is not worth
// stopping for: the compositor can still have one in flight for a buffer we destroyed. Only a broken
// connection ends the loop.
func dispatch(display *wl.Display) bool {
	err := wlclient.DisplayDispatch(display)
	switch {
	case err == nil:
		return true
	case errors.Is(err, wl.ErrContextRunProxyNil), errors.Is(err, wl.ErrContextRunNotDispatched):
		return true
	default:
		return false
	}
}

// MARK: Connecting

func (a *wlApp) connect() error {
	display, err := wlclient.DisplayConnect(nil)
	if err != nil {
		return fmt.Errorf("could not reach the Wayland compositor: %w", err)
	}
	a.display = display
	a.registry, err = wlclient.DisplayGetRegistry(display)
	if err != nil {
		return fmt.Errorf("no Wayland registry: %w", err)
	}
	wlclient.RegistryAddListener(a.registry, a)
	if err := wlclient.DisplayRoundtrip(display); err != nil {
		return err
	}
	// The screens answer their size in a second round.
	if err := wlclient.DisplayRoundtrip(display); err != nil {
		return err
	}
	switch {
	case a.compositor == nil || a.shm == nil:
		return fmt.Errorf("this compositor is missing wl_compositor or wl_shm")
	case len(a.screens) == 0:
		return fmt.Errorf("the compositor named no screens")
	case a.layerShell == nil && a.wmBase == nil:
		return fmt.Errorf("this compositor has neither layer-shell nor xdg-shell, so there is nowhere to draw")
	}
	if a.seat != nil {
		_ = wlclient.DisplayRoundtrip(display)
	}
	if a.pointer == nil {
		return fmt.Errorf("no mouse or touchpad was offered, so there is nothing to drag with")
	}
	if a.ddm != nil && a.seat != nil {
		if dd, err := a.ddm.GetDataDevice(a.seat); err == nil {
			a.dataDevice = dd
		}
	}
	return nil
}

func (a *wlApp) HandleRegistryGlobal(e wl.RegistryGlobalEvent) {
	switch e.Interface {
	case "wl_compositor":
		a.compositor = wlclient.RegistryBindCompositorInterface(a.registry, e.Name, min32(e.Version, 4))
	case "wl_shm":
		a.shm = wlclient.RegistryBindShmInterface(a.registry, e.Name, 1)
	case "wl_seat":
		a.seat = wlclient.RegistryBindSeatInterface(a.registry, e.Name, min32(e.Version, 5))
		// The listener goes on at once: the compositor says what the seat has as soon as it is bound, and an
		// event with nobody listening is simply dropped.
		wlclient.SeatAddListener(a.seat, a)
	case "wl_data_device_manager":
		a.ddm = wlclient.RegistryBindDataDeviceManagerInterface(a.registry, e.Name, min32(e.Version, 3))
	case "xdg_wm_base":
		a.wmBase = wlclient.RegistryBindWmBaseInterface(a.registry, e.Name, 1)
		a.wmBase.AddPingHandler(a)
	case "wl_output":
		output := wlclient.RegistryBindOutputInterface(a.registry, e.Name, min32(e.Version, 2))
		screen := &wlScreen{output: output, name: e.Name, scale: 1}
		a.screens = append(a.screens, screen)
		wlclient.OutputAddListener(output, screen)
	case "zwlr_layer_shell_v1":
		shell := layershell.NewZwlrLayerShellV1(a.display.Context())
		if a.registry.Bind(e.Name, "zwlr_layer_shell_v1", min32(e.Version, 4), shell) == nil {
			a.layerShell = shell
		}
	case "zwlr_screencopy_manager_v1":
		mgr := screencopy.NewZwlrScreencopyManagerV1(a.display.Context())
		if a.registry.Bind(e.Name, "zwlr_screencopy_manager_v1", min32(e.Version, 3), mgr) == nil {
			a.copyMgr = mgr
		}
	}
}

func (a *wlApp) HandleRegistryGlobalRemove(wl.RegistryGlobalRemoveEvent) {}

func (a *wlApp) HandleWmBasePing(e xdgshell.WmBasePingEvent) { _ = a.wmBase.Pong(e.Serial) }

func (a *wlApp) HandleSeatCapabilities(e wl.SeatCapabilitiesEvent) {
	if e.Capabilities&uint32(wl.SeatCapabilityPointer) != 0 && a.pointer == nil {
		if p, err := a.seat.GetPointer(); err == nil {
			a.pointer = p
			wlclient.PointerAddListener(p, a)
		}
	}
	if e.Capabilities&uint32(wl.SeatCapabilityKeyboard) != 0 && a.keyboard == nil {
		if k, err := a.seat.GetKeyboard(); err == nil {
			a.keyboard = k
			wlclient.KeyboardAddListener(k, a)
		}
	}
}

func (a *wlApp) HandleSeatName(wl.SeatNameEvent) {}

// MARK: Screens

func (s *wlScreen) HandleOutputGeometry(e wl.OutputGeometryEvent) { s.x, s.y = e.X, e.Y }

func (s *wlScreen) HandleOutputMode(e wl.OutputModeEvent) {
	if e.Flags&uint32(wl.OutputModeCurrent) != 0 {
		s.width, s.height = e.Width, e.Height
	}
}

func (s *wlScreen) HandleOutputScale(e wl.OutputScaleEvent) {
	if e.Factor > 0 {
		s.scale = e.Factor
	}
}

func (s *wlScreen) HandleOutputDone(wl.OutputDoneEvent) {}

// pixels is the screen's size in real pixels, which is what the picture and the buffers use.
func (s *wlScreen) pixels() (int, int) {
	if s.shot != nil {
		return s.shot.Bounds().Dx(), s.shot.Bounds().Dy()
	}
	return int(s.width), int(s.height)
}

// MARK: The drag layer

func (a *wlApp) showOverlay() {
	for _, s := range a.screens {
		if s.shot == nil {
			continue
		}
		surface, err := a.compositor.CreateSurface()
		if err != nil {
			a.failed = "could not make the drag layer: " + err.Error()
			return
		}
		s.surface = surface
		w, h := s.pixels()
		if a.layerShell != nil {
			layer, err := a.layerShell.GetLayerSurface(surface, s.output, layershell.ZwlrLayerShellV1LayerOverlay, "smugshot")
			if err != nil {
				a.failed = "could not make the drag layer: " + err.Error()
				return
			}
			s.layer = layer
			layer.AddConfigureHandler(&layerConfigure{app: a, screen: s})
			layer.AddClosedHandler(&layerClosed{app: a})
			// All four edges, so it covers the whole screen; keyboard, so Esc reaches us.
			_ = layer.SetAnchor(layershell.ZwlrLayerSurfaceV1AnchorTop | layershell.ZwlrLayerSurfaceV1AnchorBottom |
				layershell.ZwlrLayerSurfaceV1AnchorLeft | layershell.ZwlrLayerSurfaceV1AnchorRight)
			_ = layer.SetExclusiveZone(-1)
			_ = layer.SetKeyboardInteractivity(1)
			_ = layer.SetSize(uint32(s.width), uint32(s.height))
		} else {
			xdgSurf, err := a.wmBase.GetSurface(surface)
			if err != nil {
				a.failed = "could not make the drag layer: " + err.Error()
				return
			}
			s.xdgSurf = xdgSurf
			xdgSurf.AddConfigureHandler(&xdgConfigure{app: a, screen: s})
			top, err := xdgSurf.GetToplevel()
			if err != nil {
				a.failed = "could not make the drag layer: " + err.Error()
				return
			}
			s.xdgTop = top
			_ = top.SetTitle("Smugshot")
			_ = top.SetAppId("io.smugshot.Smugshot")
			_ = top.SetFullscreen(s.output)
		}
		_ = surface.SetBufferScale(s.scale)
		_ = surface.Commit()
		_ = w
		_ = h
	}
	_ = wlclient.DisplayRoundtrip(a.display)
}

type layerConfigure struct {
	app    *wlApp
	screen *wlScreen
}

func (c *layerConfigure) HandleZwlrLayerSurfaceV1Configure(e layershell.ZwlrLayerSurfaceV1ConfigureEvent) {
	_ = c.screen.layer.AckConfigure(e.Serial)
	c.app.firstPaint(c.screen)
}

type layerClosed struct{ app *wlApp }

func (c *layerClosed) HandleZwlrLayerSurfaceV1Closed(layershell.ZwlrLayerSurfaceV1ClosedEvent) {
	c.app.cancelled, c.app.done = true, true
}

type xdgConfigure struct {
	app    *wlApp
	screen *wlScreen
}

func (c *xdgConfigure) HandleSurfaceConfigure(e xdgshell.SurfaceConfigureEvent) {
	_ = c.screen.xdgSurf.AckConfigure(e.Serial)
	c.app.firstPaint(c.screen)
}

// firstPaint puts the dimmed frozen picture up, once the compositor has agreed to show the surface.
func (a *wlApp) firstPaint(s *wlScreen) {
	if s.ready {
		return
	}
	s.ready = true
	w, h := s.pixels()
	for i := range s.buffers {
		buf, err := a.newBuffer(w, h)
		if err != nil {
			a.failed = "could not make the drag layer's picture: " + err.Error()
			return
		}
		s.buffers[i] = buf
		paintDimmed(buf.data, s.shot)
		buf.drawn = image.Rectangle{}
	}
	a.paint(s, image.Rectangle{})
}

func (a *wlApp) newBuffer(w, h int) (*wlBuffer, error) {
	stride := w * 4
	size := stride * h
	fd, err := wlos.CreateAnonymousFile(int64(size))
	if err != nil {
		return nil, err
	}
	defer fd.Close()
	data, err := wlos.Mmap(int(fd.Fd()), 0, size, wlos.ProtRead|wlos.ProtWrite, wlos.MapShared)
	if err != nil {
		return nil, err
	}
	pool, err := a.shm.CreatePool(fd.Fd(), int32(size))
	if err != nil {
		return nil, err
	}
	defer pool.Destroy()
	buffer, err := pool.CreateBuffer(0, int32(w), int32(h), int32(stride), uint32(wl.ShmFormatXrgb8888))
	if err != nil {
		return nil, err
	}
	b := &wlBuffer{buffer: buffer, data: data}
	wlclient.BufferAddListener(buffer, b)
	return b, nil
}

// paint shows the selection undimmed inside the dimmed picture, with a frame around it, and asks the
// compositor to show only what changed.
func (a *wlApp) paint(s *wlScreen, selection image.Rectangle) {
	if !s.ready {
		return
	}
	buf := s.buffers[s.next]
	if buf == nil || buf.busy {
		// Both buffers are with the compositor; this frame is dropped, the next motion draws again.
		other := s.buffers[1-s.next]
		if other == nil || other.busy {
			return
		}
		s.next = 1 - s.next
		buf = other
	}
	w, h := s.pixels()
	bounds := image.Rect(0, 0, w, h)
	frame := 3
	old := buf.drawn
	repair := old.Inset(-frame).Intersect(bounds)
	now := selection.Intersect(bounds)
	draw := now.Inset(-frame).Intersect(bounds)

	if !repair.Empty() {
		repaintDimmed(buf.data, s.shot, repair)
	}
	if !now.Empty() {
		paintBright(buf.data, s.shot, now)
		paintFrame(buf.data, w, h, now, frame)
	}
	buf.drawn = now

	damage := repair.Union(draw)
	if damage.Empty() {
		damage = bounds
	}
	buf.busy = true
	_ = s.surface.Attach(buf.buffer, 0, 0)
	_ = s.surface.DamageBuffer(int32(damage.Min.X), int32(damage.Min.Y), int32(damage.Dx()), int32(damage.Dy()))
	_ = s.surface.Commit()
	s.next = 1 - s.next
}

func (a *wlApp) hideOverlay() {
	for _, s := range a.screens {
		if s.surface == nil {
			continue
		}
		if s.layer != nil {
			_ = s.layer.Destroy()
		}
		if s.xdgTop != nil {
			_ = s.xdgTop.Destroy()
		}
		if s.xdgSurf != nil {
			_ = s.xdgSurf.Destroy()
		}
		_ = s.surface.Destroy()
		s.surface = nil
	}
	_ = wlclient.DisplayRoundtrip(a.display)
}

// MARK: Drawing into a buffer. Wayland wants blue, green, red, then a spare byte.

func paintDimmed(dst []byte, src *image.RGBA) {
	for i, n := 0, len(src.Pix)/4; i < n; i++ {
		s, d := i*4, i*4
		if d+3 >= len(dst) {
			return
		}
		dst[d] = uint8(uint32(src.Pix[s+2]) * 82 / 100)
		dst[d+1] = uint8(uint32(src.Pix[s+1]) * 82 / 100)
		dst[d+2] = uint8(uint32(src.Pix[s]) * 82 / 100)
		dst[d+3] = 0xFF
	}
}

func repaintDimmed(dst []byte, src *image.RGBA, r image.Rectangle) {
	w := src.Bounds().Dx()
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			s, d := src.PixOffset(x, y), (y*w+x)*4
			if d+3 >= len(dst) || s+3 >= len(src.Pix) {
				continue
			}
			dst[d] = uint8(uint32(src.Pix[s+2]) * 82 / 100)
			dst[d+1] = uint8(uint32(src.Pix[s+1]) * 82 / 100)
			dst[d+2] = uint8(uint32(src.Pix[s]) * 82 / 100)
			dst[d+3] = 0xFF
		}
	}
}

func paintBright(dst []byte, src *image.RGBA, r image.Rectangle) {
	w := src.Bounds().Dx()
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			s, d := src.PixOffset(x, y), (y*w+x)*4
			if d+3 >= len(dst) || s+3 >= len(src.Pix) {
				continue
			}
			dst[d], dst[d+1], dst[d+2], dst[d+3] = src.Pix[s+2], src.Pix[s+1], src.Pix[s], 0xFF
		}
	}
}

// paintFrame draws the pink-red frame just outside the selection, so it never covers what was pointed at.
func paintFrame(dst []byte, w, h int, r image.Rectangle, width int) {
	outer := r.Inset(-width).Intersect(image.Rect(0, 0, w, h))
	for y := outer.Min.Y; y < outer.Max.Y; y++ {
		for x := outer.Min.X; x < outer.Max.X; x++ {
			if (image.Point{x, y}).In(r) {
				continue
			}
			d := (y*w + x) * 4
			if d+3 >= len(dst) {
				continue
			}
			dst[d], dst[d+1], dst[d+2], dst[d+3] = 0x55, 0x2D, 0xFF, 0xFF // #FF2D55 as blue, green, red
		}
	}
}

// MARK: Pointer and keyboard

func (a *wlApp) screenOf(surface *wl.Surface) *wlScreen {
	for _, s := range a.screens {
		if s.surface == surface {
			return s
		}
	}
	return nil
}

func (a *wlApp) HandlePointerEnter(e wl.PointerEnterEvent) {
	a.serial = e.Serial
	if s := a.screenOf(e.Surface); s != nil {
		a.on = s
	}
	a.current = a.pointAt(e.SurfaceX, e.SurfaceY)
	// No cursor of our own yet, and an empty one would hide the pointer: leave the compositor's.
}

func (a *wlApp) HandlePointerLeave(e wl.PointerLeaveEvent) { a.serial = e.Serial }

func (a *wlApp) HandlePointerMotion(e wl.PointerMotionEvent) {
	a.current = a.pointAt(e.SurfaceX, e.SurfaceY)
	if a.dragging && a.on != nil {
		a.paint(a.on, image.Rectangle{Min: a.start, Max: a.current}.Canon())
	}
}

func (a *wlApp) HandlePointerButton(e wl.PointerButtonEvent) {
	a.serial = e.Serial
	const left, right = 0x110, 0x111
	pressed := e.State == uint32(wl.PointerButtonStatePressed)
	switch {
	case e.Button == right && pressed:
		a.cancelled, a.done = true, true
	case e.Button == left && pressed:
		if a.on == nil {
			// The compositor has not said which screen the pointer is on, which can happen when it never moved
			// after the drag layer went up. The first screen is the best guess and beats doing nothing.
			for _, s := range a.screens {
				if s.shot != nil {
					a.on = s
					break
				}
			}
		}
		a.dragging = true
		a.start = a.current
	case e.Button == left && !pressed && a.dragging:
		a.dragging = false
		a.done = true
	}
}

func (a *wlApp) HandlePointerAxis(wl.PointerAxisEvent)                 {}
func (a *wlApp) HandlePointerFrame(wl.PointerFrameEvent)               {}
func (a *wlApp) HandlePointerAxisSource(wl.PointerAxisSourceEvent)     {}
func (a *wlApp) HandlePointerAxisStop(wl.PointerAxisStopEvent)         {}
func (a *wlApp) HandlePointerAxisDiscrete(wl.PointerAxisDiscreteEvent) {}
func (a *wlApp) HandlePointerAxisValue120(wl.PointerAxisValue120Event) {}

func (a *wlApp) HandleKeyboardKeymap(wl.KeyboardKeymapEvent) {}
func (a *wlApp) HandleKeyboardEnter(e wl.KeyboardEnterEvent) { a.serial = e.Serial }
func (a *wlApp) HandleKeyboardLeave(e wl.KeyboardLeaveEvent) { a.serial = e.Serial }

func (a *wlApp) HandleKeyboardKey(e wl.KeyboardKeyEvent) {
	a.serial = e.Serial
	if e.Key == escapeKey && e.State == uint32(wl.KeyboardKeyStatePressed) {
		a.cancelled, a.done = true, true
	}
}

func (a *wlApp) HandleKeyboardModifiers(e wl.KeyboardModifiersEvent) { a.serial = e.Serial }
func (a *wlApp) HandleKeyboardRepeatInfo(wl.KeyboardRepeatInfoEvent) {}

// pointAt turns Wayland's fixed-point surface coordinates into real pixels on the screen under the pointer.
func (a *wlApp) pointAt(x, y float32) image.Point {
	scale := 1
	if a.on != nil && a.on.scale > 1 {
		scale = int(a.on.scale)
	}
	return image.Pt(int(x)*scale, int(y)*scale)
}

// MARK: Writing the smugshot

// finish writes the files and puts the path on the clipboard. False when there was nothing to write.
func (a *wlApp) finish() bool {
	if a.on == nil || a.on.shot == nil {
		return false
	}
	region := image.Rectangle{Min: a.start, Max: a.current}.Canon().Intersect(a.on.shot.Bounds())
	if region.Dx() < 4 || region.Dy() < 4 {
		return false
	}
	at := time.Now()
	dir, err := newFolder(at)
	if err != nil {
		notify("Smugshot", "Could not write the smugshot: "+err.Error())
		return false
	}
	fullImg, fullRegion := full(a.on.shot, region)
	if writePNG(fullImg, filepath.Join(dir, "full.png")) != nil || writePNG(crop(a.on.shot, region), filepath.Join(dir, "crop.png")) != nil {
		notify("Smugshot", "Could not write the pictures.")
		return false
	}
	path := filepath.Join(dir, "shot.md")
	text := shotText(dir, at, "", "", fullRegion, fullImg.Bounds().Size(),
		"On Wayland no program may ask what is under the pointer, so this smugshot names no app or window.")
	os.WriteFile(path, []byte(strings.ReplaceAll(text, "\r\n", "\n")), 0o600)

	cfg := loadSettings()
	a.setClipboard(*cfg.Prefix + path)
	if cfg.soundOn() {
		playTink()
	}
	if cfg.bannerOn() {
		notify("Smugshot", "Copied. Paste it into the chat.")
	}
	sweep()
	return true
}

// MARK: Clipboard

// setClipboard offers the text to the desktop. The compositor only takes it from a program the person has just
// used, which is why this is called while the drag layer is still up, with the serial from the drag.
func (a *wlApp) setClipboard(text string) {
	a.clipboard = text
	if a.ddm == nil || a.dataDevice == nil || a.serial == 0 {
		a.copyWithTool(text)
		return
	}
	source, err := a.ddm.CreateDataSource()
	if err != nil {
		a.copyWithTool(text)
		return
	}
	a.source = source
	wlclient.DataSourceAddListener(source, a)
	for _, mime := range []string{"text/plain;charset=utf-8", "text/plain", "UTF8_STRING", "STRING"} {
		_ = source.Offer(mime)
	}
	if err := a.dataDevice.SetSelection(source, a.serial); err != nil {
		a.copyWithTool(text)
		return
	}
	_ = wlclient.DisplayRoundtrip(a.display)
}

// copyWithTool is the way out when the compositor will not take the clipboard from us: wl-copy, if it is there.
func (a *wlApp) copyWithTool(text string) {
	path, err := exec.LookPath("wl-copy")
	if err != nil {
		fmt.Fprintln(os.Stderr, "Smugshot: could not put the path on the clipboard, and wl-copy is not installed.")
		return
	}
	cmd := exec.Command(path)
	cmd.Stdin = strings.NewReader(text)
	_ = cmd.Start()
	a.source = nil
}

func (a *wlApp) HandleDataSourceSend(e wl.DataSourceSendEvent) {
	file := os.NewFile(uintptr(e.Fd), "clipboard")
	if file == nil {
		return
	}
	_, _ = file.WriteString(a.clipboard)
	_ = file.Close()
}

// HandleDataSourceCancelled fires when something else takes the clipboard: our text is gone, so we can stop.
func (a *wlApp) HandleDataSourceCancelled(wl.DataSourceCancelledEvent) { a.done = true }

func (a *wlApp) HandleDataSourceTarget(wl.DataSourceTargetEvent)                     {}
func (a *wlApp) HandleDataSourceAction(wl.DataSourceActionEvent)                     {}
func (a *wlApp) HandleDataSourceDndDropPerformed(wl.DataSourceDndDropPerformedEvent) {}
func (a *wlApp) HandleDataSourceDndFinished(wl.DataSourceDndFinishedEvent)           {}

// serveClipboard keeps the program alive so the paste works: on Wayland the text lives in the program that put
// it there, not in the system. It ends when something else takes the clipboard, or after a while.
func (a *wlApp) serveClipboard() {
	if a.source == nil {
		return
	}
	a.done = false
	go func() {
		time.Sleep(clipboardLinger)
		os.Exit(0)
	}()
	for !a.done {
		if !dispatch(a.display) {
			return
		}
	}
}

func min32(a, b uint32) uint32 {
	if a < b {
		return a
	}
	return b
}
