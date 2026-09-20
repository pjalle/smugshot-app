//go:build linux

// Taking the picture on Wayland. No program may simply read the screen, so this asks the compositor, in the
// order that works best:
//
//  1. The wlroots screencopy protocol (sway, Hyprland, river, labwc). Straight from the compositor, one picture
//     per screen, no dialog, exact pixels.
//  2. The desktop's own screenshot service over D-Bus (GNOME, KDE, and anything else with a portal). It hands
//     back one picture of the whole desktop, which is then cut up per screen. The first time, the desktop asks
//     the person whether Smugshot may do this, and remembers the answer.
//
// If neither is there, the person is told which package to install rather than left with a silent failure.
package main

import (
	"fmt"
	"image"
	"image/png"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/godbus/dbus/v5"
	wlos "github.com/neurlang/wayland/os"
	"github.com/neurlang/wayland/wl"
	"github.com/neurlang/wayland/wlclient"

	"github.com/pjalle/smugshot/windows/wlproto/screencopy"
)

// capture fills in every screen's frozen picture.
func (a *wlApp) capture() error {
	if a.copyMgr != nil {
		if err := a.captureWithScreencopy(); err == nil {
			return nil
		} else {
			fmt.Fprintln(os.Stderr, "Smugshot: the compositor's own screen copy did not work ("+err.Error()+"), asking the desktop instead")
		}
	}
	return a.captureWithPortal()
}

// MARK: The wlroots way

// frameGrab collects what the compositor says about one screen's picture while it is being copied.
type frameGrab struct {
	format uint32
	width  uint32
	height uint32
	stride uint32
	done   bool
	failed bool
	invert bool
}

func (f *frameGrab) HandleZwlrScreencopyFrameV1Buffer(e screencopy.ZwlrScreencopyFrameV1BufferEvent) {
	f.format, f.width, f.height, f.stride = e.Format, e.Width, e.Height, e.Stride
}

func (f *frameGrab) HandleZwlrScreencopyFrameV1BufferDone(screencopy.ZwlrScreencopyFrameV1BufferDoneEvent) {
}

func (f *frameGrab) HandleZwlrScreencopyFrameV1Flags(e screencopy.ZwlrScreencopyFrameV1FlagsEvent) {
	f.invert = e.Flags&screencopy.ZwlrScreencopyFrameV1FlagsYInvert != 0
}

func (f *frameGrab) HandleZwlrScreencopyFrameV1Ready(screencopy.ZwlrScreencopyFrameV1ReadyEvent) {
	f.done = true
}

func (f *frameGrab) HandleZwlrScreencopyFrameV1Failed(screencopy.ZwlrScreencopyFrameV1FailedEvent) {
	f.failed, f.done = true, true
}

func (f *frameGrab) HandleZwlrScreencopyFrameV1Damage(screencopy.ZwlrScreencopyFrameV1DamageEvent) {}
func (f *frameGrab) HandleZwlrScreencopyFrameV1LinuxDmabuf(screencopy.ZwlrScreencopyFrameV1LinuxDmabufEvent) {
}

func (a *wlApp) captureWithScreencopy() error {
	for _, s := range a.screens {
		frame, err := a.copyMgr.CaptureOutput(0, s.output) // 0: leave the pointer out of the picture
		if err != nil {
			return err
		}
		grab := &frameGrab{}
		frame.AddBufferHandler(grab)
		frame.AddBufferDoneHandler(grab)
		frame.AddFlagsHandler(grab)
		frame.AddReadyHandler(grab)
		frame.AddFailedHandler(grab)

		// The compositor first says what size and shape it wants; then we hand it somewhere to write.
		if err := wlclient.DisplayRoundtrip(a.display); err != nil {
			return err
		}
		if grab.width == 0 || grab.height == 0 {
			return fmt.Errorf("the compositor offered no picture for a screen")
		}
		size := int(grab.stride) * int(grab.height)
		fd, err := wlos.CreateAnonymousFile(int64(size))
		if err != nil {
			return err
		}
		data, err := wlos.Mmap(int(fd.Fd()), 0, size, wlos.ProtRead|wlos.ProtWrite, wlos.MapShared)
		if err != nil {
			fd.Close()
			return err
		}
		pool, err := a.shm.CreatePool(fd.Fd(), int32(size))
		if err != nil {
			fd.Close()
			return err
		}
		buffer, err := pool.CreateBuffer(0, int32(grab.width), int32(grab.height), int32(grab.stride), grab.format)
		pool.Destroy()
		fd.Close()
		if err != nil {
			return err
		}
		if err := frame.Copy(buffer); err != nil {
			return err
		}
		deadline := time.Now().Add(5 * time.Second)
		for !grab.done {
			if time.Now().After(deadline) {
				return fmt.Errorf("the compositor did not hand over the picture in time")
			}
			if !dispatch(a.display) {
				return fmt.Errorf("the connection to the compositor broke while taking the picture")
			}
		}
		if grab.failed {
			return fmt.Errorf("the compositor refused to copy the screen")
		}
		s.shot = imageFromBuffer(data, int(grab.width), int(grab.height), int(grab.stride), grab.format, grab.invert)
		// The buffer is left alone on purpose: the compositor may still send a release for it, and letting it
		// go now would leave that event pointing at nothing.
		_ = frame.Destroy()
		if s.shot == nil {
			return fmt.Errorf("the compositor used a picture format Smugshot does not know (%d)", grab.format)
		}
	}
	return nil
}

