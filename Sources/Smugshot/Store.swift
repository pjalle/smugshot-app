import AppKit

/// Where smugshots live on disk: <folder>/<timestamp>/, private, gone after the retention period.
/// The folder is ~/.smugshots unless changed in Settings.
enum Store {
    static var root: URL { Settings.folder }

    /// A smugshot folder is named 2026-09-17-143207, or 2026-09-17-143207-2 when two land in the same second.
    /// The sweep only ever deletes folders named like this, so a user-chosen folder that holds other things is safe.
    private static let namePattern = try! NSRegularExpression(pattern: "^\\d{4}-\\d{2}-\\d{2}-\\d{6}(-\\d+)?$")

    static func prepare() {
        let fm = FileManager.default
        let folder = root
        guard !fm.fileExists(atPath: folder.path) else { return }
        try? fm.createDirectory(at: folder, withIntermediateDirectories: true, attributes: [.posixPermissions: 0o700])
        // Keep it out of Time Machine. The default dot-folder is already skipped by Spotlight.
        let tmutil = Process()
        tmutil.executableURL = URL(fileURLWithPath: "/usr/bin/tmutil")
        tmutil.arguments = ["addexclusion", folder.path]
        try? tmutil.run()
    }

    static func newFolder(at date: Date) throws -> URL {
        prepare()
        let formatter = DateFormatter()
        formatter.locale = Locale(identifier: "en_US_POSIX")
        formatter.dateFormat = "yyyy-MM-dd-HHmmss"
        var url = root.appendingPathComponent(formatter.string(from: date), isDirectory: true)
        var n = 2
        while FileManager.default.fileExists(atPath: url.path) {
            url = root.appendingPathComponent("\(formatter.string(from: date))-\(n)", isDirectory: true)
            n += 1
        }
        try FileManager.default.createDirectory(at: url, withIntermediateDirectories: true, attributes: [.posixPermissions: 0o700])
        return url
    }

    /// Deletes smugshots older than the "Keep for" setting. Does nothing when that is "Forever".
    static func sweep() {
        guard let lifetime = Settings.retention.lifetime else { return }
        let fm = FileManager.default
        guard let items = try? fm.contentsOfDirectory(at: root, includingPropertiesForKeys: [.creationDateKey]) else { return }
        let cutoff = Date().addingTimeInterval(-lifetime)
        for item in items where isSmugshot(item) {
            let created = (try? item.resourceValues(forKeys: [.creationDateKey]))?.creationDate ?? Date()
            if created < cutoff { try? fm.removeItem(at: item) }
        }
    }

    /// The newest smugshots, newest first. The folder names sort by time.
    static func recent(limit: Int) -> [URL] {
        let items = (try? FileManager.default.contentsOfDirectory(at: root, includingPropertiesForKeys: nil)) ?? []
        return Array(items.filter(isSmugshot).sorted { $0.lastPathComponent > $1.lastPathComponent }.prefix(limit))
    }

    static func isSmugshot(_ url: URL) -> Bool {
        let name = url.lastPathComponent
        guard namePattern.firstMatch(in: name, range: NSRange(name.startIndex..., in: name)) != nil else { return false }
        var isDirectory: ObjCBool = false
        return FileManager.default.fileExists(atPath: url.path, isDirectory: &isDirectory) && isDirectory.boolValue
    }
}

/// The app and window under the drag (not necessarily the app in front).
struct Target {
    let appName: String
    let bundleID: String?
    let pid: pid_t?
    let windowTitle: String?

