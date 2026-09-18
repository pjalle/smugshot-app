import AppKit
import ApplicationServices

/// Reads what the Mac's accessibility layer (the one screen readers use) says about
/// the controls under the drag. Needs the Accessibility permission.
enum Accessibility {
    static var isAllowed: Bool { AXIsProcessTrusted() }

    static func askForPermission() {
        let options = [kAXTrustedCheckOptionPrompt.takeUnretainedValue() as String: true] as CFDictionary
        _ = AXIsProcessTrustedWithOptions(options)
    }

    // MARK: Warming up lazy apps

    private static var warmed = Set<pid_t>()
    private static let queue = DispatchQueue(label: "smugshot.accessibility", qos: .userInitiated)

    /// Browsers and Electron apps only publish their controls after something asks, and then
    /// take a second or two. Asking at drag time is too late, so ask whenever an app comes to the front.
    static func startWarming() {
        NSWorkspace.shared.notificationCenter.addObserver(forName: NSWorkspace.didActivateApplicationNotification,
                                                          object: nil, queue: nil) { note in
            guard let app = note.userInfo?[NSWorkspace.applicationUserInfoKey] as? NSRunningApplication else { return }
            warm(app.processIdentifier)
        }
        if let front = NSWorkspace.shared.frontmostApplication { warm(front.processIdentifier) }
    }

    private static func warm(_ pid: pid_t) {
        queue.async {
            guard isAllowed, pid != ProcessInfo.processInfo.processIdentifier, !warmed.contains(pid) else { return }
            warmed.insert(pid)
            let app = AXUIElementCreateApplication(pid)
            AXUIElementSetMessagingTimeout(app, 0.4)
            // The gentle ask: no side effects. (AXEnhancedUserInterface is the forceful one and disturbs window animations.)
            AXUIElementSetAttributeValue(app, "AXManualAccessibility" as CFString, kCFBooleanTrue)
            _ = string(app, kAXRoleAttribute) // Chromium switches its basic mode on when this is read
        }
    }

    // MARK: Describing a region

    struct Description {
        var lines: [String] = []       // indented outline, the pointed-at element marked with ">"
        var url: String?
        var webAreaFrame: CGRect?      // where the page sits on screen, for the browser step
        var note: String?
    }

    /// `region` is in global screen points with a top-left origin, the way the accessibility layer counts.
    static func describe(region: CGRect, pid: pid_t) -> Description {
        guard isAllowed else {
            return Description(note: "Smugshot has no Accessibility permission, so it cannot name the controls here.")
        }
        var result = Description()
        queue.sync {
            let app = AXUIElementCreateApplication(pid)
            AXUIElementSetMessagingTimeout(app, 0.4)
            let deadline = Date().addingTimeInterval(1.5)

            guard let target = bestElement(in: region, app: app) else {
                result.note = "This app published no controls under the drag (common in games, canvas apps and some Electron apps)."
                return
            }

            // Walk up to the window for the chain of parents.
            var chain: [AXUIElement] = [target]
            while let parent = element(chain[0], kAXParentAttribute), chain.count < 40, Date() < deadline {
                if string(parent, kAXRoleAttribute) == (kAXApplicationRole as String) { break }
                chain.insert(parent, at: 0)
            }
            for node in chain where string(node, kAXRoleAttribute) == "AXWebArea" {
                result.url = result.url ?? url(node)
                result.webAreaFrame = result.webAreaFrame ?? frame(node)
            }

            // Parents are summarised; containers with no label are skipped to keep it short.
            var depth = 0
            for node in chain.dropLast() {
                let line = summary(node)
                let role = string(node, kAXRoleAttribute) ?? ""
                if line.contains("\"") || ["AXWindow", "AXWebArea", "AXToolbar", "AXTable", "AXOutline", "AXSheet", "AXMenu", "AXTabGroup"].contains(role) {
                    result.lines.append(String(repeating: "  ", count: depth) + line)
                    depth += 1
                }
            }
            result.lines.append(String(repeating: "  ", count: depth) + "> " + summary(target))

            // What else sits inside the dragged area.
            var budget = 1500
            var inside: [String] = []
            collect(target, region: region, depth: depth + 1, into: &inside, budget: &budget, deadline: deadline, skip: nil)
            if inside.isEmpty, let parent = chain.dropLast().last {
                collect(parent, region: region, depth: depth + 1, into: &inside, budget: &budget, deadline: deadline, skip: target)
            }
            result.lines.append(contentsOf: inside.prefix(60))
            if inside.count > 60 { result.lines.append("  … \(inside.count - 60) more not listed") }
            if Date() >= deadline { result.note = "Stopped early: this app answered slowly, so the list may be incomplete." }
        }
        return result
    }

    /// Asks the app what is at the centre and near the corners, and keeps the answer that fits the drag best.
    private static func bestElement(in region: CGRect, app: AXUIElement) -> AXUIElement? {
        let inset = region.insetBy(dx: region.width * 0.2, dy: region.height * 0.2)
        let points = [CGPoint(x: region.midX, y: region.midY),
                      CGPoint(x: inset.minX, y: inset.minY), CGPoint(x: inset.maxX, y: inset.minY),
                      CGPoint(x: inset.minX, y: inset.maxY), CGPoint(x: inset.maxX, y: inset.maxY)]
        var best: (AXUIElement, CGFloat)?
        for point in points {
            var hit: AXUIElement?
            guard AXUIElementCopyElementAtPosition(app, Float(point.x), Float(point.y), &hit) == .success, var node = hit else { continue }
            // Climb until the element is about as big as the drag: a drag over a whole panel means the panel, not one word in it.
            for _ in 0..<12 {
                let score = fit(frame(node), region)
                if best == nil || score > best!.1 { best = (node, score) }
                guard let f = frame(node), f.width * f.height < region.width * region.height,
                      let parent = element(node, kAXParentAttribute) else { break }
                node = parent
            }
        }
        return best?.0
    }

