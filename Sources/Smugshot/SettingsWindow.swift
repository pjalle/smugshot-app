import AppKit

/// The Settings window: the shortcut, where smugshots go, how long they are kept, the clipboard text, and
/// what is gathered. Every change takes effect as soon as it is made; there is no Save button.
final class SettingsWindow: NSObject, NSTextFieldDelegate {
    private var window: NSWindow?

    private let shortcutButton = NSButton(title: "", target: nil, action: nil)
    private let shortcutNote = NSTextField(wrappingLabelWithString: "")
    private let folderField = NSTextField(labelWithString: "")
    private let folderNote = NSTextField(wrappingLabelWithString: "")
    private let keepPopUp = NSPopUpButton(frame: .zero, pullsDown: false)
    private let prefixField = NSTextField(string: "")
    private let preview = NSTextField(labelWithString: "")
    private let nameControlsBox = NSButton(checkboxWithTitle: "What was there: the control under the drag and its parents", target: nil, action: nil)
    private let readTextBox = NSButton(checkboxWithTitle: "Text read from the close-up", target: nil, action: nil)
    private let webElementBox = NSButton(checkboxWithTitle: "Web element: the page element's HTML, from the browser", target: nil, action: nil)
    private let soundPopUp = NSPopUpButton(frame: .zero, pullsDown: false)
    private let quietBox = NSButton(checkboxWithTitle: "No sound and no icon flash, for calls and screen shares", target: nil, action: nil)

    /// While recording a shortcut, the next key press is taken and the local monitor is this.
    private var recording: Any?

    /// Set by AppDelegate: whether the current shortcut could be registered. Shown under the button.
    var shortcutTaken = false { didSet { if window != nil { load() } } }

    func show() {
        if window == nil { build() }
        load()
        NSApp.activate(ignoringOtherApps: true)
        window?.center()
        window?.makeKeyAndOrderFront(nil)
    }

    // MARK: Building

