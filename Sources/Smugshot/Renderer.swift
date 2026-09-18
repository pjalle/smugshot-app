import AppKit
import UniformTypeIdentifiers

/// Turns the raw screen picture and the dragged rectangle into full.png and crop.png.
enum Renderer {
    static let highlight = NSColor(srgbRed: 1.0, green: 0.18, blue: 0.33, alpha: 1.0)
    /// Agents shrink anything larger than this before the model sees it.
    static let maxEdge: CGFloat = 2000

    /// Whole screen, dragged part outlined, the rest dimmed. Returns the image and the region in its pixels.
    static func full(image: CGImage, region: CGRect) -> (CGImage, CGRect)? {
        let size = CGSize(width: image.width, height: image.height)
        let scale = min(1, maxEdge / max(size.width, size.height))
        let outSize = CGSize(width: (size.width * scale).rounded(), height: (size.height * scale).rounded())
        let outRegion = region.applying(CGAffineTransform(scaleX: scale, y: scale)).integral
        guard let ctx = context(outSize) else { return nil }

        ctx.interpolationQuality = .high
        ctx.draw(image, in: CGRect(origin: .zero, size: outSize))

        let flipped = flip(outRegion, in: outSize.height)
        ctx.addRect(CGRect(origin: .zero, size: outSize))
        ctx.addRect(flipped)
        ctx.setFillColor(NSColor.black.withAlphaComponent(0.35).cgColor)
        ctx.fillPath(using: .evenOdd)
        outline(flipped, width: 4, in: ctx)

        return ctx.makeImage().map { ($0, outRegion) }
    }

    /// The dragged part at full sharpness with a margin around it.
    static func crop(image: CGImage, region: CGRect) -> CGImage? {
        let bounds = CGRect(x: 0, y: 0, width: image.width, height: image.height)
        let margin = max(40, 0.15 * max(region.width, region.height))
        let padded = region.insetBy(dx: -margin, dy: -margin).intersection(bounds).integral
        guard let cut = image.cropping(to: padded) else { return nil }

        let scale = min(1, maxEdge / max(padded.width, padded.height))
        let outSize = CGSize(width: (padded.width * scale).rounded(), height: (padded.height * scale).rounded())
        guard let ctx = context(outSize) else { return nil }
        ctx.interpolationQuality = .high
        ctx.draw(cut, in: CGRect(origin: .zero, size: outSize))

        let inner = region.offsetBy(dx: -padded.minX, dy: -padded.minY)
            .applying(CGAffineTransform(scaleX: scale, y: scale))
        outline(flip(inner, in: outSize.height), width: 3, in: ctx)
        return ctx.makeImage()
    }

    /// Just the dragged part, unmarked, for reading its text.
    static func raw(image: CGImage, region: CGRect) -> CGImage? {
        image.cropping(to: region.intersection(CGRect(x: 0, y: 0, width: image.width, height: image.height)))
    }

    static func writePNG(_ image: CGImage, to url: URL) -> Bool {
        guard let dest = CGImageDestinationCreateWithURL(url as CFURL, UTType.png.identifier as CFString, 1, nil) else { return false }
        CGImageDestinationAddImage(dest, image, nil)
        return CGImageDestinationFinalize(dest)
    }

    private static func context(_ size: CGSize) -> CGContext? {
        CGContext(data: nil, width: Int(size.width), height: Int(size.height), bitsPerComponent: 8, bytesPerRow: 0,
                  space: CGColorSpace(name: CGColorSpace.sRGB)!, bitmapInfo: CGImageAlphaInfo.premultipliedLast.rawValue)
    }

    /// Pictures count from the top-left, drawing contexts from the bottom-left.
    private static func flip(_ rect: CGRect, in height: CGFloat) -> CGRect {
        CGRect(x: rect.minX, y: height - rect.maxY, width: rect.width, height: rect.height)
    }

    /// Drawn just outside the region so it never covers what was pointed at.
    private static func outline(_ rect: CGRect, width: CGFloat, in ctx: CGContext) {
        ctx.setStrokeColor(highlight.cgColor)
        ctx.setLineWidth(width)
        ctx.stroke(rect.insetBy(dx: -width / 2, dy: -width / 2))
    }
}
