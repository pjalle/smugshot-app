import AppKit
import Carbon.HIToolbox
import ServiceManagement

final class AppDelegate: NSObject, NSApplicationDelegate, NSMenuDelegate {
    private var statusItem: NSStatusItem!
    private let capturer = Capturer()
    private let overlay = Overlay()
    private var hotKey: HotKey?
    private var escapeKey: HotKey?
    /// While the screen is frozen, the shortcut's key on its own switches "Paste it for me" for this one smugshot.
    private var pasteKey: HotKey?
    private let settingsWindow = SettingsWindow()
    private let whatsNewWindow = WhatsNewWindow()
    private var sweepTimer: Timer?

    /// The shot.md most recently put on the clipboard, for "Copy last smugshot again".
    private var lastShot: URL?

    // One gesture in flight at a time.
    private var active = false
    private var captureTasks: [ObjectIdentifier: Task<CGImage, Error>] = [:]
    private var startedAt = Date()
    /// The app in front when the shortcut was pressed: where "the app I came from" pastes.
    private var cameFrom: NSRunningApplication?
    /// Whether this smugshot is pasted for the user: the setting, unless the key switched it for this one.
    private var pasteThisShot = false

    func applicationDidFinishLaunching(_ notification: Notification) {
        setUpStatusItem()
        Store.prepare()
        Store.sweep()
        // Short "Keep for" settings need the sweep to run on its own, not only after each shot.
        sweepTimer = Timer.scheduledTimer(withTimeInterval: 30, repeats: true) { _ in Store.sweep() }
        NotificationCenter.default.addObserver(forName: Settings.changed, object: nil, queue: .main) { [weak self] _ in
            Store.prepare()
            Store.sweep()
            self?.registerHotKey()
        }

        let allowed = CGPreflightScreenCaptureAccess()
        NSLog("Smugshot: screen recording allowed = \(allowed)")
        if !allowed { CGRequestScreenCaptureAccess() }
        if Settings.nameControls || Settings.pasteInto != .off, !Accessibility.isAllowed { Accessibility.askForPermission() }
        Accessibility.startWarming()
        installTestHook()
        Task { await capturer.refresh() }
        NotificationCenter.default.addObserver(forName: NSApplication.didChangeScreenParametersNotification, object: nil, queue: .main) { [weak self] _ in
            Task { await self?.capturer.refresh() }
        }

        registerHotKey()
    }

    /// Takes the shortcut from Settings (default Command + Shift + 1) system-wide. Called again whenever it changes.
    private func registerHotKey() {
        let shortcut = Settings.shortcut
        if let hotKey, hotKey.keyCode == shortcut.keyCode, hotKey.modifiers == shortcut.modifiers { return }
        hotKey?.unregister()
        hotKey = HotKey(keyCode: shortcut.keyCode, modifiers: shortcut.modifiers, onDown: { [weak self] in self?.hotKeyPressed() })
        settingsWindow.shortcutTaken = hotKey == nil
        if hotKey == nil { NSLog("Smugshot: could not register the hotkey \(shortcut.label) (already taken?)") }
    }

    // MARK: Test hook

    /// Off unless `defaults write com.pjalle.smugshot enableTestHook -bool true`. Lets a script take a smugshot of a
    /// given rectangle on the main screen ("x,y,w,h" in points from the top-left) without hands on the mouse:
    ///   swift -e 'import Foundation; DistributedNotificationCenter.default().postNotificationName(.init("com.pjalle.smugshot.test"), object: "100,100,400,300", userInfo: nil, deliverImmediately: true)'
    /// The clipboard is left alone; the path is written to ~/.smugshots/last-test.txt.
    /// With object "whats-new" it opens the What's new window instead, and "settings" the Settings window.
    private func installTestHook() {
        guard UserDefaults.standard.bool(forKey: "enableTestHook") else { return }
        DistributedNotificationCenter.default().addObserver(forName: .init("com.pjalle.smugshot.test"), object: nil, queue: .main) { [capturer, whatsNewWindow, settingsWindow] note in
            // object "whats-new" or "settings" opens that window instead, to look at it without the menu.
            if note.object as? String == "whats-new" { Task { @MainActor in whatsNewWindow.show() }; return }
            if note.object as? String == "settings" { Task { @MainActor in settingsWindow.show() }; return }
            let numbers = (note.object as? String ?? "").split(separator: ",").compactMap { Double($0) }
            guard numbers.count == 4, let screen = NSScreen.screens.first else { return }
            let rect = CGRect(x: numbers[0], y: numbers[1], width: numbers[2], height: numbers[3])
            Task { @MainActor in
                let out = Store.root.appendingPathComponent("last-test.txt")
                do {
                    let image = try await capturer.capture(screen: screen)
                    let path = try await Self.save(image: image, rect: rect, screen: screen, date: Date())
                    try? path.write(to: out, atomically: true, encoding: .utf8)
                } catch { try? "error: \(error)".write(to: out, atomically: true, encoding: .utf8) }
            }
        }
    }

