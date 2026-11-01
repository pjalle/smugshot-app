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
	gotApp, gotWindow := false, false // a marked-up smugshot lists one per spot; the first one names it
	for _, l := range strings.Split(string(data), "\n") {
		l = strings.TrimRight(l, "\r")
		if strings.HasPrefix(l, "- Spots: ") {
			line += "  " + strings.TrimPrefix(l, "- Spots: ") + " spots"
		} else if strings.HasPrefix(l, "- App: ") && !gotApp {
			line += "  " + strings.TrimPrefix(l, "- App: ")
			gotApp = true
		} else if strings.HasPrefix(l, "- Window: ") && !gotWindow {
			line += ": " + strings.TrimPrefix(l, "- Window: ")
			gotWindow = true
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

// textSection is the part of shot.md with the text read from the close-up, worded as on the Mac, or "" when
// nothing was read. It goes in as shotText's note.
func textSection(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	section := []string{"## Text read from the close-up", "Recognised from the picture, so expect small mistakes.", "```"}
	section = append(section, lines...)
	section = append(section, "```")
	return strings.Join(section, "\r\n")
}

// ocrLines turns what the text reader printed into lines: in reading order as they came, without trailing
// spaces or empty lines. A byte-order mark in front is dropped; Windows PowerShell can write one.
func ocrLines(out string) []string {
	var lines []string
	for _, l := range strings.Split(strings.TrimPrefix(out, "\ufeff"), "\n") {
		if l = strings.TrimRight(l, " \t\r"); l != "" {
			lines = append(lines, l)
		}
	}
	return lines
}

// A spot in a marked-up smugshot: one drag and the note typed for it. Crop and Full are file names in the
// smugshot's folder, numbered from 01.
type spot struct {
	Crop, Full  string
	Region      image.Rectangle // in the full picture
	Size        image.Point     // of the full picture
	App, Window string
	Note        string
	Text        []string // read from the close-up, or nil
}

// spotFiles are the file names for spot n, counted from 1.
func spotFiles(n int) (crop, full string) {
	return fmt.Sprintf("crop-%02d.png", n), fmt.Sprintf("full-%02d.png", n)
}

// markupText is shot.md for a marked-up smugshot: the spots in the order they were dragged, each with its
// note, its two pictures and the text read from its close-up.
func markupText(dir string, at time.Time, spots []spot) string {
	n := len(spots)
	word := "spots"
	if n == 1 {
		word = "spot"
	}
	lines := []string{
		"# Smugshot",
		fmt.Sprintf("The user marked %d %s on their screen, typed a note for each, and pasted this path so you can see what they mean.", n, word),
		"Take the spots in order. For each, read the note, open its close-up (sharp, the spot is outlined), then its whole screen (the spot outlined, the rest dimmed) when you need the surroundings.",
		"",
		"- When: " + at.Format("2006-01-02 15:04:05"),
		fmt.Sprintf("- Spots: %d", n),
	}
	for i, s := range spots {
		lines = append(lines, "", fmt.Sprintf("## Spot %d", i+1))
		note := strings.TrimSpace(strings.ReplaceAll(s.Note, "\r\n", "\n"))
		if note == "" {
			lines = append(lines, "No note, only the spot.")
		} else {
			lines = append(lines, "The user's note:")
			for _, l := range strings.Split(note, "\n") {
				lines = append(lines, strings.TrimRight("> "+l, " "))
			}
		}
		lines = append(lines, "",
			"- Close-up: "+filepath.Join(dir, s.Crop),
			"- Whole screen: "+filepath.Join(dir, s.Full))
		if s.App != "" {
			lines = append(lines, "- App: "+s.App)
		}
		if s.Window != "" {
			lines = append(lines, "- Window: "+s.Window)
		}
		lines = append(lines, fmt.Sprintf("- Region in %s (pixels): x %d, y %d, w %d, h %d of %dx%d",
			s.Full, s.Region.Min.X, s.Region.Min.Y, s.Region.Dx(), s.Region.Dy(), s.Size.X, s.Size.Y))
		if len(s.Text) > 0 {
			lines = append(lines, "", "### Text read from the close-up", "Recognised from the picture, so expect small mistakes.", "```")
			lines = append(lines, s.Text...)
			lines = append(lines, "```")
		}
	}
	return strings.Join(lines, "\r\n") + "\r\n"
}

// markupHint is the line at the top of the drag layer while marking up: which spot is next and how to stop.
// paste is the usual pasting line, or "".
func markupHint(next int, paste string) string {
	line := fmt.Sprintf("Marking up: drag spot %d · Esc when done", next)
	if next == 1 {
		line = "Marking up: drag the first spot · Esc to stop"
	}
	if paste != "" {
		line += " · " + paste
	}
	return line
}

// noteLabelText is the line above the note box for spot n.
func noteLabelText(n int) string {
	return fmt.Sprintf("Spot %d: what about it?  Enter: next spot · Shift+Enter: new line\r\nCtrl+Enter: done · Esc: drop this spot", n)
}
