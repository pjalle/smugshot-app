import AppKit
import ScreenCaptureKit

enum CaptureError: Error { case displayNotFound }

/// Takes a one-shot picture of a whole display.
final class Capturer {
    private var content: SCShareableContent?

    /// Looking up the displays is the slow part, so it is done ahead of time.
    func refresh() async {
        content = try? await SCShareableContent.excludingDesktopWindows(false, onScreenWindowsOnly: true)
    }

    func capture(screen: NSScreen) async throws -> CGImage {
        if content == nil { await refresh() }
        guard let content,
              let displayID = screen.deviceDescription[NSDeviceDescriptionKey("NSScreenNumber")] as? CGDirectDisplayID,
              let display = content.displays.first(where: { $0.displayID == displayID })
        else { throw CaptureError.displayNotFound }

        // Nothing is left out. The drag layer goes up only after the picture is taken, so it cannot be in it,
        // and Smugshot's own Settings window can be pointed at like any other.
        let filter = SCContentFilter(display: display, excludingWindows: [])

        // Without an explicit pixel size the picture comes out at 1x and blurry.
        let config = SCStreamConfiguration()
        let scale = CGFloat(filter.pointPixelScale)
        config.width = Int(filter.contentRect.width * scale)
        config.height = Int(filter.contentRect.height * scale)
        config.showsCursor = false
        config.captureResolution = .best

        return try await SCScreenshotManager.captureImage(contentFilter: filter, configuration: config)
    }
}
