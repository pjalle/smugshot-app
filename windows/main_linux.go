//go:build linux

// Smugshot on Linux: press the shortcut, drag, paste the path. The same gesture and the same files as the Mac
// and Windows versions, but which half runs depends on the session:
//
//   - X11 (x11_linux.go): one program that stays running, holds the shortcut for the whole desktop, and takes a
//     smugshot each time it is pressed. This is the version that behaves like the Windows one.
//   - Wayland (wayland_linux.go): one run is one smugshot, because Wayland lets no program hold a shortcut for
//     the whole desktop. You bind `smugshot` to a key in your own desktop settings; --help says how.
//
// Not here yet on either: a tray icon, a Settings window, naming the control under the drag, its text, and the
// browser element.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

func main() {
	for _, arg := range os.Args[1:] {
		switch arg {
		case "-h", "--help", "help":
			usage()
			return
		default:
			fmt.Fprintln(os.Stderr, "Smugshot: I do not know the option "+arg)
			usage()
			os.Exit(2)
		}
	}

	if onWayland() {
		// One run is one smugshot, so two presses at once would otherwise put two drag layers up.
		if !lockSingleInstance("smugshot-shot.lock") {
			return
		}
		runWayland()
		return
	}
	if !lockSingleInstance("smugshot.lock") {
		fmt.Fprintln(os.Stderr, "Smugshot is already running.")
		return
	}
	runX11()
}

func usage() {
	fmt.Println(`Smugshot: point at part of your screen so an AI agent can see what you mean.

  smugshot         X11: stay running and hold the shortcut.
                   Wayland: take one smugshot now.
  smugshot --help  this text

On Wayland bind "smugshot" to a key in your desktop's keyboard settings:
  GNOME     Settings > Keyboard > View and Customize Shortcuts > Custom Shortcuts
  KDE       System Settings > Shortcuts > Add Command
  sway      bindsym Print exec smugshot
  Hyprland  bind = , Print, exec, smugshot

Settings are a small file at ~/.config/Smugshot/settings.json, written the first time it runs.`)
}

// lockFile stays open for as long as this process lives; the lock goes with it. Kept here so the garbage
// collector does not close it.
var lockFile *os.File

// releaseLock lets the next smugshot start. The Wayland half calls it once the drag layer is down: after that
// it only lingers to serve the clipboard, which must not stop the next smugshot.
func releaseLock() {
	if lockFile != nil {
		syscall.Flock(int(lockFile.Fd()), syscall.LOCK_UN)
		lockFile.Close()
		lockFile = nil
	}
}

// lockSingleInstance takes the named lock. A second copy gets false.
func lockSingleInstance(name string) bool {
	dir := os.Getenv("XDG_RUNTIME_DIR")
	if dir == "" {
		dir = os.TempDir()
	}
	f, err := os.OpenFile(filepath.Join(dir, name), os.O_CREATE|os.O_RDWR, 0o600)
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

// MARK: Sound and messages, shared by both halves

// playTink plays the bundled sound through whichever player the desktop has. Silent when there is none.
func playTink() {
	path := filepath.Join(os.TempDir(), "smugshot-tink.wav")
	if data, err := os.ReadFile(path); err != nil || len(data) != len(tinkWAV) {
		os.WriteFile(path, tinkWAV, 0o600)
	}
	for _, player := range [][]string{{"paplay", path}, {"pw-play", path}, {"aplay", "-q", path}} {
		if _, err := exec.LookPath(player[0]); err == nil {
			exec.Command(player[0], player[1:]...).Start()
			return
		}
	}
}

// notify shows a desktop notification. Nothing happens when notify-send is not installed.
func notify(title, body string) {
	if _, err := exec.LookPath("notify-send"); err == nil {
		exec.Command("notify-send", "-a", "Smugshot", "-t", "2500", title, body).Start()
	}
}
