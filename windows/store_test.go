package main

import (
	"image"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseKeepFor(t *testing.T) {
	cases := []struct {
		in      string
		want    time.Duration
		forever bool
		bad     bool
	}{
		{"1m", time.Minute, false, false},
		{"5m", 5 * time.Minute, false, false},
		{"1h", time.Hour, false, false},
		{"1d", 24 * time.Hour, false, false},
		{"1w", 7 * 24 * time.Hour, false, false},
		{" 90s ", 90 * time.Second, false, false},
		{"Forever", 0, true, false},
		{"after-pasting", 0, false, true},
		{"", 0, false, true},
		{"soon", 0, false, true},
		{"0m", 0, false, true},
		{"5", 0, false, true},
	}
	for _, c := range cases {
		got, forever, err := parseKeepFor(c.in)
		if (err != nil) != c.bad || forever != c.forever || got != c.want {
			t.Errorf("parseKeepFor(%q) = %v, %v, %v; want %v, %v, bad=%v", c.in, got, forever, err, c.want, c.forever, c.bad)
		}
	}
}

func TestParseHotkey(t *testing.T) {
	cases := []struct {
		in        string
		modifiers uint32
		key       uint32
		bad       bool
	}{
		{"ctrl+shift+1", 0x6, '1', false},
		{"Ctrl + Alt + S", 0x3, 'S', false},
		{"win+shift+f9", 0xC, 0x78, false},
		{"alt+f24", 0x1, 0x87, false},
		{"shift+1", 0, 0, true}, // shift alone cannot be taken from every app
		{"ctrl+", 0, 0, true},
		{"1", 0, 0, true},
		{"ctrl+f25", 0, 0, true},
		{"ctrl+home", 0, 0, true},
		{"meta+a", 0, 0, true},
	}
	for _, c := range cases {
		m, k, err := parseHotkey(c.in)
		if (err != nil) != c.bad || m != c.modifiers || k != c.key {
			t.Errorf("parseHotkey(%q) = %#x, %#x, %v; want %#x, %#x, bad=%v", c.in, m, k, err, c.modifiers, c.key, c.bad)
		}
	}
}

func TestParseSettingsDefaults(t *testing.T) {
	for _, data := range []string{"", "not json", "{}", `{"keepFor": "soon", "hotkey": "shift+1"}`} {
		s := parseSettings([]byte(data))
		if s.Folder != defaultFolder() || s.KeepFor != defaultKeepFor || s.Prefix == nil || *s.Prefix != defaultPrefix || s.Hotkey != defaultHotkey {
			t.Errorf("parseSettings(%q) = %+v; want the defaults", data, s)
		}
	}
}

func TestParseSettingsValues(t *testing.T) {
	s := parseSettings([]byte(`{"folder": "C:\\shots", "keepFor": "5m", "prefix": ""}`))
	if s.Folder != `C:\shots` || s.KeepFor != "5m" || s.Prefix == nil || *s.Prefix != "" {
		t.Errorf("got %+v", s)
	}
	home, _ := os.UserHomeDir()
	s = parseSettings([]byte(`{"folder": "~/shots", "prefix": "look: "}`))
	if s.Folder != filepath.Join(home, "shots") || *s.Prefix != "look: " {
		t.Errorf("got %+v", s)
	}
	if !s.soundOn() || !s.bannerOn() {
		t.Errorf("sound and banner should be on when the keys are missing")
	}
	s = parseSettings([]byte(`{"sound": false, "banner": false}`))
	if s.soundOn() || s.bannerOn() {
		t.Errorf("sound and banner should be off when set to false")
	}
}

func TestSoundSetting(t *testing.T) {
	cases := []struct {
		data string
		on   bool
		name string
	}{
		{`{}`, true, "feed"},
		{`{"sound": true}`, true, "feed"},
		{`{"sound": false}`, false, "off"},
		{`{"sound": "off"}`, false, "off"},
		{`{"sound": "Menu"}`, true, "menu"},
		{`{"sound": "tink"}`, true, "tink"},
		{`{"sound": "no such sound"}`, true, "feed"},
		{`{"sound": 3}`, true, "feed"}, // not a bool, not a string: the file is treated as broken, so the defaults
	}
	for _, c := range cases {
		s := parseSettings([]byte(c.data))
		if s.soundOn() != c.on || s.soundName() != c.name {
			t.Errorf("parseSettings(%s): on %v name %q; want on %v name %q", c.data, s.soundOn(), s.soundName(), c.on, c.name)
		}
	}
	for _, snd := range sounds {
		if snd.Name != strings.ToLower(snd.Name) || snd.Title == "" {
			t.Errorf("sound %+v: names are lower case and titles are not empty", snd)
		}
	}
}

func TestSweepFolderOnlyTouchesSmugshots(t *testing.T) {
	dir := t.TempDir()
	old := time.Now().Add(-2 * time.Hour)
	for _, name := range []string{"2026-09-17-143207", "2026-09-17-143207-2", "Documents", "2026-09-17", "notes.txt"} {
		p := filepath.Join(dir, name)
		if name == "notes.txt" {
			os.WriteFile(p, []byte("keep"), 0o600)
		} else {
			os.Mkdir(p, 0o700)
		}
		os.Chtimes(p, old, old)
	}
	os.Mkdir(filepath.Join(dir, "2026-09-17-160000"), 0o700) // fresh, stays

	sweepFolder(dir, time.Hour, time.Now())

	for _, name := range []string{"Documents", "2026-09-17", "notes.txt", "2026-09-17-160000"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("%s should have been left alone", name)
		}
	}
	for _, name := range []string{"2026-09-17-143207", "2026-09-17-143207-2"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			t.Errorf("%s should have been deleted", name)
		}
	}
}