    // MARK: Gesture

    /// Works like the Mac's own Command+Shift+4: press once, let go, then drag whenever ready.
    /// Esc, a right-click, a plain click or pressing the hotkey again cancels.
    private func hotKeyPressed() {
        if active { finish(nil, nil) } else { begin() }
    }

    private func begin() {
        let screens = NSScreen.screens
        guard !active, !screens.isEmpty else { return }
        active = true
        startedAt = Date()
        let front = NSWorkspace.shared.frontmostApplication
        cameFrom = front?.processIdentifier == ProcessInfo.processInfo.processIdentifier ? nil : front
        pasteThisShot = Settings.pasteInto != .off

        // Every screen is pictured now, as the user saw it when they decided to point.
        for screen in screens {
            captureTasks[ObjectIdentifier(screen)] = Task { [capturer] in try await capturer.capture(screen: screen) }
        }
        // The drag layer waits for the pictures (well under a second) and then shows them, so the screen freezes:
        // a hover state or a tooltip stays put while dragging. Showing the layer first would also take the
        // mouse away from the app underneath before the picture is taken.
        let tasks = captureTasks
        let started = startedAt
        Task { @MainActor in
            var frozen: [ObjectIdentifier: CGImage] = [:]
            for (id, task) in tasks { frozen[id] = try? await task.value }
            guard self.active, self.startedAt == started else { return }
            self.overlay.show(on: screens, frozen: frozen) { [weak self] screen, rect in self?.finish(screen, rect) }
            self.overlay.showHint(self.pasteHint())
        }
        escapeKey = HotKey(keyCode: UInt32(kVK_Escape), modifiers: 0, onDown: { [weak self] in self?.finish(nil, nil) })
        pasteKey = HotKey(keyCode: Settings.shortcut.keyCode, modifiers: 0, onDown: { [weak self] in self?.togglePaste() })
    }

    /// The shortcut's key, pressed on its own while the screen is frozen.
    private func togglePaste() {
        guard active else { return }
        pasteThisShot.toggle()
        overlay.showHint(pasteHint())
    }

    /// Where this smugshot would be pasted, or nil when there is nowhere.
    private func pasteDestination() -> Paster.Destination? {
        switch Settings.pasteInto {
        case .off, .previousApp: return cameFrom.map { .app($0) }
        case .app(let bundleID): return .bundle(bundleID)
        }
    }

    /// The line at the top of the frozen screen. Nothing when pasting is off and was not touched.
    private func pasteHint() -> String? {
        let key = KeyName.of(Settings.shortcut.keyCode)
        if pasteThisShot {
            guard let destination = pasteDestination() else { return "No app to paste into · \(key) switches pasting off" }
            return "Pastes into \(destination.name) after the drag · \(key) switches it off"
        }
        return Settings.pasteInto == .off ? nil : "Not pasting this time · \(key) switches it back on"
    }

    private func finish(_ screen: NSScreen?, _ rect: CGRect?) {
        guard active else { return }
        overlay.hide()
        escapeKey?.unregister()
        escapeKey = nil
        pasteKey?.unregister()
        pasteKey = nil
        let destination = pasteThisShot ? pasteDestination() : nil

        let tasks = captureTasks
        captureTasks = [:]
        guard let rect, let screen, let captureTask = tasks[ObjectIdentifier(screen)] else {
            tasks.values.forEach { $0.cancel() }
            active = false
            return
        }
        tasks.filter { $0.key != ObjectIdentifier(screen) }.values.forEach { $0.cancel() }

        let date = startedAt
        Task { @MainActor in
            defer { self.active = false }
            do {
                let image = try await captureTask.value
                let path = try await Self.save(image: image, rect: rect, screen: screen, date: date)
                self.copyToClipboard(URL(fileURLWithPath: path))
                if !Settings.quiet {
                    Settings.sound.play()
                    self.flash("checkmark.circle.fill")
                }
                if let destination, await !Paster.paste(into: destination) {
                    // The path is on the clipboard all the same; the user pastes it by hand.
                    if !Settings.quiet { self.flash("exclamationmark.triangle.fill") }
                }
            } catch {
                NSLog("Smugshot: \(error)")
                if !Settings.quiet {
                    NSSound(named: "Basso")?.play()
                    self.flash("exclamationmark.triangle.fill")
                }
            }
            Store.sweep()
            await self.capturer.refresh()
        }
    }

