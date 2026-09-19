package main

import (
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