func TestRecentAndSummary(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"2026-09-17-143207", "2026-09-18-090000", "2026-09-16-120000", "Documents"} {
		os.Mkdir(filepath.Join(dir, name), 0o700)
	}
	os.WriteFile(filepath.Join(dir, "2026-09-18-090000", "shot.md"), []byte("# Smugshot\r\n- App: brave.exe\r\n- Window: Quill\r\n"), 0o600)

	got := recent(dir, 2)
	if len(got) != 2 || filepath.Base(got[0]) != "2026-09-18-090000" || filepath.Base(got[1]) != "2026-09-17-143207" {
		t.Errorf("recent = %v", got)
	}
	if s := summary(got[0]); s != "09:00  brave.exe: Quill" {
		t.Errorf("summary = %q", s)
	}
	if s := summary(got[1]); s != "14:32" {
		t.Errorf("summary without shot.md = %q", s)
	}
}

func TestSettingsTemplateParses(t *testing.T) {
	s := parseSettings([]byte(settingsTemplate))
	if s.Folder != defaultFolder() || s.KeepFor != "1d" || *s.Prefix != defaultPrefix || s.Hotkey != defaultHotkey {
		t.Errorf("the template should give the defaults, got %+v", s)
	}
}

func TestHotkeyLabel(t *testing.T) {
	for in, want := range map[string]string{
		"ctrl+shift+1":  "Ctrl + Shift + 1",
		"ctrl+alt+s":    "Ctrl + Alt + S",
		"win+shift+f9":  "Win + Shift + F9",
		" Ctrl + Alt+s": "Ctrl + Alt + S",
	} {
		if got := hotkeyLabel(in); got != want {
			t.Errorf("hotkeyLabel(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTextSection(t *testing.T) {
	if got := textSection(nil); got != "" {
		t.Errorf("no lines should give no section, got %q", got)
	}
	want := "## Text read from the close-up\r\nRecognised from the picture, so expect small mistakes.\r\n```\r\nSave\r\nCancel\r\n```"
	if got := textSection([]string{"Save", "Cancel"}); got != want {
		t.Errorf("textSection = %q, want %q", got, want)
	}
	text := shotText(`C:\x`, time.Date(2026, 9, 30, 9, 0, 0, 0, time.UTC), "Code.exe", "main.go", image.Rect(1, 2, 11, 22), image.Pt(2000, 1250), textSection([]string{"func main() {"}))
	if !strings.Contains(text, "of 2000x1250\r\n\r\n## Text read from the close-up\r\n") || !strings.HasSuffix(text, "func main() {\r\n```\r\n") {
		t.Errorf("the section should follow a blank line at the end of shot.md:\n%s", text)
	}
}

func TestOCRLines(t *testing.T) {
	got := ocrLines("\ufeffFile  Edit \r\n\r\n   \r\n  indented stays\r\nlast")
	want := []string{"File  Edit", "  indented stays", "last"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("ocrLines = %q, want %q", got, want)
	}
	if got := ocrLines(""); len(got) != 0 {
		t.Errorf("nothing printed should give no lines, got %q", got)
	}
}

func TestReadTextAndPasteIntoSettings(t *testing.T) {
	cases := []struct {
		data      string
		readText  bool
		pasteOn   bool
		pasteInto string
	}{
		{`{}`, true, false, ""},
		{`{"readText": false}`, false, false, ""},
		{`{"readText": true, "pasteInto": "off"}`, true, false, "off"},
		{`{"pasteInto": " Off "}`, true, false, "off"},
		{`{"pasteInto": "Previous"}`, true, true, "previous"},
		{`{"pasteInto": "WindowsTerminal.exe"}`, true, true, "WindowsTerminal.exe"},
		{`{"pasteInto": 3}`, true, false, ""}, // the file is treated as broken, so the defaults
	}
	for _, c := range cases {
		s := parseSettings([]byte(c.data))
		if s.readTextOn() != c.readText || s.pasteOn() != c.pasteOn || s.PasteInto != c.pasteInto {
			t.Errorf("parseSettings(%s): readText %v, pasteOn %v, pasteInto %q; want %v, %v, %q",
				c.data, s.readTextOn(), s.pasteOn(), s.PasteInto, c.readText, c.pasteOn, c.pasteInto)
		}
	}
	s := parseSettings([]byte(settingsTemplate))
	if !s.readTextOn() || s.pasteOn() {
		t.Errorf("the template should read text and not paste, got %+v", s)
	}
}

func TestPasteChoices(t *testing.T) {
	for value, title := range map[string]string{
		"":                    "Off",
		"off":                 "Off",
		"previous":            "The app I came from",
		"WindowsTerminal.exe": "Windows Terminal (WindowsTerminal.exe)",
		"code.exe":            "Visual Studio Code (Code.exe)",
		"hx.exe":              "hx.exe",
	} {
		if got := pasteChoiceTitle(value); got != title {
			t.Errorf("pasteChoiceTitle(%q) = %q, want %q", value, got, title)
		}
	}
	for text, value := range map[string]string{
		"":                                       "off",
		"Off":                                    "off",
		"The app I came from":                    "previous",
		"previous":                               "previous",
		"Windows Terminal (WindowsTerminal.exe)": "WindowsTerminal.exe",
		"cursor":                                 "Cursor.exe",
		" hx.exe ":                               "hx.exe",
	} {
		if got := pasteChoiceValue(text); got != value {
			t.Errorf("pasteChoiceValue(%q) = %q, want %q", text, got, value)
		}
	}
	for _, p := range knownPrograms { // every list entry comes back as the program it names
		if got := pasteChoiceValue(pasteChoiceTitle(p.File)); got != p.File {
			t.Errorf("%s: the round trip gave %q", p.File, got)
		}
	}
	for file, title := range map[string]string{"code.exe": "Visual Studio Code", "hx.exe": "hx", "HX.EXE": "HX", "hx": "hx"} {
		if got := programTitle(file); got != title {
			t.Errorf("programTitle(%q) = %q, want %q", file, got, title)
		}
	}
}

func TestPasteHint(t *testing.T) {
	cases := []struct {
		thisShot, settingOn bool
		destination, want   string
	}{
		{true, true, "Windows Terminal", "Pastes into Windows Terminal after the drag · 1 switches it off"},
		{true, false, "", "No app to paste into · 1 switches pasting off"},
		{false, true, "", "Not pasting this time · 1 switches it back on"},
		{false, false, "", ""},
	}
	for _, c := range cases {
		if got := pasteHint(c.thisShot, c.settingOn, c.destination, "1"); got != c.want {
			t.Errorf("pasteHint(%v, %v, %q) = %q, want %q", c.thisShot, c.settingOn, c.destination, got, c.want)
		}
	}
	for in, want := range map[string]string{"ctrl+shift+1": "1", "ctrl+alt+s": "S", "win+shift+f9": "F9"} {
		if got := hotkeyKey(in); got != want {
			t.Errorf("hotkeyKey(%q) = %q, want %q", in, got, want)
		}
	}
}
