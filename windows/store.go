package main

import (
	"fmt"
	"image"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// A smugshot folder is named 2006-01-02-150405, or 2006-01-02-150405-2 when two land in the same second.
// The sweep only ever deletes folders named like this, so a user-chosen folder that holds other things is safe.
var folderName = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}-\d{6}(-\d+)?$`)

func root() string { return loadSettings().Folder }

func newFolder(at time.Time) (string, error) {
	base := filepath.Join(root(), at.Format("2006-01-02-150405"))
	dir := base
	for n := 2; ; n++ {
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			break
		}
		dir = fmt.Sprintf("%s-%d", base, n)
	}
	return dir, os.MkdirAll(dir, 0o700)
}

// sweep deletes smugshots older than the keepFor setting.
func sweep() {
	s := loadSettings()
	lifetime, forever, err := parseKeepFor(s.KeepFor)
	if err != nil || forever {
		return
	}
	sweepFolder(s.Folder, lifetime, time.Now())
}

func sweepFolder(dir string, lifetime time.Duration, now time.Time) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() || !folderName.MatchString(e.Name()) {
			continue
		}
		if info, err := e.Info(); err == nil && now.Sub(info.ModTime()) > lifetime {
			os.RemoveAll(filepath.Join(dir, e.Name()))
		}
	}
}

// recent lists the newest smugshot folders, newest first. The folder names sort by time.
func recent(dir string, limit int) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() && folderName.MatchString(e.Name()) {
			names = append(names, e.Name())
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(names)))
	if len(names) > limit {
		names = names[:limit]
	}
	var dirs []string
	for _, n := range names {
		dirs = append(dirs, filepath.Join(dir, n))
	}
	return dirs
}

// summary is one line for the menu, read back from shot.md: "14:32  brave.exe: Quill".
func summary(dir string) string {
	name := filepath.Base(dir) // 2006-01-02-150405[-n]
	line := name
	if len(name) >= 17 {
		line = name[11:13] + ":" + name[13:15]
	}
	data, _ := os.ReadFile(filepath.Join(dir, "shot.md"))
	for _, l := range strings.Split(string(data), "\n") {
		l = strings.TrimRight(l, "\r")
		if strings.HasPrefix(l, "- App: ") {
			line += "  " + strings.TrimPrefix(l, "- App: ")
		} else if strings.HasPrefix(l, "- Window: ") {
			line += ": " + strings.TrimPrefix(l, "- Window: ")
		}
	}
	if r := []rune(line); len(r) > 70 {
		line = string(r[:69]) + "\u2026"
	}
	return line
}

// note is an extra line for things the reader should know about this smugshot, or "" for none.
func shotText(dir string, at time.Time, app, window string, region image.Rectangle, size image.Point, note string) string {
	lines := []string{
		"# Smugshot",
		"The user pointed at part of their screen and pasted this path so you can see what they mean.",
		"Open crop.png first (sharp close-up, the pointed-at part is outlined), then full.png (whole screen, the pointed-at part is outlined and the rest is dimmed).",
		"",
		"- Close-up: " + filepath.Join(dir, "crop.png"),
		"- Whole screen: " + filepath.Join(dir, "full.png"),
		"- When: " + at.Format("2006-01-02 15:04:05"),
	}
	if app != "" {
		lines = append(lines, "- App: "+app)
	}
	if window != "" {
		lines = append(lines, "- Window: "+window)
	}
	lines = append(lines, fmt.Sprintf("- Region in full.png (pixels): x %d, y %d, w %d, h %d of %dx%d",
		region.Min.X, region.Min.Y, region.Dx(), region.Dy(), size.X, size.Y))
	if note != "" {
		lines = append(lines, "", note)
	}
	return strings.Join(lines, "\r\n") + "\r\n"
}
