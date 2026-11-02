import AppKit
import Carbon.HIToolbox

// A marked-up smugshot: press the mark-up shortcut (Command + Shift + 2 unless Settings say otherwise), drag a
// spot, type a note for it in the box that opens beside it, and keep going. Enter saves the note and freezes
// the screen again for the next spot, Shift + Enter is a new line in the note, Esc in the box drops that spot,
// and Command + Enter, Esc on the frozen screen or either shortcut ends the mark-up. Everything lands in one
// folder, crop-01.png and full-01.png onwards, with one shot.md listing the spots and their notes in order,
// and that path goes on the clipboard like any smugshot.

/// One spot in a marked-up smugshot: one drag and the note typed for it. A class, because the slow reads
/// (controls, web element, text) fill it in while the next spot is being dragged.
final class Spot {
    let crop: String          // file names in the smugshot's folder, numbered from 01
    let full: String
    let region: CGRect        // in the full picture's pixels
    let fullSize: CGSize
    let target: Target
    var note = ""
    var url: String?
    var controls: Accessibility.Description?
    var web: Browser.Result?
    var words: [String] = []

    init(crop: String, full: String, region: CGRect, fullSize: CGSize, target: Target) {
        self.crop = crop
        self.full = full
        self.region = region
        self.fullSize = fullSize
        self.target = target
    }

    /// The file names for spot n, counted from 1.
    static func files(_ n: Int) -> (crop: String, full: String) {
        (String(format: "crop-%02d.png", n), String(format: "full-%02d.png", n))
    }
}

extension ShotFile {
    /// shot.md for a marked-up smugshot: the spots in the order they were dragged, each with its note, its two
    /// pictures and what was read for it. The same shape as the Windows version writes.
    static func markupText(folder: URL, date: Date, spots: [Spot]) -> String {
        let formatter = DateFormatter()
        formatter.locale = Locale(identifier: "en_US_POSIX")
        formatter.dateFormat = "yyyy-MM-dd HH:mm:ss"
        let n = spots.count
        let (word, each) = n == 1 ? ("spot", "it") : ("spots", "each")
        var lines = [
            "# Smugshot",
            "The user marked \(n) \(word) on their screen, typed a note for \(each), and pasted this path so you can see what they mean.",
            "Take the spots in order. For each, read the note, open its close-up (sharp, the spot is outlined), then its whole screen (the spot outlined, the rest dimmed) when you need the surroundings.",
            "",
            "- When: \(formatter.string(from: date))",
            "- Spots: \(n)",
        ]
        for (i, spot) in spots.enumerated() {
            lines += ["", "## Spot \(i + 1)"]
            let note = spot.note.replacingOccurrences(of: "\r\n", with: "\n").trimmingCharacters(in: .whitespacesAndNewlines)
            if note.isEmpty {
                lines.append("No note, only the spot.")
            } else {
                lines.append("The user's note:")
                for line in note.split(separator: "\n", omittingEmptySubsequences: false) {
                    lines.append("> \(line)".replacingOccurrences(of: "\\s+$", with: "", options: .regularExpression))
                }
            }
            lines += ["",
                      "- Close-up: \(folder.appendingPathComponent(spot.crop).path)",
                      "- Whole screen: \(folder.appendingPathComponent(spot.full).path)",
                      "- App: \(spot.target.appName)"]
            if let title = spot.target.windowTitle { lines.append("- Window: \(title)") }
            if let url = spot.url { lines.append("- URL: \(url)") }
            lines.append("- Region in \(spot.full) (pixels): x \(Int(spot.region.minX)), y \(Int(spot.region.minY)), w \(Int(spot.region.width)), h \(Int(spot.region.height)) of \(Int(spot.fullSize.width))x\(Int(spot.fullSize.height))")
            if let controls = spot.controls {
                lines += ["", "### What was there", "From the Mac's accessibility layer. The line marked > is the control that best matches the drag; indented lines below it are inside the dragged area."]
                if !controls.lines.isEmpty { lines += ["```"] + controls.lines + ["```"] }
                if let note = controls.note { lines.append(note) }
            }
            if let web = spot.web {
                lines += ["", "### Web element", "The page element that best matches the drag, read from the browser."]
                if !web.lines.isEmpty { lines += ["```"] + web.lines + ["```"] }
                if let note = web.note { lines.append(note) }
            }
            if !spot.words.isEmpty {
                lines += ["", "### Text read from the close-up", "Recognised from the picture, so expect small mistakes.", "```"] + spot.words + ["```"]
            }
        }
        return lines.joined(separator: "\n") + "\n"
    }

    /// The line at the top of the frozen screen while marking up: which spot is next and how to stop. `paste`
    /// is the usual pasting line, or nil.
    static func markupHint(next: Int, paste: String?) -> String {
        var line = next == 1 ? "Marking up: drag the first spot · Esc to stop" : "Marking up: drag spot \(next) · Esc when done"
        if let paste { line += " · " + paste }
        return line
    }

    /// The line above the note box for spot n.
    static func noteLabel(_ n: Int) -> String {
        "Spot \(n): what about it?  ↩ next spot · ⇧↩ new line · ⌘↩ done · ⎋ drop this spot"
    }
}

