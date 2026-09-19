import AppKit
import Carbon.HIToolbox

/// "Paste it for me": after a smugshot, bring an app to the front and press Command+V there, so the path lands
/// in whatever has the cursor. It never presses Enter. The made-up key press needs the Accessibility permission.
enum Paster {
    /// Where the path goes, from Settings or the per-shot key.
    enum Destination {
        /// The app that was in front when the shortcut was pressed.
        case app(NSRunningApplication)
        /// An app picked in Settings, by bundle identifier. Started if it is not running.
        case bundle(String)

        var name: String {
            switch self {
            case .app(let app): return app.localizedName ?? "the app you came from"
            case .bundle(let id): return KnownApp.name(bundleID: id)
            }
        }
    }

    /// Brings the app forward, waits until it is in front, and pastes. Returns false when it could not.
    @MainActor
    static func paste(into destination: Destination) async -> Bool {
        guard Accessibility.isAllowed else {
            NSLog("Smugshot: cannot paste without the Accessibility permission")
            Accessibility.askForPermission()
            return false
        }
        guard let app = await bringForward(destination) else {
            NSLog("Smugshot: could not bring \(destination.name) to the front")
            return false
        }
        guard await waitForWindow(of: app) else {
            NSLog("Smugshot: \(destination.name) came to the front but no window took the keyboard")
            return false
        }
        guard NSWorkspace.shared.frontmostApplication?.processIdentifier == app.processIdentifier else { return false }
        return pressCommandV()
    }

    /// Being in front is not enough: the key press is lost if it lands while the Mac is still switching to
    /// another Space, or before an app that has only just started has a window. So this waits until the app
    /// says one of its windows has the keyboard (up to three seconds), then a little longer for the switch
    /// to finish.
    @MainActor
    private static func waitForWindow(of app: NSRunningApplication) async -> Bool {
        let element = AXUIElementCreateApplication(app.processIdentifier)
        AXUIElementSetMessagingTimeout(element, 0.3)
        for _ in 0..<60 {
            var focused: AnyObject?
            if AXUIElementCopyAttributeValue(element, kAXFocusedWindowAttribute as CFString, &focused) == .success, focused != nil {
                try? await Task.sleep(for: .milliseconds(400))
                return true
            }
            try? await Task.sleep(for: .milliseconds(50))
        }
        return false
    }

    @MainActor
    private static func bringForward(_ destination: Destination) async -> NSRunningApplication? {
        // Asking the workspace to open the app brings it forward whether it runs or not, and works from a
        // background app on macOS 14 and later, where activate() on its own does not.
        let url: URL?
        switch destination {
        case .app(let running): url = running.isTerminated ? nil : running.bundleURL
        case .bundle(let id): url = NSWorkspace.shared.urlForApplication(withBundleIdentifier: id)
        }
        guard let url else { return nil }
        let configuration = NSWorkspace.OpenConfiguration()
        configuration.activates = true
        guard let app = try? await NSWorkspace.shared.openApplication(at: url, configuration: configuration) else { return nil }
        // Up to three seconds, for an app that is only just starting.
        for _ in 0..<60 {
            if NSWorkspace.shared.frontmostApplication?.processIdentifier == app.processIdentifier { return app }
            try? await Task.sleep(for: .milliseconds(50))
        }
        return nil
    }

    /// A made-up Command+V, key down and key up, as if from the keyboard.
    private static func pressCommandV() -> Bool {
        guard let source = CGEventSource(stateID: .combinedSessionState),
              let down = CGEvent(keyboardEventSource: source, virtualKey: CGKeyCode(kVK_ANSI_V), keyDown: true),
              let up = CGEvent(keyboardEventSource: source, virtualKey: CGKeyCode(kVK_ANSI_V), keyDown: false) else { return false }
        down.flags = .maskCommand
        up.flags = .maskCommand
        down.post(tap: .cghidEventTap)
        up.post(tap: .cghidEventTap)
        return true
    }
}

/// The apps people talk to agents in, for the Settings list. Only the installed ones are shown.
enum KnownApp {
    static let all: [(name: String, bundleID: String)] = [
        ("Terminal", "com.apple.Terminal"),
        ("Ghostty", "com.mitchellh.ghostty"),
        ("iTerm", "com.googlecode.iterm2"),
        ("Warp", "dev.warp.Warp-Stable"),
        ("kitty", "net.kovidgoyal.kitty"),
        ("WezTerm", "com.github.wez.wezterm"),
        ("Alacritty", "org.alacritty"),
        ("Visual Studio Code", "com.microsoft.VSCode"),
        ("Cursor", "com.todesktop.230313mzl4w4u92"),
        ("Windsurf", "com.exafunction.windsurf"),
        ("Zed", "dev.zed.Zed"),
        ("Claude", "com.anthropic.claudefordesktop"),
        ("ChatGPT", "com.openai.chat"),
    ]

    static var installed: [(name: String, bundleID: String)] {
        all.filter { NSWorkspace.shared.urlForApplication(withBundleIdentifier: $0.bundleID) != nil }
    }

    /// The app's name as Finder shows it, or the known name, or the identifier itself.
    static func name(bundleID: String) -> String {
        if let url = NSWorkspace.shared.urlForApplication(withBundleIdentifier: bundleID) {
            let shown = FileManager.default.displayName(atPath: url.path)
            return shown.hasSuffix(".app") ? String(shown.dropLast(4)) : shown
        }
        return all.first { $0.bundleID == bundleID }?.name ?? bundleID
    }
}