    /// `point` is in global screen points with a top-left origin.
    static func under(_ point: CGPoint) -> Target {
        let me = ProcessInfo.processInfo.processIdentifier
        let windows = CGWindowListCopyWindowInfo([.optionOnScreenOnly, .excludeDesktopElements], kCGNullWindowID) as? [[String: Any]] ?? []
        // Front to back: the first normal window that contains the point is the one the user saw there.
        for window in windows {
            guard (window[kCGWindowLayer as String] as? Int) == 0,
                  let pid = window[kCGWindowOwnerPID as String] as? pid_t, pid != me,
                  let box = window[kCGWindowBounds as String] as? NSDictionary,
                  let bounds = CGRect(dictionaryRepresentation: box), bounds.contains(point) else { continue }
            let app = NSRunningApplication(processIdentifier: pid)
            let title = window[kCGWindowName as String] as? String
            return Target(appName: app?.localizedName ?? (window[kCGWindowOwnerName as String] as? String) ?? "Unknown",
                          bundleID: app?.bundleIdentifier, pid: pid, windowTitle: title?.isEmpty == false ? title : nil)
        }
        let front = NSWorkspace.shared.frontmostApplication
        return Target(appName: front?.localizedName ?? "Desktop", bundleID: front?.bundleIdentifier, pid: front?.processIdentifier, windowTitle: nil)
    }
}

enum ShotFile {
    /// One line for the menu, read back from shot.md: "14:32  Brave: Quill".
    static func summary(folder: URL) -> String {
        let name = folder.lastPathComponent // 2026-09-17-143207[-n]
        let time = name.count >= 17 ? String(name.dropFirst(11).prefix(2)) + ":" + String(name.dropFirst(13).prefix(2)) : name
        let text = (try? String(contentsOf: folder.appendingPathComponent("shot.md"), encoding: .utf8)) ?? ""
        func field(_ key: String) -> String? {
            text.split(separator: "\n").first { $0.hasPrefix("- \(key): ") }.map { String($0.dropFirst(key.count + 4)) }
        }
        var line = time
        if let app = field("App") { line += "  " + app }
        if let window = field("Window") { line += ": " + window }
        return line.count > 70 ? String(line.prefix(69)) + "…" : line
    }

    static func text(folder: URL, date: Date, target: Target, url: String?, region: CGRect, fullSize: CGSize,
                     controls: Accessibility.Description?, words: [String], web: Browser.Result?) -> String {
        let formatter = DateFormatter()
        formatter.locale = Locale(identifier: "en_US_POSIX")
        formatter.dateFormat = "yyyy-MM-dd HH:mm:ss"
        var lines = [
            "# Smugshot",
            "The user pointed at part of their screen and pasted this path so you can see what they mean.",
            "Open crop.png first (sharp close-up, the pointed-at part is outlined), then full.png (whole screen, the pointed-at part is outlined and the rest is dimmed).",
            "",
            "- Close-up: \(folder.appendingPathComponent("crop.png").path)",
            "- Whole screen: \(folder.appendingPathComponent("full.png").path)",
            "- When: \(formatter.string(from: date))",
            "- App: \(target.appName)",
        ]
        if let title = target.windowTitle { lines.append("- Window: \(title)") }
        if let url { lines.append("- URL: \(url)") }
        lines.append("- Region in full.png (pixels): x \(Int(region.minX)), y \(Int(region.minY)), w \(Int(region.width)), h \(Int(region.height)) of \(Int(fullSize.width))x\(Int(fullSize.height))")

        if let controls {
            lines += ["", "## What was there", "From the Mac's accessibility layer. The line marked > is the control that best matches the drag; indented lines below it are inside the dragged area."]
            if !controls.lines.isEmpty { lines += ["```"] + controls.lines + ["```"] }
            if let note = controls.note { lines.append(note) }
        }
        if let web {
            lines += ["", "## Web element", "The page element that best matches the drag, read from the browser."]
            if !web.lines.isEmpty { lines += ["```"] + web.lines + ["```"] }
            if let note = web.note { lines.append(note) }
        }
        if !words.isEmpty {
            lines += ["", "## Text read from the close-up", "Recognised from the picture, so expect small mistakes.", "```"] + words + ["```"]
        }
        return lines.joined(separator: "\n") + "\n"
    }
}