/// The small box beside a spot where the note is typed. A panel that takes the keyboard without activating
/// Smugshot, so when it goes the keyboard is back where it was.
final class NoteWindow {
    private var panel: NotePanel?
    private let label = NSTextField(wrappingLabelWithString: "")
    private let textView = NoteTextView()
    private(set) var isOpen = false

    /// Enter: the note is done, on to the next spot. Command or Control + Enter: the note is done and so is
    /// the mark-up. Esc: drop this spot.
    var onNext: (() -> Void)?
    var onDone: (() -> Void)?
    var onDrop: (() -> Void)?

    var text: String { textView.string }

    /// Shows the box for spot n beside the spot: below it, or above when there is no room below, kept on the
    /// spot's screen. `rect` is the drag in points with a top-left origin on `screen`, as the drag layer reports it.
    func open(spot n: Int, below rect: CGRect, on screen: NSScreen) {
        if panel == nil { build() }
        guard let panel else { return }
        label.stringValue = ShotFile.noteLabel(n)
        textView.string = ""

        let size = panel.frame.size
        let gap: CGFloat = 10
        let work = screen.visibleFrame
        // The drag in the Mac's own coordinates: counted from the bottom-left.
        let left = screen.frame.minX + rect.minX
        let top = screen.frame.maxY - rect.minY
        let bottom = screen.frame.maxY - rect.maxY
        var x = left
        var y = bottom - gap - size.height
        if y < work.minY { y = top + gap }
        if y + size.height > work.maxY { y = work.minY + gap } // a spot as tall as the screen: over its bottom edge
        if x + size.width > work.maxX { x = work.maxX - size.width - gap }
        if x < work.minX { x = work.minX + gap }
        panel.setFrameOrigin(NSPoint(x: x.rounded(), y: y.rounded()))
        panel.orderFrontRegardless()
        panel.makeKey()
        panel.makeFirstResponder(textView)
        isOpen = true
    }

    func hide() {
        panel?.orderOut(nil)
        isOpen = false
    }

    private func build() {
        let panel = NotePanel(contentRect: NSRect(x: 0, y: 0, width: 380, height: 150),
                              styleMask: [.borderless, .nonactivatingPanel], backing: .buffered, defer: false)
        panel.level = .screenSaver
        panel.collectionBehavior = [.canJoinAllSpaces, .fullScreenAuxiliary, .stationary]
        panel.isReleasedWhenClosed = false
        panel.hasShadow = true
        panel.isOpaque = false
        panel.backgroundColor = .clear

        let content = NSView(frame: panel.contentLayoutRect)
        content.wantsLayer = true
        content.layer?.backgroundColor = NSColor.windowBackgroundColor.cgColor
        content.layer?.cornerRadius = 10
        content.layer?.borderWidth = 1
        content.layer?.borderColor = NSColor.separatorColor.cgColor

        label.font = .systemFont(ofSize: NSFont.smallSystemFontSize)
        label.textColor = .secondaryLabelColor
        label.frame = NSRect(x: 12, y: 150 - 12 - 32, width: 356, height: 32)
        label.autoresizingMask = [.width, .minYMargin]
        content.addSubview(label)

        let scroll = NSScrollView(frame: NSRect(x: 12, y: 12, width: 356, height: 150 - 12 - 32 - 8 - 12))
        scroll.hasVerticalScroller = true
        scroll.borderType = .bezelBorder
        scroll.autoresizingMask = [.width, .height]
        textView.font = .systemFont(ofSize: 13)
        textView.isRichText = false
        textView.isAutomaticQuoteSubstitutionEnabled = false
        textView.isAutomaticDashSubstitutionEnabled = false
        textView.textContainerInset = NSSize(width: 4, height: 6)
        textView.minSize = NSSize(width: 0, height: scroll.contentSize.height)
        textView.maxSize = NSSize(width: CGFloat.greatestFiniteMagnitude, height: CGFloat.greatestFiniteMagnitude)
        textView.isVerticallyResizable = true
        textView.isHorizontallyResizable = false
        textView.autoresizingMask = [.width]
        textView.textContainer?.containerSize = NSSize(width: scroll.contentSize.width, height: CGFloat.greatestFiniteMagnitude)
        textView.textContainer?.widthTracksTextView = true
        textView.onNext = { [weak self] in self?.onNext?() }
        textView.onDone = { [weak self] in self?.onDone?() }
        textView.onDrop = { [weak self] in self?.onDrop?() }
        scroll.documentView = textView
        content.addSubview(scroll)

        panel.contentView = content
        self.panel = panel
    }
}

/// A borderless panel that can take the keyboard.
final class NotePanel: NSPanel {
    override var canBecomeKey: Bool { true }
    override var canBecomeMain: Bool { false }
}

/// The note's text, with the keys that move a mark-up along.
final class NoteTextView: NSTextView {
    var onNext: (() -> Void)?
    var onDone: (() -> Void)?
    var onDrop: (() -> Void)?

    override func keyDown(with event: NSEvent) {
        let flags = event.modifierFlags.intersection(.deviceIndependentFlagsMask)
        switch Int(event.keyCode) {
        case kVK_Return, kVK_ANSI_KeypadEnter:
            if flags.contains(.shift) { insertNewline(nil); return }
            if flags.contains(.command) || flags.contains(.control) { onDone?() } else { onNext?() }
        case kVK_Escape:
            onDrop?()
        default:
            super.keyDown(with: event)
        }
    }
}