// imageFromBuffer turns what the compositor wrote into an ordinary picture. Wayland's usual formats put blue
// first; the odd ones are left alone, and the caller says so.
func imageFromBuffer(data []byte, w, h, stride int, format uint32, invert bool) *image.RGBA {
	var swap bool
	switch format {
	case uint32(wl.ShmFormatXrgb8888), uint32(wl.ShmFormatArgb8888):
		swap = true // blue, green, red in memory
	case uint32(wl.ShmFormatXbgr8888), uint32(wl.ShmFormatAbgr8888):
		swap = false
	default:
		return nil
	}
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		src := y * stride
		dstY := y
		if invert {
			dstY = h - 1 - y
		}
		for x := 0; x < w; x++ {
			s := src + x*4
			if s+3 >= len(data) {
				continue
			}
			d := img.PixOffset(x, dstY)
			if swap {
				img.Pix[d], img.Pix[d+1], img.Pix[d+2] = data[s+2], data[s+1], data[s]
			} else {
				img.Pix[d], img.Pix[d+1], img.Pix[d+2] = data[s], data[s+1], data[s+2]
			}
			img.Pix[d+3] = 0xFF
		}
	}
	return img
}

// MARK: The desktop's own screenshot service

// captureWithPortal asks org.freedesktop.portal.Screenshot for one picture of the whole desktop and cuts it up
// per screen. The first call may put a question on screen; the desktop remembers the answer.
func (a *wlApp) captureWithPortal() error {
	bus, err := dbus.SessionBus()
	if err != nil {
		return fmt.Errorf("no desktop bus, so there is no way to ask for a picture: %w", err)
	}
	portal := bus.Object("org.freedesktop.portal.Desktop", dbus.ObjectPath("/org/freedesktop/portal/desktop"))
	if err := portal.Call("org.freedesktop.DBus.Properties.Get", 0, "org.freedesktop.portal.Screenshot", "version").Err; err != nil {
		return fmt.Errorf("this desktop has no screenshot service: install xdg-desktop-portal and the one for your desktop " +
			"(xdg-desktop-portal-gnome, -kde or -wlr)")
	}

	// The answer comes back as a signal on a Request object, so listen before asking.
	if err := bus.AddMatchSignal(dbus.WithMatchInterface("org.freedesktop.portal.Request"),
		dbus.WithMatchMember("Response")); err != nil {
		return err
	}
	signals := make(chan *dbus.Signal, 8)
	bus.Signal(signals)
	defer bus.RemoveSignal(signals)

	token := fmt.Sprintf("smugshot%d", time.Now().UnixNano())
	options := map[string]dbus.Variant{
		"handle_token": dbus.MakeVariant(token),
		"interactive":  dbus.MakeVariant(false),
		"modal":        dbus.MakeVariant(false),
	}
	var request dbus.ObjectPath
	if err := portal.Call("org.freedesktop.portal.Screenshot.Screenshot", 0, "", options).Store(&request); err != nil {
		return fmt.Errorf("the desktop's screenshot service refused: %w", err)
	}

	deadline := time.After(60 * time.Second)
	for {
		select {
		case signal := <-signals:
			if signal == nil || signal.Path != request || len(signal.Body) < 2 {
				continue
			}
			code, _ := signal.Body[0].(uint32)
			if code != 0 {
				return fmt.Errorf("the desktop did not allow the picture (you can allow Smugshot in your desktop's privacy settings)")
			}
			results, _ := signal.Body[1].(map[string]dbus.Variant)
			uri, _ := results["uri"].Value().(string)
			if uri == "" {
				return fmt.Errorf("the desktop allowed the picture but named no file")
			}
			return a.sliceDesktop(uri)
		case <-deadline:
			return fmt.Errorf("the desktop's screenshot service did not answer")
		}
	}
}

// sliceDesktop reads the whole-desktop picture the portal wrote and gives each screen its part.
func (a *wlApp) sliceDesktop(uri string) error {
	parsed, err := url.Parse(uri)
	if err != nil {
		return err
	}
	path := parsed.Path
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("could not read the picture the desktop wrote: %w", err)
	}
	defer file.Close()
	defer os.Remove(path) // the portal writes it into the person's own folder; do not leave it lying about
	decoded, err := png.Decode(file)
	if err != nil {
		return fmt.Errorf("could not read the picture the desktop wrote: %w", err)
	}
	whole := image.NewRGBA(decoded.Bounds())
	for y := decoded.Bounds().Min.Y; y < decoded.Bounds().Max.Y; y++ {
		for x := decoded.Bounds().Min.X; x < decoded.Bounds().Max.X; x++ {
			whole.Set(x, y, decoded.At(x, y))
		}
	}

	// One screen is the usual case, and then the picture is simply that screen.
	if len(a.screens) == 1 {
		a.screens[0].shot = whole
		return nil
	}
	for _, s := range a.screens {
		scale := int(s.scale)
		if scale < 1 {
			scale = 1
		}
		r := image.Rect(int(s.x)*scale, int(s.y)*scale,
			(int(s.x)+int(s.width))*scale, (int(s.y)+int(s.height))*scale).Intersect(whole.Bounds())
		if r.Empty() {
			continue
		}
		part := image.NewRGBA(image.Rect(0, 0, r.Dx(), r.Dy()))
		for y := 0; y < r.Dy(); y++ {
			src := whole.PixOffset(r.Min.X, r.Min.Y+y)
			copy(part.Pix[part.PixOffset(0, y):], whole.Pix[src:src+r.Dx()*4])
		}
		s.shot = part
	}
	for _, s := range a.screens {
		if s.shot != nil {
			return nil
		}
	}
	return fmt.Errorf("the picture the desktop gave did not line up with any screen")
}

// onWayland says whether this is a Wayland session rather than an X11 one.
func onWayland() bool {
	if os.Getenv("WAYLAND_DISPLAY") != "" {
		return true
	}
	return strings.EqualFold(os.Getenv("XDG_SESSION_TYPE"), "wayland")
}
