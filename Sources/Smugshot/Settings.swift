import AppKit
import Carbon.HIToolbox

/// What the user can change: where smugshots go, how long they live, the text in front of the path, the
/// shortcut, and what is gathered besides the pictures.
/// Kept in UserDefaults (com.pjalle.smugshot), so `defaults write` works as well as the Settings window.
enum Settings {
    static let defaultFolder = FileManager.default.homeDirectoryForCurrentUser.appendingPathComponent(".smugshots", isDirectory: true)
    static let defaultPrefix = "smugshot: "
    static let defaultRetention = Retention.oneDay
    static let defaultShortcut = Shortcut(keyCode: UInt32(kVK_ANSI_1), modifiers: UInt32(cmdKey | shiftKey))

    /// Posted on the main thread after any setting changes.
    static let changed = Notification.Name("com.pjalle.smugshot.settingsChanged")

    private static var defaults: UserDefaults { .standard }

    /// The folder smugshots are written to. `defaults write com.pjalle.smugshot folder -string ~/somewhere`.
    static var folder: URL {
        get {
            if let path = defaults.string(forKey: "folder"), !path.isEmpty {
                return URL(fileURLWithPath: (path as NSString).expandingTildeInPath, isDirectory: true)
            }
            return defaultFolder
        }
        set { store(newValue.standardizedFileURL.path == defaultFolder.standardizedFileURL.path ? nil : newValue.path, for: "folder") }
    }

    static var usesDefaultFolder: Bool { folder.standardizedFileURL.path == defaultFolder.standardizedFileURL.path }

    /// The text put in front of the path on the clipboard, spaces included. Empty is allowed.
    /// `defaults write com.pjalle.smugshot prefix -string "look: "`.
    static var prefix: String {
        get { defaults.object(forKey: "prefix") as? String ?? defaultPrefix }
        set { store(newValue == defaultPrefix ? nil : newValue, for: "prefix") }
    }

    /// How long a smugshot stays on disk. `defaults write com.pjalle.smugshot keepFor -string 5m`.
    static var retention: Retention {
        get { Retention(rawValue: defaults.string(forKey: "keepFor") ?? "") ?? defaultRetention }
        set { store(newValue == defaultRetention ? nil : newValue.rawValue, for: "keepFor") }
    }

    /// The shortcut that starts a smugshot. `defaults write com.pjalle.smugshot hotkeyKeyCode -int 18` and
    /// `hotkeyModifiers -int 768` (256 Command, 512 Shift, 2048 Option, 4096 Control, added together).
    static var shortcut: Shortcut {
        get {
            guard defaults.object(forKey: "hotkeyKeyCode") != nil else { return defaultShortcut }
            return Shortcut(keyCode: UInt32(defaults.integer(forKey: "hotkeyKeyCode")),
                            modifiers: UInt32(defaults.integer(forKey: "hotkeyModifiers")))
        }
        set {
            if newValue == defaultShortcut {
                defaults.removeObject(forKey: "hotkeyKeyCode")
                store(nil, for: "hotkeyModifiers")
            } else {
                defaults.set(Int(newValue.keyCode), forKey: "hotkeyKeyCode")
                store(Int(newValue.modifiers), for: "hotkeyModifiers")
            }
        }
    }

    // What is gathered besides the two pictures, the app and the window. All on by default.
    // `defaults write com.pjalle.smugshot readText -bool false`, and the same for nameControls and webElement.

    /// "What was there": the control under the drag, from the accessibility layer.
    static var nameControls: Bool {
        get { flag("nameControls") }
        set { store(newValue ? nil : false, for: "nameControls") }
    }

    /// "Text read from the close-up": text recognised in the picture.
    static var readText: Bool {
        get { flag("readText") }
        set { store(newValue ? nil : false, for: "readText") }
    }

    /// "Web element": the page element's HTML, asked of the browser.
    static var webElement: Bool {
        get { flag("webElement") }
        set { store(newValue ? nil : false, for: "webElement") }
    }