    private func build() {
        let window = NSWindow(contentRect: NSRect(x: 0, y: 0, width: 600, height: 400),
                              styleMask: [.titled, .closable], backing: .buffered, defer: false)
        window.title = "Smugshot Settings"
        window.isReleasedWhenClosed = false
        window.delegate = self

        // Shortcut
        shortcutButton.target = self
        shortcutButton.action = #selector(recordShortcut)
        shortcutButton.setContentHuggingPriority(.required, for: .horizontal)
        let shortcutDefault = NSButton(title: "Use Default", target: self, action: #selector(resetShortcut))
        let shortcutRow = NSStackView(views: [shortcutButton, shortcutDefault])
        shortcutRow.orientation = .horizontal
        shortcutRow.spacing = 6
        small(shortcutNote)

        // Save to
        folderField.lineBreakMode = .byTruncatingMiddle
        folderField.setContentHuggingPriority(.defaultLow, for: .horizontal)
        folderField.setContentCompressionResistancePriority(.defaultLow, for: .horizontal)
        let choose = NSButton(title: "Choose…", target: self, action: #selector(chooseFolder))
        let useDefault = NSButton(title: "Use Default", target: self, action: #selector(resetFolder))
        let folderRow = NSStackView(views: [folderField, choose, useDefault])
        folderRow.orientation = .horizontal
        folderRow.spacing = 6
        small(folderNote)

        // Keep for
        for retention in Retention.allCases {
            keepPopUp.addItem(withTitle: retention.title)
            keepPopUp.lastItem?.representedObject = retention.rawValue
        }
        keepPopUp.target = self
        keepPopUp.action = #selector(keepChanged)
        let keepNote = NSTextField(wrappingLabelWithString: "Counted from when the smugshot was taken. Agents read the files within seconds of the paste, so a few minutes is plenty.")
        small(keepNote)

        // Clipboard text
        prefixField.delegate = self
        prefixField.placeholderString = "nothing in front of the path"
        let prefixNote = NSTextField(wrappingLabelWithString:
            "The text in front of the path, spaces included. It is there because a message that starts with / is read as a command by Claude Code and other chat tools.")
        small(prefixNote)
        preview.font = .monospacedSystemFont(ofSize: NSFont.smallSystemFontSize, weight: .regular)
        preview.textColor = .secondaryLabelColor
        preview.lineBreakMode = .byTruncatingMiddle
        preview.setContentCompressionResistancePriority(.defaultLow, for: .horizontal)

        // Gather
        for box in [nameControlsBox, readTextBox, webElementBox] {
            box.target = self
            box.action = #selector(gatherChanged)
        }
        let gather = NSStackView(views: [nameControlsBox, readTextBox, webElementBox])
        gather.orientation = .vertical
        gather.alignment = .leading
        gather.spacing = 4
        let gatherNote = NSTextField(wrappingLabelWithString: "The two pictures, the app and the window are always in shot.md. Switching a slow one off makes a smugshot land faster. \"What was there\" needs the Accessibility permission; for \"Web element\" the browser asks once.")
        small(gatherNote)

        // Sound
        for sound in Sound.allCases {
            soundPopUp.addItem(withTitle: sound.title)
            soundPopUp.lastItem?.representedObject = sound.rawValue
        }
        soundPopUp.target = self
        soundPopUp.action = #selector(soundChanged)

        // Quiet
        quietBox.target = self
        quietBox.action = #selector(quietChanged)

        let grid = NSGridView(views: [
            [label("Shortcut:"), shortcutRow],
            [NSGridCell.emptyContentView, shortcutNote],
            [label("Save to:"), folderRow],
            [NSGridCell.emptyContentView, folderNote],
            [label("Keep for:"), keepPopUp],
            [NSGridCell.emptyContentView, keepNote],
            [label("Clipboard text:"), prefixField],
            [NSGridCell.emptyContentView, prefixNote],
            [NSGridCell.emptyContentView, preview],
            [label("Gather:"), gather],
            [NSGridCell.emptyContentView, gatherNote],
            [label("Sound:"), soundPopUp],
            [label("Quiet:"), quietBox],
        ])
        grid.rowSpacing = 6
        grid.columnSpacing = 10
        grid.column(at: 0).xPlacement = .trailing
        grid.column(at: 1).width = 460
        for row in [2, 4, 6, 9, 11, 12] { grid.row(at: row).topPadding = 10 }
        for note in [shortcutNote, folderNote, keepNote, prefixNote, gatherNote] { note.preferredMaxLayoutWidth = 460 }
        grid.translatesAutoresizingMaskIntoConstraints = false

        let content = NSView()
        content.addSubview(grid)
        NSLayoutConstraint.activate([
            grid.topAnchor.constraint(equalTo: content.topAnchor, constant: 20),
            grid.leadingAnchor.constraint(equalTo: content.leadingAnchor, constant: 20),
            grid.trailingAnchor.constraint(equalTo: content.trailingAnchor, constant: -20),
            grid.bottomAnchor.constraint(equalTo: content.bottomAnchor, constant: -20),
        ])
        window.contentView = content
        window.setContentSize(content.fittingSize)
        self.window = window
    }

    private func label(_ text: String) -> NSTextField {
        let field = NSTextField(labelWithString: text)
        field.alignment = .right
        return field
    }

    private func small(_ field: NSTextField) {
        field.font = .systemFont(ofSize: NSFont.smallSystemFontSize)
        field.textColor = .secondaryLabelColor
    }

    // MARK: Showing the current values

    private func load() {
        if recording == nil { shortcutButton.title = Settings.shortcut.label }
        shortcutNote.stringValue = shortcutTaken
            ? "⚠️ That shortcut is taken by another app or by macOS. Pick another."
            : "Press it once, let go, then drag. It needs Command, Control or Option."
        folderField.stringValue = (Settings.folder.path as NSString).abbreviatingWithTildeInPath
        folderNote.stringValue = Settings.usesDefaultFolder
            ? "Private to you, skipped by Spotlight and Time Machine."
            : "The Claude Code skill was allowed to read ~/.smugshots only. With another folder, Claude Code asks before reading each smugshot."
        keepPopUp.selectItem(at: Retention.allCases.firstIndex(of: Settings.retention) ?? 0)
        prefixField.stringValue = Settings.prefix
        nameControlsBox.state = Settings.nameControls ? .on : .off
        readTextBox.state = Settings.readText ? .on : .off
        webElementBox.state = Settings.webElement ? .on : .off
        soundPopUp.selectItem(at: Sound.allCases.firstIndex(of: Settings.sound) ?? 0)
        quietBox.state = Settings.quiet ? .on : .off
        updatePreview()
    }

    private func updatePreview() {
        let example = Settings.folder.appendingPathComponent("2026-09-17-143207/shot.md").path
        preview.stringValue = "Pastes as:  " + Settings.clipboardText(for: example)
        preview.toolTip = preview.stringValue
    }

    // MARK: Shortcut

    /// Click the button, press the keys. Esc or clicking again keeps the old shortcut.
    @objc private func recordShortcut() {
        if recording != nil { stopRecording(); return }
        shortcutButton.title = "Press keys…"
        shortcutNote.stringValue = "Press the new shortcut. Esc keeps the old one."
        recording = NSEvent.addLocalMonitorForEvents(matching: .keyDown) { [weak self] event in
            guard let self else { return event }
            if event.keyCode == 53 { self.stopRecording(); return nil } // Esc
            guard let shortcut = Shortcut(event: event) else {
                self.shortcutNote.stringValue = "Add Command, Control or Option; a plain key cannot be taken from every app."
                return nil
            }
            self.stopRecording()
            Settings.shortcut = shortcut
            self.load()
            return nil
        }
    }

    private func stopRecording() {
        if let recording { NSEvent.removeMonitor(recording) }
        recording = nil
        load()
    }

    @objc private func resetShortcut() {
        stopRecording()
        Settings.shortcut = Settings.defaultShortcut
        load()
    }

    // MARK: Other changes

    @objc private func chooseFolder() {
        guard let window else { return }
        let panel = NSOpenPanel()
        panel.canChooseDirectories = true
        panel.canChooseFiles = false
        panel.canCreateDirectories = true
        panel.allowsMultipleSelection = false
        panel.directoryURL = Settings.folder
        panel.prompt = "Use This Folder"
        panel.message = "Smugshots are written to their own timestamped folders inside the folder you pick."
        panel.beginSheetModal(for: window) { [weak self] response in
            guard response == .OK, let url = panel.url else { return }
            Settings.folder = url
            Store.prepare()
            self?.load()
        }
    }

    @objc private func resetFolder() {
        Settings.folder = Settings.defaultFolder
        Store.prepare()
        load()
    }

    @objc private func keepChanged() {
        guard let raw = keepPopUp.selectedItem?.representedObject as? String, let retention = Retention(rawValue: raw) else { return }
        Settings.retention = retention
    }

    @objc private func gatherChanged() {
        Settings.nameControls = nameControlsBox.state == .on
        Settings.readText = readTextBox.state == .on
        Settings.webElement = webElementBox.state == .on
        if Settings.nameControls, !Accessibility.isAllowed { Accessibility.askForPermission() }
    }

    /// Plays the choice, Quiet or not: picking a sound is asking to hear it.
    @objc private func soundChanged() {
        guard let raw = soundPopUp.selectedItem?.representedObject as? String, let sound = Sound(rawValue: raw) else { return }
        Settings.sound = sound
        sound.play()
    }

    @objc private func quietChanged() { Settings.quiet = quietBox.state == .on }

    func controlTextDidChange(_ notification: Notification) {
        guard (notification.object as? NSTextField) === prefixField else { return }
        Settings.prefix = prefixField.stringValue
        updatePreview()
    }
}

extension SettingsWindow: NSWindowDelegate {
    func windowWillClose(_ notification: Notification) { stopRecording() }
}
