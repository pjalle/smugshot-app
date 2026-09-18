import AppKit

/// The menu bar glyph: four crop corners around a smirk with one raised eyebrow.
/// Drawn in code as a template image so macOS tints it for light and dark menu bars.
enum MenuBarIcon {
    static func image(size: CGFloat = 18) -> NSImage {
        let image = NSImage(size: NSSize(width: size, height: size), flipped: false) { _ in
            let u = size / 18 // the glyph is designed on an 18-point grid
            NSColor.black.setStroke()
            NSColor.black.setFill()

            let corners = NSBezierPath()
            corners.lineWidth = 1.5 * u
            corners.lineCapStyle = .round
            corners.lineJoinStyle = .round
            let lo = 1.5 * u, hi = 16.5 * u, arm = 3.6 * u
            for (x, y, dx, dy) in [(lo, lo, arm, arm), (hi, lo, -arm, arm), (lo, hi, arm, -arm), (hi, hi, -arm, -arm)] {
                corners.move(to: NSPoint(x: x + dx, y: y))
                corners.line(to: NSPoint(x: x, y: y))
                corners.line(to: NSPoint(x: x, y: y + dy))
            }
            corners.stroke()

            NSBezierPath(rect: NSRect(x: 5.4 * u, y: 8.6 * u, width: 1.9 * u, height: 1.9 * u)).fill()
            NSBezierPath(rect: NSRect(x: 10.7 * u, y: 8.6 * u, width: 1.9 * u, height: 1.9 * u)).fill()

            let face = NSBezierPath()
            face.lineWidth = 1.25 * u
            face.lineCapStyle = .round
            // The raised eyebrow over the right eye.
            face.move(to: NSPoint(x: 9.7 * u, y: 12.2 * u))
            face.curve(to: NSPoint(x: 12.7 * u, y: 12.6 * u),
                       controlPoint1: NSPoint(x: 10.4 * u, y: 13.9 * u), controlPoint2: NSPoint(x: 11.9 * u, y: 14.0 * u))
            // The smirk, higher on the right.
            face.move(to: NSPoint(x: 5.6 * u, y: 6.2 * u))
            face.curve(to: NSPoint(x: 12.9 * u, y: 7.9 * u),
                       controlPoint1: NSPoint(x: 8.4 * u, y: 4.9 * u), controlPoint2: NSPoint(x: 11.9 * u, y: 5.2 * u))
            face.stroke()
            return true
        }
        image.isTemplate = true
        image.accessibilityDescription = "Smugshot"
        return image
    }
}
