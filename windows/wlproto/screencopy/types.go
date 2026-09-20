// Package screencopy is the wlroots screencopy protocol: it asks the compositor for a picture of an output.
// The .go file next to this one is generated from the protocol's XML by the wayland-scanner; see doc.go.
// This file ties the generated code to the shared Wayland types, the way the library's own protocols do.
package screencopy

import "github.com/neurlang/wayland/wl"

type BaseProxy = wl.BaseProxy
type Context = wl.Context
type Event = wl.Event
type Output = wl.Output
type Buffer = wl.Buffer
