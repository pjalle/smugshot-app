import AppKit
import Carbon.HIToolbox
import ServiceManagement

final class AppDelegate: NSObject, NSApplicationDelegate, NSMenuDelegate {
    private var statusItem: NSStatusItem!
    private let capturer = Capturer()
    private let overlay = Overlay()
    private var hotKey: HotKey?
    private var markupHotKey: HotKey?
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

    /// The mark-up in flight, or nil. Only one at a time, like the gesture.
    private struct Markup {
        let at: Date
        var folder: URL?          // made with the first spot, so a mark-up stopped before any spot leaves nothing behind
        var spots: [Spot] = []
        var reading: [Task<Void, Never>] = [] // the slow reads, one per spot, waited for at the end
    }
    private var markup: Markup?
    private let note = NoteWindow()

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
        note.onNext = { [weak self] in self?.saveNote(); self?.nextSpot() }
        note.onDone = { [weak self] in self?.endMarkup() }
        note.onDrop = { [weak self] in self?.dropSpot() }
    }

    /// Takes the shortcuts from Settings (Command + Shift + 1 for a smugshot, Command + Shift + 2 for a mark-up)
    /// system-wide. Called again whenever they change.
    private func registerHotKey() {
        let shortcut = Settings.shortcut
        if hotKey == nil || hotKey?.keyCode != shortcut.keyCode || hotKey?.modifiers != shortcut.modifiers {
            hotKey?.unregister()
            hotKey = HotKey(keyCode: shortcut.keyCode, modifiers: shortcut.modifiers, onDown: { [weak self] in self?.hotKeyPressed() })
            settingsWindow.shortcutTaken = hotKey == nil
            if hotKey == nil { NSLog("Smugshot: could not register the hotkey \(shortcut.label) (already taken?)") }
        }
        let markupShortcut = Settings.markupShortcut
        if markupHotKey == nil || markupHotKey?.keyCode != markupShortcut.keyCode || markupHotKey?.modifiers != markupShortcut.modifiers {
            markupHotKey?.unregister()
            markupHotKey = HotKey(keyCode: markupShortcut.keyCode, modifiers: markupShortcut.modifiers, onDown: { [weak self] in self?.markupKeyPressed() })
            settingsWindow.markupTaken = markupHotKey == nil
            if markupHotKey == nil { NSLog("Smugshot: could not register the mark-up hotkey \(markupShortcut.label) (already taken?)") }
        }
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
    /// Esc, a right-click, a plain click or pressing the hotkey again cancels. During a mark-up, either
    /// shortcut ends it, keeping the spots so far.
    private func hotKeyPressed() {
        if markup != nil { endMarkup() } else if active { finish(nil, nil) } else { begin() }
    }

    private func markupKeyPressed() {
        if markup != nil { endMarkup() } else if active { finish(nil, nil) } else { startMarkup() }
    }

    private func begin() {
        let screens = NSScreen.screens
        guard !active, !screens.isEmpty else { return }
        active = true
        startedAt = Date()
        // A mark-up's later spots come back from its own note box, so the app it came from, and whether to
        // paste, are only taken on the first.
        if markup == nil || markup?.spots.isEmpty == true {
            let front = NSWorkspace.shared.frontmostApplication
            cameFrom = front?.processIdentifier == ProcessInfo.processInfo.processIdentifier ? nil : front
            pasteThisShot = Settings.pasteInto != .off
        }

        // Every screen is pictured now, as the user saw it when they decided to point.
        for screen in screens {
            captureTasks[ObjectIdentifier(screen)] = Task { [capturer] in try await capturer.capture(screen: screen) }
        }
        // The drag layer waits for the pictures (well under a second) and then shows them, so the screen freezes:
        // a hover state or a tooltip stays put while dragging. Showing the layer first would also take the
        // mouse away from the app underneath before the picture is taken.
        let tasks = captureTasks
        let started = startedAt
        let markingUp = markup != nil
        Task { @MainActor in
            var frozen: [ObjectIdentifier: CGImage] = [:]
            for (id, task) in tasks { frozen[id] = try? await task.value }
            guard self.active, self.startedAt == started else { return }
            // While marking up, a click without a drag is nothing: drag again.
            self.overlay.show(on: screens, frozen: frozen, clickCancels: !markingUp) { [weak self] screen, rect in self?.finish(screen, rect) }
            self.overlay.showHint(self.hint())
        }
        escapeKey = HotKey(keyCode: UInt32(kVK_Escape), modifiers: 0, onDown: { [weak self] in self?.finish(nil, nil) })
        pasteKey = HotKey(keyCode: Settings.shortcut.keyCode, modifiers: 0, onDown: { [weak self] in self?.togglePaste() })
    }

    /// The shortcut's key, pressed on its own while the screen is frozen.
    private func togglePaste() {
        guard active else { return }
        pasteThisShot.toggle()
        overlay.showHint(hint())
    }

    /// Where this smugshot would be pasted, or nil when there is nowhere.
    private func pasteDestination() -> Paster.Destination? {
        switch Settings.pasteInto {
        case .off, .previousApp: return cameFrom.map { .app($0) }
        case .app(let bundleID): return .bundle(bundleID)
        }
    }

    /// The line at the top of the frozen screen. Nothing when pasting is off and was not touched, except
    /// while marking up, which always says which spot is next.
    private func hint() -> String? {
        let paste = pasteHint()
        guard let markup else { return paste }
        return ShotFile.markupHint(next: markup.spots.count + 1, paste: paste)
    }

    private func pasteHint() -> String? {
        let key = KeyName.of(Settings.shortcut.keyCode)
        if pasteThisShot {
            guard let destination = pasteDestination() else { return "No app to paste into · \(key) switches pasting off" }
            return "Pastes into \(destination.name) after the drag · \(key) switches it off"
        }
        return Settings.pasteInto == .off ? nil : "Not pasting this time · \(key) switches it back on"
    }

    /// Takes the drag layer down, and the keys that go with it. The picture of `screen` is handed back; the
    /// rest are cancelled.
    private func takeDown(keeping screen: NSScreen?) -> Task<CGImage, Error>? {
        overlay.hide()
        escapeKey?.unregister()
        escapeKey = nil
        pasteKey?.unregister()
        pasteKey = nil
        let tasks = captureTasks
        captureTasks = [:]
        let keep = screen.map { ObjectIdentifier($0) }
        for (id, task) in tasks where id != keep { task.cancel() }
        return keep.flatMap { tasks[$0] }
    }

    private func finish(_ screen: NSScreen?, _ rect: CGRect?) {
        guard active else { return }
        if markup != nil {
            if let screen, let rect { finishSpot(screen, rect) } else { endMarkup() }
            return
        }
        let destination = pasteThisShot ? pasteDestination() : nil
        let captureTask = takeDown(keeping: screen)
        guard let rect, let screen, let captureTask else {
            active = false
            return
        }

        let date = startedAt
        Task { @MainActor in
            defer { self.active = false }
            do {
                let image = try await captureTask.value
                let path = try await Self.save(image: image, rect: rect, screen: screen, date: date)
                await self.deliver(path: path, destination: destination)
            } catch {
                self.failed(error)
            }
            Store.sweep()
            await self.capturer.refresh()
        }
    }

    /// Hands a finished shot.md over: the path on the clipboard, pasted if asked, then the sound and the flash.
    private func deliver(path: String, destination: Paster.Destination?) async {
        copyToClipboard(URL(fileURLWithPath: path))
        if !Settings.quiet {
            Settings.sound.play()
            flash("checkmark.circle.fill")
        }
        if let destination, await !Paster.paste(into: destination) {
            // The path is on the clipboard all the same; the user pastes it by hand.
            if !Settings.quiet { flash("exclamationmark.triangle.fill") }
        }
    }

    private func failed(_ error: Error) {
        NSLog("Smugshot: \(error)")
        if !Settings.quiet {
            NSSound(named: "Basso")?.play()
            flash("exclamationmark.triangle.fill")
        }
    }

    // MARK: Mark-up

    private func startMarkup() {
        guard !active else { return }
        markup = Markup(at: Date())
        begin()
        if !active { markup = nil } // no screen to picture
    }

    /// The end of one drag while marking up: save the two pictures, start the slow reads, and ask for the note.
    private func finishSpot(_ screen: NSScreen, _ rect: CGRect) {
        guard markup != nil, let captureTask = takeDown(keeping: screen) else { endMarkup(); return }
        active = false
        let at = markup?.at
        Task { @MainActor in
            let image: CGImage
            do { image = try await captureTask.value } catch { self.failed(error); self.nextSpot(); return }
            // Not if the mark-up ended while the picture was on its way.
            guard var m = self.markup, m.at == at else { return }
            do {
                let folder = try m.folder ?? Store.newFolder(at: m.at)
                m.folder = folder
                let names = Spot.files(m.spots.count + 1)
                let capture = try Self.saveCapture(image: image, rect: rect, screen: screen, folder: folder, cropName: names.crop, fullName: names.full)
                let spot = Spot(crop: names.crop, full: names.full, region: capture.region, fullSize: capture.fullSize, target: capture.target)
                m.spots.append(spot)
                m.reading.append(Task { @MainActor in
                    let read = await Self.gather(capture)
                    spot.url = read.web?.url ?? read.controls?.url
                    spot.controls = read.controls
                    spot.web = read.web
                    spot.words = read.words
                })
                self.markup = m
                self.note.open(spot: m.spots.count, below: rect, on: screen)
            } catch {
                self.markup = m
                self.failed(error)
                self.nextSpot()
            }
        }
    }

    /// Freezes the screen again for another spot, once the note box is off it, so the box is never in the
    /// next picture.
    private func nextSpot() {
        note.hide()
        Task { @MainActor in
            try? await Task.sleep(for: .milliseconds(80))
            guard self.markup != nil, !self.active else { return }
            self.begin()
            if !self.active { self.endMarkup() }
        }
    }

    private func saveNote() {
        guard note.isOpen, let spot = markup?.spots.last else { return }
        spot.note = note.text
    }

    /// Esc in the note box: the last spot goes, files and all.
    private func dropSpot() {
        guard var m = markup, let spot = m.spots.popLast() else { return }
        if let folder = m.folder {
            try? FileManager.default.removeItem(at: folder.appendingPathComponent(spot.crop))
            try? FileManager.default.removeItem(at: folder.appendingPathComponent(spot.full))
        }
        markup = m
        nextSpot()
    }

    /// Writes shot.md for the spots so far and hands it over. With no spots there is no smugshot.
    private func endMarkup() {
        guard let m = markup else { return }
        saveNote()
        note.hide()
        markup = nil
        if active {
            _ = takeDown(keeping: nil)
            active = false
        }
        let destination = pasteThisShot ? pasteDestination() : nil
        Task { @MainActor in
            for task in m.reading { await task.value } // the slow reads take a second or so per spot
            guard let folder = m.folder, !m.spots.isEmpty else {
                if let folder = m.folder { try? FileManager.default.removeItem(at: folder) } // every spot was dropped
                return
            }
            let shot = folder.appendingPathComponent("shot.md")
            do {
                try ShotFile.markupText(folder: folder, date: m.at, spots: m.spots).write(to: shot, atomically: true, encoding: .utf8)
                await self.deliver(path: shot.path, destination: destination)
            } catch {
                self.failed(error)
            }
            Store.sweep()
            await self.capturer.refresh()
        }
    }

    // MARK: Saving

    /// Puts a shot.md path on the clipboard, with the prefix in front: a bare path starts with "/", which chat
    /// tools read as a command.
    private func copyToClipboard(_ shot: URL) {
        NSPasteboard.general.clearContents()
        NSPasteboard.general.setString(Settings.clipboardText(for: shot.path), forType: .string)
        lastShot = shot
    }

    /// One drag, pictured and written: what the slow reads need to know about it.
    struct Capture {
        let image: CGImage        // the raw screen picture
        let rawRegion: CGRect     // the drag in its pixels
        let region: CGRect        // the drag in full.png's pixels
        let fullSize: CGSize
        let global: CGRect        // the drag in the whole desktop's points, counted from the top-left of the main screen
        let scale: CGFloat
        let target: Target
    }

    /// Writes the close-up and the whole screen for one drag into `folder`, under the names given, and says
    /// which app and window were under it.
    @MainActor
    private static func saveCapture(image: CGImage, rect: CGRect, screen: NSScreen, folder: URL, cropName: String, fullName: String) throws -> Capture {
        // Each screen has its own scale, so it is read off the picture itself.
        let scale = CGFloat(image.width) / screen.frame.width
        let region = rect.applying(CGAffineTransform(scaleX: scale, y: scale)).integral

        guard let (full, fullRegion) = Renderer.full(image: image, region: region),
              let crop = Renderer.crop(image: image, region: region) else { throw CocoaError(.fileWriteUnknown) }
        guard Renderer.writePNG(full, to: folder.appendingPathComponent(fullName)),
              Renderer.writePNG(crop, to: folder.appendingPathComponent(cropName)) else { throw CocoaError(.fileWriteUnknown) }

        // The drag in the whole desktop's coordinates, counted from the top-left of the main screen,
        // which is how windows and the accessibility layer count.
        let mainHeight = NSScreen.screens.first?.frame.maxY ?? screen.frame.maxY
        let global = CGRect(x: screen.frame.minX + rect.minX, y: mainHeight - screen.frame.maxY + rect.minY,
                            width: rect.width, height: rect.height)
        return Capture(image: image, rawRegion: region, region: fullRegion, fullSize: CGSize(width: full.width, height: full.height),
                       global: global, scale: scale, target: Target.under(CGPoint(x: global.midX, y: global.midY)))
    }

    /// The slow reads for one drag. Each can be switched off in Settings. The controls and the text are read
    /// off the main thread; the browser question has to be asked on it.
    @MainActor
    private static func gather(_ capture: Capture) async -> (controls: Accessibility.Description?, web: Browser.Result?, words: [String]) {
        let (nameControls, readText, webElement) = (Settings.nameControls, Settings.readText, Settings.webElement)
        let (image, rawRegion, global, target) = (capture.image, capture.rawRegion, capture.global, capture.target)
        async let controlsRead = Task.detached {
            nameControls ? target.pid.map { Accessibility.describe(region: global, pid: $0) } : nil
        }.value
        async let wordsRead = Task.detached {
            readText ? Renderer.raw(image: image, region: rawRegion).map(TextReader.read) ?? [] : []
        }.value
        let controls = await controlsRead
        let web = webElement ? target.bundleID.flatMap {
            Browser.element(bundleID: $0, region: global, webArea: controls?.webAreaFrame, screenScale: capture.scale)
        } : nil
        let words = await wordsRead
        return (controls, web, words)
    }

    @MainActor
    private static func save(image: CGImage, rect: CGRect, screen: NSScreen, date: Date) async throws -> String {
        let folder = try Store.newFolder(at: date)
        let capture = try saveCapture(image: image, rect: rect, screen: screen, folder: folder, cropName: "crop.png", fullName: "full.png")
        let read = await gather(capture)
        let shot = folder.appendingPathComponent("shot.md")
        let text = ShotFile.text(folder: folder, date: date, target: capture.target, url: read.web?.url ?? read.controls?.url,
                                 region: capture.region, fullSize: capture.fullSize,
                                 controls: read.controls, words: read.words, web: read.web)
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
        menu.addItem(withTitle: "Press \(Settings.markupShortcut.label) to mark up several spots, a note on each", action: nil, keyEquivalent: "")
        if !CGPreflightScreenCaptureAccess() {
            menu.addItem(withTitle: "⚠️ Needs Screen Recording permission…", action: #selector(openScreenRecordingSettings), keyEquivalent: "")
        }
        if hotKey == nil {
            menu.addItem(withTitle: "⚠️ The shortcut is taken by another app. Pick another in Settings…", action: #selector(openSettings), keyEquivalent: "")
        }
        if markupHotKey == nil {
            menu.addItem(withTitle: "⚠️ The mark-up shortcut is taken by another app. Pick another in Settings…", action: #selector(openSettings), keyEquivalent: "")
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

    /// The close-up, 40 points high, for the Recent menu. For a marked-up smugshot, the first spot's.
    private func thumbnail(of folder: URL) -> NSImage? {
        let crop = ["crop.png", Spot.files(1).crop].map { folder.appendingPathComponent($0) }.first { FileManager.default.fileExists(atPath: $0.path) }
        guard let crop, let image = NSImage(contentsOf: crop), image.size.height > 0 else { return nil }
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
        NSWorkspace.shared.activateFileViewerSelecting([folder.appendingPathComponent("shot.md")])
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