    /// Puts a shot.md path on the clipboard, with the prefix in front: a bare path starts with "/", which chat
    /// tools read as a command.
    private func copyToClipboard(_ shot: URL) {
        NSPasteboard.general.clearContents()
        NSPasteboard.general.setString(Settings.clipboardText(for: shot.path), forType: .string)
        lastShot = shot
    }

    @MainActor
    private static func save(image: CGImage, rect: CGRect, screen: NSScreen, date: Date) async throws -> String {
        // Each screen has its own scale, so it is read off the picture itself.
        let scale = CGFloat(image.width) / screen.frame.width
        let region = rect.applying(CGAffineTransform(scaleX: scale, y: scale)).integral

        guard let (full, fullRegion) = Renderer.full(image: image, region: region),
              let crop = Renderer.crop(image: image, region: region) else { throw CocoaError(.fileWriteUnknown) }

        let folder = try Store.newFolder(at: date)
        guard Renderer.writePNG(full, to: folder.appendingPathComponent("full.png")),
              Renderer.writePNG(crop, to: folder.appendingPathComponent("crop.png")) else { throw CocoaError(.fileWriteUnknown) }

        // The drag in the whole desktop's coordinates, counted from the top-left of the main screen,
        // which is how windows and the accessibility layer count.
        let mainHeight = NSScreen.screens.first?.frame.maxY ?? screen.frame.maxY
        let global = CGRect(x: screen.frame.minX + rect.minX, y: mainHeight - screen.frame.maxY + rect.minY,
                            width: rect.width, height: rect.height)
        let target = Target.under(CGPoint(x: global.midX, y: global.midY))

        // Each of these can be switched off in Settings. Slow reads happen off the main thread; the browser
        // question has to be asked on it.
        let (nameControls, readText, webElement) = (Settings.nameControls, Settings.readText, Settings.webElement)
        async let controlsRead = Task.detached {
            nameControls ? target.pid.map { Accessibility.describe(region: global, pid: $0) } : nil
        }.value
        async let wordsRead = Task.detached {
            readText ? Renderer.raw(image: image, region: region).map(TextReader.read) ?? [] : []
        }.value
        let controls = await controlsRead
        let web = webElement ? target.bundleID.flatMap {
            Browser.element(bundleID: $0, region: global, webArea: controls?.webAreaFrame, screenScale: scale)
        } : nil
        let words = await wordsRead

        let shot = folder.appendingPathComponent("shot.md")
        let text = ShotFile.text(folder: folder, date: date, target: target, url: web?.url ?? controls?.url,
                                 region: fullRegion, fullSize: CGSize(width: full.width, height: full.height),
                                 controls: controls, words: words, web: web)
        try text.write(to: shot, atomically: true, encoding: .utf8)
        return shot.path
    }

    // MARK: Menu bar

    private func setUpStatusItem() {
        statusItem = NSStatusBar.system.statusItem(withLength: NSStatusItem.squareLength)
        showIdleIcon()
        let menu = NSMenu()
        menu.delegate = self
        statusItem.menu = menu
    }

    private func showIdleIcon() {
        statusItem.button?.image = MenuBarIcon.image()
    }

    private func setIcon(_ symbol: String) {
        statusItem.button?.image = NSImage(systemSymbolName: symbol, accessibilityDescription: "Smugshot")
    }

    private func flash(_ symbol: String) {
        setIcon(symbol)
        DispatchQueue.main.asyncAfter(deadline: .now() + 1.2) { [weak self] in self?.showIdleIcon() }
    }

