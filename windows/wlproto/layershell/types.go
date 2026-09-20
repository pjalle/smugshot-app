// Package layershell is the wlroots layer-shell protocol: it puts a surface above every ordinary window.
// The .go file next to this one is generated from the protocol's XML by the wayland-scanner; see doc.go.
package layershell

import (
	"github.com/neurlang/wayland/wl"
	xdgshell "github.com/neurlang/wayland/xdg"
)

type BaseProxy = wl.BaseProxy
type Context = wl.Context
type Event = wl.Event
type Output = wl.Output
type Surface = wl.Surface
type XdgPopup = xdgshell.Popup