    /// Quiet: no sound and no icon flash after a smugshot. For calls and screen shares.
    /// `defaults write com.pjalle.smugshot quiet -bool true`.
    static var quiet: Bool {
        get { defaults.bool(forKey: "quiet") }
        set { store(newValue ? true : nil, for: "quiet") }
    }

    /// The sound after a smugshot. `defaults write com.pjalle.smugshot sound -string pop`.
    static var sound: Sound {
        get { Sound(rawValue: defaults.string(forKey: "sound") ?? "") ?? defaultSound }
        set { store(newValue == defaultSound ? nil : newValue.rawValue, for: "sound") }
    }
    static let defaultSound = Sound.screenCapture

    /// What lands on the clipboard for a given shot.md path.
    static func clipboardText(for path: String) -> String { prefix + path }

    private static func flag(_ key: String) -> Bool { defaults.object(forKey: key) == nil ? true : defaults.bool(forKey: key) }

    private static func store(_ value: Any?, for key: String) {
        if let value { defaults.set(value, forKey: key) } else { defaults.removeObject(forKey: key) }
        NotificationCenter.default.post(name: changed, object: nil)
    }
}

/// How long a smugshot stays on disk. The raw value is what is stored and what `defaults write` takes.
/// The sounds to choose from. All of them ship with macOS, so nothing is bundled.
enum Sound: String, CaseIterable {
    case screenCapture = "screen-capture", shutter, frog, pop, sent, tink

    var title: String {
        switch self {
        case .frog: return "Frog"
        case .pop: return "Pop"
        case .sent: return "Sent"
        case .screenCapture: return "Screen Capture"
        case .shutter: return "Shutter"
        case .tink: return "Tink"
        }
    }

    private var path: String {
        let alerts = "/System/Library/Sounds/"
        let system = "/System/Library/Components/CoreAudio.component/Contents/SharedSupport/SystemSounds/system/"
        switch self {
        case .frog: return alerts + "Frog.aiff"
        case .pop: return alerts + "Pop.aiff"
        case .sent: return system + "acknowledgment_sent.caf"
        case .screenCapture: return system + "Screen Capture.aif"
        case .shutter: return system + "Shutter.aif"
        case .tink: return alerts + "Tink.aiff"
        }
    }

    /// Falls back to Tink by name if a macOS update moves the file.
    func play() {
        let sound = NSSound(contentsOfFile: path, byReference: true) ?? NSSound(named: "Tink")
        sound?.play()
    }
}

enum Retention: String, CaseIterable {
    case oneMinute = "1m"
    case twoMinutes = "2m"
    case fiveMinutes = "5m"
    case tenMinutes = "10m"
    case thirtyMinutes = "30m"
    case oneHour = "1h"
    case oneDay = "1d"
    case oneWeek = "1w"
    case forever = "forever"

    var title: String {
        switch self {
        case .oneMinute: return "1 minute"
        case .twoMinutes: return "2 minutes"
        case .fiveMinutes: return "5 minutes"
        case .tenMinutes: return "10 minutes"
        case .thirtyMinutes: return "30 minutes"
        case .oneHour: return "1 hour"
        case .oneDay: return "1 day"
        case .oneWeek: return "1 week"
        case .forever: return "Forever"
        }
    }

    /// Seconds a smugshot lives before the sweep removes it, or nil for forever.
    var lifetime: TimeInterval? {
        switch self {
        case .oneMinute: return 60
        case .twoMinutes: return 2 * 60
        case .fiveMinutes: return 5 * 60
        case .tenMinutes: return 10 * 60
        case .thirtyMinutes: return 30 * 60
        case .oneHour: return 60 * 60
        case .oneDay: return 24 * 60 * 60
        case .oneWeek: return 7 * 24 * 60 * 60
        case .forever: return nil
        }
    }
}