    func menuNeedsUpdate(_ menu: NSMenu) {
        menu.removeAllItems()
        menu.addItem(withTitle: "Press \(Settings.shortcut.label), drag, paste the path", action: nil, keyEquivalent: "")
        if !CGPreflightScreenCaptureAccess() {
            menu.addItem(withTitle: "⚠️ Needs Screen Recording permission…", action: #selector(openScreenRecordingSettings), keyEquivalent: "")
        }
        if hotKey == nil {
            menu.addItem(withTitle: "⚠️ The shortcut is taken by another app. Pick another in Settings…", action: #selector(openSettings), keyEquivalent: "")
        }
        if Settings.nameControls, !Accessibility.isAllowed {
            menu.addItem(withTitle: "⚠️ Needs Accessibility permission to name controls…", action: #selector(openAccessibilitySettings), keyEquivalent: "")
        }
        if Settings.pasteInto != .off, !Accessibility.isAllowed {
            menu.addItem(withTitle: "⚠️ Needs Accessibility permission to paste for you…", action: #selector(openAccessibilitySettings), keyEquivalent: "")
        }
        menu.addItem(.separator())
        let recent = Store.recent(limit: 5)
        let last = lastShot.flatMap { FileManager.default.fileExists(atPath: $0.path) ? $0 : nil }
            ?? recent.first?.appendingPathComponent("shot.md")
        let again = menu.addItem(withTitle: "Copy last smugshot again", action: last == nil ? nil : #selector(copyLastAgain), keyEquivalent: "")
        again.representedObject = last
        if !recent.isEmpty {
            let submenu = NSMenu()
            for folder in recent {
                // Choosing one copies its path; holding Option shows it in Finder instead.
                let item = submenu.addItem(withTitle: ShotFile.summary(folder: folder), action: #selector(copyRecent(_:)), keyEquivalent: "")
                item.representedObject = folder
                item.image = thumbnail(of: folder)
                let reveal = submenu.addItem(withTitle: "Show in Finder", action: #selector(revealRecent(_:)), keyEquivalent: "")
                reveal.representedObject = folder
                reveal.isAlternate = true
                reveal.keyEquivalentModifierMask = .option
            }
            menu.addItem(withTitle: "Recent smugshots", action: nil, keyEquivalent: "").submenu = submenu
        }
        let quiet = menu.addItem(withTitle: "Quiet (no sound, no flash)", action: #selector(toggleQuiet), keyEquivalent: "")
        quiet.state = Settings.quiet ? .on : .off
        menu.addItem(.separator())
        menu.addItem(withTitle: "Settings…", action: #selector(openSettings), keyEquivalent: ",")
        menu.addItem(withTitle: "Open smugshots folder", action: #selector(openFolder), keyEquivalent: "")
        let login = menu.addItem(withTitle: "Start at login", action: #selector(toggleLogin), keyEquivalent: "")
        login.state = SMAppService.mainApp.status == .enabled ? .on : .off
        menu.addItem(.separator())
        menu.addItem(withTitle: "What's new", action: #selector(openWhatsNew), keyEquivalent: "")
        menu.addItem(withTitle: "Check for updates… (you have \(WhatsNewWindow.version))", action: #selector(checkForUpdates), keyEquivalent: "")
        menu.addItem(.separator())
        menu.addItem(withTitle: "Quit Smugshot", action: #selector(NSApplication.terminate(_:)), keyEquivalent: "q")
    }

    /// The close-up, 40 points high, for the Recent menu.
    private func thumbnail(of folder: URL) -> NSImage? {
        guard let image = NSImage(contentsOf: folder.appendingPathComponent("crop.png")), image.size.height > 0 else { return nil }
        let height: CGFloat = 40
        let width = min(96, image.size.width * height / image.size.height)
        let thumb = NSImage(size: NSSize(width: width, height: height), flipped: false) { rect in
            image.draw(in: rect, from: .zero, operation: .sourceOver, fraction: 1)
            return true
        }
        return thumb
    }

    @objc private func copyLastAgain(_ sender: NSMenuItem) {
        guard let shot = sender.representedObject as? URL else { return }
        copyToClipboard(shot)
        if !Settings.quiet { flash("checkmark.circle.fill") }
    }

    @objc private func copyRecent(_ sender: NSMenuItem) {
        guard let folder = sender.representedObject as? URL else { return }
        copyToClipboard(folder.appendingPathComponent("shot.md"))
        if !Settings.quiet { flash("checkmark.circle.fill") }
    }

    @objc private func revealRecent(_ sender: NSMenuItem) {
        guard let folder = sender.representedObject as? URL else { return }
        NSWorkspace.shared.activateFileViewerSelecting([folder.appendingPathComponent("crop.png")])
    }

    @objc private func toggleQuiet() { Settings.quiet.toggle() }

    @objc private func openSettings() { settingsWindow.show() }

    @objc private func openWhatsNew() { whatsNewWindow.show() }

    /// Smugshot never goes online itself. This hands the address to the browser; the page names the newest version.
    @objc private func checkForUpdates() { NSWorkspace.shared.open(URL(string: "https://smugshot.io")!) }

    @objc private func openFolder() {
        Store.prepare()
        NSWorkspace.shared.open(Store.root)
    }

    @objc private func openScreenRecordingSettings() {
        NSWorkspace.shared.open(URL(string: "x-apple.systempreferences:com.apple.preference.security?Privacy_ScreenCapture")!)
    }

    @objc private func openAccessibilitySettings() {
        NSWorkspace.shared.open(URL(string: "x-apple.systempreferences:com.apple.preference.security?Privacy_Accessibility")!)
    }

    @objc private func toggleLogin() {
        do {
            if SMAppService.mainApp.status == .enabled { try SMAppService.mainApp.unregister() } else { try SMAppService.mainApp.register() }
        } catch { NSLog("Smugshot: start at login failed: \(error)") }
    }
}
