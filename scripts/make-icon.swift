// Turns design/chosen/app-icon-source.png into Resources/AppIcon.icns.
// Usage: swift scripts/make-icon.swift   (from the repo root)
// The source tile fills its canvas and has white outside its corners, so it is
// re-cut to the Mac icon shape: an 824-pixel rounded tile centred on a clear 1024 canvas.
import AppKit

let source = NSImage(contentsOfFile: "design/chosen/app-icon-source.png")!.cgImage(forProposedRect: nil, context: nil, hints: nil)!
let canvas = 1024, tile: CGFloat = 824, radius: CGFloat = 185
let ctx = CGContext(data: nil, width: canvas, height: canvas, bitsPerComponent: 8, bytesPerRow: 0,
                    space: CGColorSpace(name: CGColorSpace.sRGB)!, bitmapInfo: CGImageAlphaInfo.premultipliedLast.rawValue)!
let box = CGRect(x: (CGFloat(canvas) - tile) / 2, y: (CGFloat(canvas) - tile) / 2, width: tile, height: tile)
ctx.setShadow(offset: CGSize(width: 0, height: -10), blur: 24, color: CGColor(gray: 0, alpha: 0.3))
ctx.addPath(CGPath(roundedRect: box, cornerWidth: radius, cornerHeight: radius, transform: nil))
ctx.setFillColor(CGColor(gray: 0.18, alpha: 1)); ctx.fillPath()
ctx.setShadow(offset: .zero, blur: 0, color: nil)
ctx.addPath(CGPath(roundedRect: box, cornerWidth: radius, cornerHeight: radius, transform: nil)); ctx.clip()
ctx.interpolationQuality = .high
ctx.draw(source, in: box.insetBy(dx: -tile * 0.03, dy: -tile * 0.03)) // slightly oversized so the source's own corners fall outside the cut
let master = ctx.makeImage()!

let set = URL(fileURLWithPath: NSTemporaryDirectory()).appendingPathComponent("AppIcon.iconset")
try? FileManager.default.removeItem(at: set)
try! FileManager.default.createDirectory(at: set, withIntermediateDirectories: true)
for (points, scale) in [(16,1),(16,2),(32,1),(32,2),(128,1),(128,2),(256,1),(256,2),(512,1),(512,2)] {
    let px = points * scale
    let c = CGContext(data: nil, width: px, height: px, bitsPerComponent: 8, bytesPerRow: 0,
                      space: CGColorSpace(name: CGColorSpace.sRGB)!, bitmapInfo: CGImageAlphaInfo.premultipliedLast.rawValue)!
    c.interpolationQuality = .high
    c.draw(master, in: CGRect(x: 0, y: 0, width: px, height: px))
    let name = "icon_\(points)x\(points)\(scale == 2 ? "@2x" : "").png"
    let dest = CGImageDestinationCreateWithURL(set.appendingPathComponent(name) as CFURL, "public.png" as CFString, 1, nil)!
    CGImageDestinationAddImage(dest, c.makeImage()!, nil); CGImageDestinationFinalize(dest)
}
let tool = Process()
tool.executableURL = URL(fileURLWithPath: "/usr/bin/iconutil")
tool.arguments = ["-c", "icns", set.path, "-o", "Resources/AppIcon.icns"]
try! tool.run(); tool.waitUntilExit()
print(tool.terminationStatus == 0 ? "Resources/AppIcon.icns written" : "iconutil failed")