/// A key plus modifiers, in the Carbon hotkey service's terms (the same numbers `defaults write` takes).
struct Shortcut: Equatable {
    let keyCode: UInt32
    let modifiers: UInt32

    /// The shortcut a key press describes, or nil if it is not fit to be one: it needs Command, Control or
    /// Option, so a plain letter or Shift+letter can never be taken from every app.
    init?(event: NSEvent) {
        let flags = event.modifierFlags.intersection(.deviceIndependentFlagsMask)
        var modifiers: UInt32 = 0
        if flags.contains(.command) { modifiers |= UInt32(cmdKey) }
        if flags.contains(.shift) { modifiers |= UInt32(shiftKey) }
        if flags.contains(.option) { modifiers |= UInt32(optionKey) }
        if flags.contains(.control) { modifiers |= UInt32(controlKey) }
        guard modifiers & UInt32(cmdKey | optionKey | controlKey) != 0 else { return nil }
        self.init(keyCode: UInt32(event.keyCode), modifiers: modifiers)
    }

    init(keyCode: UInt32, modifiers: UInt32) {
        self.keyCode = keyCode
        self.modifiers = modifiers
    }

    /// How the Mac writes it in menus: ⌃⌥⇧⌘1.
    var label: String {
        var text = ""
        if modifiers & UInt32(controlKey) != 0 { text += "⌃" }
        if modifiers & UInt32(optionKey) != 0 { text += "⌥" }
        if modifiers & UInt32(shiftKey) != 0 { text += "⇧" }
        if modifiers & UInt32(cmdKey) != 0 { text += "⌘" }
        return text + KeyName.of(keyCode)
    }
}

/// The name of a key for showing a shortcut, from the current keyboard layout.
enum KeyName {
    private static let special: [Int: String] = [
        kVK_Return: "↩", kVK_Tab: "⇥", kVK_Space: "Space", kVK_Delete: "⌫", kVK_Escape: "⎋", kVK_ForwardDelete: "⌦",
        kVK_LeftArrow: "←", kVK_RightArrow: "→", kVK_UpArrow: "↑", kVK_DownArrow: "↓",
        kVK_Home: "↖", kVK_End: "↘", kVK_PageUp: "⇞", kVK_PageDown: "⇟",
        kVK_F1: "F1", kVK_F2: "F2", kVK_F3: "F3", kVK_F4: "F4", kVK_F5: "F5", kVK_F6: "F6", kVK_F7: "F7", kVK_F8: "F8",
        kVK_F9: "F9", kVK_F10: "F10", kVK_F11: "F11", kVK_F12: "F12", kVK_F13: "F13", kVK_F14: "F14", kVK_F15: "F15",
        kVK_F16: "F16", kVK_F17: "F17", kVK_F18: "F18", kVK_F19: "F19", kVK_F20: "F20",
    ]

    static func of(_ keyCode: UInt32) -> String {
        if let name = special[Int(keyCode)] { return name }
        guard let source = TISCopyCurrentKeyboardLayoutInputSource()?.takeRetainedValue(),
              let layoutData = TISGetInputSourceProperty(source, kTISPropertyUnicodeKeyLayoutData) else { return "key \(keyCode)" }
        let data = unsafeBitCast(layoutData, to: CFData.self) as Data
        var deadKeys: UInt32 = 0
        var length = 0
        var chars = [UniChar](repeating: 0, count: 4)
        let status = data.withUnsafeBytes { bytes -> OSStatus in
            let layout = bytes.baseAddress!.assumingMemoryBound(to: UCKeyboardLayout.self)
            return UCKeyTranslate(layout, UInt16(keyCode), UInt16(kUCKeyActionDisplay), 0, UInt32(LMGetKbdType()),
                                  UInt32(kUCKeyTranslateNoDeadKeysMask), &deadKeys, chars.count, &length, &chars)
        }
        guard status == noErr, length > 0 else { return "key \(keyCode)" }
        return String(utf16CodeUnits: chars, count: length).uppercased()
    }
}
