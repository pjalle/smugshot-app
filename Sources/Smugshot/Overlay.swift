import AppKit

final class OverlayPanel: NSPanel {
    override var canBecomeKey: Bool { false }
    override var canBecomeMain: Bool { false }
}

/// Covers one screen, catches the drag, and reports the dragged rectangle
/// in points with a top-left origin (the same way round as the picture).
final class OverlayView: NSView {
    var onFinish: ((CGRect?) -> Void)?
    private var start: NSPoint?
    private var current: NSPoint?

    override var isFlipped: Bool { true }
    override func acceptsFirstMouse(for event: NSEvent?) -> Bool { true }

    override func resetCursorRects() {
        addCursorRect(bounds, cursor: .crosshair)
    }

    private var selection: CGRect? {
        guard let start, let current else { return nil }
        return CGRect(x: min(start.x, current.x), y: min(start.y, current.y),
                      width: abs(start.x - current.x), height: abs(start.y - current.y))
    }

    override func mouseDown(with event: NSEvent) {
        start = convert(event.locationInWindow, from: nil)
        current = start
        needsDisplay = true
    }

    override func rightMouseDown(with event: NSEvent) {
        start = nil
        current = nil
        onFinish?(nil)
    }

    override func mouseDragged(with event: NSEvent) {
        current = convert(event.locationInWindow, from: nil)
        needsDisplay = true
    }

    override func mouseUp(with event: NSEvent) {
        current = convert(event.locationInWindow, from: nil)
        let rect = selection?.intersection(bounds)
        start = nil
        current = nil
        if let rect, rect.width >= 4, rect.height >= 4 { onFinish?(rect) } else { onFinish?(nil) }
    }

    override func draw(_ dirtyRect: NSRect) {
        // The faint dim also makes the window solid enough to catch clicks.
        let dim = NSBezierPath(rect: bounds)
        if let selection {
            dim.append(NSBezierPath(rect: selection))
            dim.windingRule = .evenOdd
        }
        NSColor.black.withAlphaComponent(0.18).setFill()
        dim.fill()

        if let selection {
            let outline = NSBezierPath(rect: selection.insetBy(dx: -1, dy: -1))
            outline.lineWidth = 2
            Renderer.highlight.setStroke()
            outline.stroke()
        }
    }
}

/// One drag layer per screen, so the drag can start on whichever screen the user goes to.
final class Overlay {
    private var panels: [OverlayPanel] = []
    private var hints: [HintView] = []

    /// `frozen` holds the picture already taken of each screen. It is shown under the drag layer, so the screen
    /// stands still the way it does for the Mac's own Command+Shift+4: a hover state or a tooltip stays visible
    /// while dragging, even though the app underneath has stopped seeing the mouse.
    func show(on screens: [NSScreen], frozen: [ObjectIdentifier: CGImage] = [:],
              onFinish: @escaping (NSScreen, CGRect?) -> Void) {
        for screen in screens {
            let panel = OverlayPanel(contentRect: screen.frame, styleMask: [.borderless, .nonactivatingPanel],
                                     backing: .buffered, defer: false)
            panel.level = .screenSaver
            panel.collectionBehavior = [.canJoinAllSpaces, .fullScreenAuxiliary, .stationary]
            panel.isOpaque = false
            panel.backgroundColor = .clear
            panel.hasShadow = false
            panel.ignoresMouseEvents = false
            panel.isReleasedWhenClosed = false

            let view = OverlayView(frame: NSRect(origin: .zero, size: screen.frame.size))
            view.onFinish = { rect in onFinish(screen, rect) }
            if let image = frozen[ObjectIdentifier(screen)] {
                // A layer, not drawn in draw(_:), so a drag does not repaint a full Retina picture each time.
                let still = NSView(frame: view.frame)
                still.wantsLayer = true
                still.layer?.contents = image
                still.layer?.contentsGravity = .resize
                view.autoresizingMask = [.width, .height]
                still.addSubview(view)
                panel.contentView = still
            } else {
                panel.contentView = view
            }
            if let content = panel.contentView { hints.append(Self.addHint(to: content)) }
            panel.setFrame(screen.frame, display: true)
            panel.orderFrontRegardless()
            panels.append(panel)
        }
        NSCursor.crosshair.set()
    }

    /// One line at the top of every screen, or nothing: "Pastes into Ghostty after the drag · 1 switches it off".
    func showHint(_ text: String?) {
        for hint in hints { hint.show(text) }
    }

    private static func addHint(to view: NSView) -> HintView {
        let hint = HintView()
        view.addSubview(hint)
        return hint
    }

    func hide() {
        panels.forEach { $0.orderOut(nil) }
        panels = []
        hints = []
        NSCursor.arrow.set()
    }
}

/// A dark pill with one line of white text, near the top of a screen.
final class HintView: NSView {
    private let label = NSTextField(labelWithString: "")
    private let inset = NSSize(width: 14, height: 8)

    init() {
        super.init(frame: .zero)
        wantsLayer = true
        layer?.backgroundColor = NSColor.black.withAlphaComponent(0.72).cgColor
        layer?.cornerRadius = 9
        label.font = .systemFont(ofSize: 13, weight: .medium)
        label.textColor = .white
        addSubview(label)
        isHidden = true
    }

    required init?(coder: NSCoder) { fatalError() }

    /// The drag goes through it to the layer underneath.
    override func hitTest(_ point: NSPoint) -> NSView? { nil }

    func show(_ text: String?) {
        guard let text, let superview else { isHidden = true; return }
        label.stringValue = text
        label.sizeToFit()
        let size = NSSize(width: label.frame.width + inset.width * 2, height: label.frame.height + inset.height * 2)
        frame = NSRect(x: ((superview.bounds.width - size.width) / 2).rounded(), y: superview.bounds.height - 72 - size.height,
                       width: size.width, height: size.height)
        label.frame = NSRect(origin: NSPoint(x: inset.width, y: inset.height), size: label.frame.size)
        isHidden = false
    }
}
