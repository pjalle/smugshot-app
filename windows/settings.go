package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Settings on Windows are a small JSON file, %APPDATA%\Smugshot\settings.json. "Settings…" in the tray
// menu opens it in Notepad. It is read again before every smugshot and every sweep, so no restart is needed.
//
// The same settings as on the Mac where they apply: where smugshots go, how long they are kept, the text
// in front of the path, and the hotkey. The hotkey is read once, at start.
type settings struct {
	Folder  string  `json:"folder"`
	KeepFor string  `json:"keepFor"`
	Prefix  *string `json:"prefix"` // a pointer, so an explicit "" means "nothing in front of the path"
	Hotkey  string  `json:"hotkey"`
	Sound   *bool   `json:"sound"`  // the tink after a smugshot; pointers so that a missing key means on
	Banner  *bool   `json:"banner"` // the small "Copied" banner after a smugshot
}

func (s settings) soundOn() bool  { return s.Sound == nil || *s.Sound }
func (s settings) bannerOn() bool { return s.Banner == nil || *s.Banner }

const (
	defaultKeepFor = "1d"
	defaultPrefix  = "smugshot: "
	defaultHotkey  = "ctrl+shift+1"
)

// The file written the first time Settings… is opened, so the user sees what can be set.
const settingsTemplate = `{
  "_help": "Smugshot settings. folder: where smugshots go (empty = ~\\.smugshots). keepFor: 1m, 2m, 5m, 10m, 30m, 1h, 1d, 1w or forever. prefix: the text in front of the path on the clipboard, spaces included. hotkey: modifiers plus one key, like ctrl+shift+1, ctrl+alt+s, win+shift+f9; needs a restart. sound and banner: the tink and the small Copied banner after a smugshot, true or false. Other changes apply to the next smugshot.",
  "folder": "",
  "keepFor": "1d",
  "prefix": "smugshot: ",
  "hotkey": "ctrl+shift+1",
  "sound": true,
  "banner": true
}
`

func defaultFolder() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".smugshots")
}

func settingsPath() string {
	dir, err := os.UserConfigDir() // %APPDATA%
	if err != nil {
		dir, _ = os.UserHomeDir()
	}
	return filepath.Join(dir, "Smugshot", "settings.json")
}

// loadSettings reads the settings file. A missing or broken file means the defaults.
func loadSettings() settings {
	return parseSettings(readFile(settingsPath()))
}

func readFile(path string) []byte {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	return data
}

func parseSettings(data []byte) settings {
	var s settings
	if len(data) > 0 {
		if err := json.Unmarshal(data, &s); err != nil {
			s = settings{}
		}
	}
	if s.Folder == "" {
		s.Folder = defaultFolder()
	} else if strings.HasPrefix(s.Folder, "~") {
		home, _ := os.UserHomeDir()
		s.Folder = filepath.Join(home, strings.TrimLeft(s.Folder[1:], `\/`))
	}
	if s.KeepFor == "" {
		s.KeepFor = defaultKeepFor
	}
	if _, _, err := parseKeepFor(s.KeepFor); err != nil {
		s.KeepFor = defaultKeepFor
	}
	if s.Prefix == nil {
		p := defaultPrefix
		s.Prefix = &p
	}
	if _, _, err := parseHotkey(s.Hotkey); err != nil {
		s.Hotkey = defaultHotkey
	}
	return s
}

// parseHotkey turns "ctrl+shift+1" into RegisterHotKey's modifier flags and virtual key code.
// Modifiers: ctrl, shift, alt, win; at least one of ctrl, alt or win. The key: a letter, a digit, or F1 to F24.
func parseHotkey(value string) (modifiers uint32, key uint32, err error) {
	parts := strings.Split(strings.ToLower(strings.ReplaceAll(value, " ", "")), "+")
	if len(parts) < 2 {
		return 0, 0, errors.New("a hotkey needs modifiers and a key: " + value)
	}
	flags := map[string]uint32{"alt": 0x1, "ctrl": 0x2, "control": 0x2, "shift": 0x4, "win": 0x8}
	for _, m := range parts[:len(parts)-1] {
		f, ok := flags[m]
		if !ok {
			return 0, 0, errors.New("unknown modifier: " + m)
		}
		modifiers |= f
	}
	if modifiers&(0x1|0x2|0x8) == 0 {
		return 0, 0, errors.New("needs ctrl, alt or win: " + value)
	}
	k := parts[len(parts)-1]
	switch {
	case len(k) == 1 && (k[0] >= 'a' && k[0] <= 'z' || k[0] >= '0' && k[0] <= '9'):
		key = uint32(strings.ToUpper(k)[0]) // virtual key codes for letters and digits are their ASCII
	case len(k) >= 2 && k[0] == 'f':
		n, err := strconv.Atoi(k[1:])
		if err != nil || n < 1 || n > 24 {
			return 0, 0, errors.New("unknown key: " + k)
		}
		key = 0x70 + uint32(n-1) // VK_F1
	default:
		return 0, 0, errors.New("unknown key: " + k)
	}
	return modifiers, key, nil
}

// ensureSettingsFile writes the template if there is no settings file yet, and returns the path.
func ensureSettingsFile() string {
	path := settingsPath()
	if _, err := os.Stat(path); err == nil {
		return path
	}
	os.MkdirAll(filepath.Dir(path), 0o700)
	os.WriteFile(path, []byte(settingsTemplate), 0o600)
	return path
}

// parseKeepFor turns a keepFor value into a lifetime. forever is true for "forever".
// Accepts the presets (1m … 1w) and plain numbers with a unit: 90s, 45m, 3h, 2d, 2w.
func parseKeepFor(value string) (lifetime time.Duration, forever bool, err error) {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "forever":
		return 0, true, nil
	case "":
		return 0, false, errors.New("empty")
	}
	units := map[byte]time.Duration{'s': time.Second, 'm': time.Minute, 'h': time.Hour, 'd': 24 * time.Hour, 'w': 7 * 24 * time.Hour}
	unit, ok := units[value[len(value)-1]]
	if !ok {
		return 0, false, errors.New("unknown unit in " + value)
	}
	n, err := strconv.Atoi(value[:len(value)-1])
	if err != nil || n <= 0 {
		return 0, false, errors.New("not a number: " + value)
	}
	return time.Duration(n) * unit, false, nil
}

// hotkeyLabel turns "ctrl+shift+1" into "Ctrl + Shift + 1", the way a person reads it.
func hotkeyLabel(value string) string {
	parts := strings.Split(strings.ToLower(strings.TrimSpace(value)), "+")
	for i, part := range parts {
		part = strings.TrimSpace(part)
		switch {
		case part == "":
		case len(part) <= 3 && part[0] == 'f' && part != "f": // f1..f24
			part = strings.ToUpper(part)
		default:
			part = strings.ToUpper(part[:1]) + part[1:]
		}
		parts[i] = part
	}
	return strings.Join(parts, " + ")
}