    /// Overlap divided by combined area: 1 is a perfect match, 0 is no overlap.
    private static func fit(_ frame: CGRect?, _ region: CGRect) -> CGFloat {
        guard let frame, !frame.isEmpty else { return 0 }
        let overlap = frame.intersection(region)
        guard !overlap.isNull else { return 0 }
        let shared = overlap.width * overlap.height
        return shared / (frame.width * frame.height + region.width * region.height - shared)
    }

    private static func collect(_ node: AXUIElement, region: CGRect, depth: Int, into lines: inout [String],
                                budget: inout Int, deadline: Date, skip: AXUIElement?) {
        // Big tables are read through their visible rows only; walking 200,000 rows takes forever.
        let kids = elements(node, "AXVisibleRows") ?? elements(node, kAXVisibleChildrenAttribute) ?? elements(node, kAXChildrenAttribute) ?? []
        for kid in kids {
            guard budget > 0, Date() < deadline, depth < 40 else { return }
            budget -= 1
            if let skip, CFEqual(kid, skip) { continue }
            guard let f = frame(kid), f.intersects(region) else { continue }
            let line = summary(kid)
            let worthListing = line.contains("\"") || line.contains("(") || line.contains("id:")
            if worthListing { lines.append(String(repeating: "  ", count: depth) + line) }
            collect(kid, region: region, depth: worthListing ? depth + 1 : depth, into: &lines, budget: &budget, deadline: deadline, skip: nil)
        }
    }

    /// One line per element: role, label, state, and for web content the DOM id and classes.
    private static func summary(_ node: AXUIElement) -> String {
        var parts = [string(node, kAXSubroleAttribute).flatMap { $0.isEmpty ? nil : $0 } ?? string(node, kAXRoleAttribute) ?? "AXUnknown"]
        let label = [kAXTitleAttribute, kAXDescriptionAttribute, "AXPlaceholderValue", kAXHelpAttribute]
            .compactMap { string(node, $0) }.first { !$0.isEmpty }
        if let label { parts.append("\"\(clip(label, 120))\"") }
        if let value = string(node, kAXValueAttribute), !value.isEmpty, value != label { parts.append("value:\"\(clip(value, 200))\"") }

        var states: [String] = []
        if bool(node, kAXEnabledAttribute) == false { states.append("disabled") }
        if bool(node, kAXFocusedAttribute) == true { states.append("focused") }
        if bool(node, kAXSelectedAttribute) == true { states.append("selected") }
        if bool(node, "AXExpanded") == true { states.append("expanded") }
        if !states.isEmpty { parts.append("(" + states.joined(separator: ", ") + ")") }

        if let id = string(node, "AXDOMIdentifier") ?? string(node, kAXIdentifierAttribute), !id.isEmpty { parts.append("id:\(id)") }
        if let classes = value(node, "AXDOMClassList") as? [String], !classes.isEmpty { parts.append("class:" + clip(classes.joined(separator: " "), 120)) }
        if let link = url(node), string(node, kAXRoleAttribute) == "AXLink" { parts.append("href:\(clip(link, 160))") }
        return parts.joined(separator: " ")
    }

    // MARK: Small readers

    private static func value(_ node: AXUIElement, _ attribute: String) -> AnyObject? {
        var out: AnyObject?
        return AXUIElementCopyAttributeValue(node, attribute as CFString, &out) == .success ? out : nil
    }

    private static func string(_ node: AXUIElement, _ attribute: String) -> String? {
        let v = value(node, attribute)
        if let s = v as? String { return s }
        if let n = v as? NSNumber, attribute == kAXValueAttribute { return n.stringValue }
        return nil
    }

    private static func bool(_ node: AXUIElement, _ attribute: String) -> Bool? { (value(node, attribute) as? NSNumber)?.boolValue }

    private static func element(_ node: AXUIElement, _ attribute: String) -> AXUIElement? {
        guard let v = value(node, attribute), CFGetTypeID(v) == AXUIElementGetTypeID() else { return nil }
        return (v as! AXUIElement)
    }

    private static func elements(_ node: AXUIElement, _ attribute: String) -> [AXUIElement]? {
        guard let list = value(node, attribute) as? [AXUIElement], !list.isEmpty else { return nil }
        return list
    }

    private static func url(_ node: AXUIElement) -> String? {
        let v = value(node, kAXURLAttribute)
        return (v as? URL)?.absoluteString ?? (v as? String)
    }

    static func frame(_ node: AXUIElement) -> CGRect? {
        guard let p = value(node, kAXPositionAttribute), let s = value(node, kAXSizeAttribute),
              CFGetTypeID(p) == AXValueGetTypeID(), CFGetTypeID(s) == AXValueGetTypeID() else { return nil }
        var origin = CGPoint.zero, size = CGSize.zero
        AXValueGetValue(p as! AXValue, .cgPoint, &origin)
        AXValueGetValue(s as! AXValue, .cgSize, &size)
        return CGRect(origin: origin, size: size)
    }

    private static func clip(_ text: String, _ max: Int) -> String {
        let flat = text.split(whereSeparator: \.isWhitespace).joined(separator: " ")
        return flat.count > max ? String(flat.prefix(max)) + "…" : flat
    }
}
