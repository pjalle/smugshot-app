import AppKit

/// "What's new": the changelog that build.sh puts in the app, shown in a small window. Read from the app's own
/// folder, never from the network.
final class WhatsNewWindow: NSObject {
    private var window: NSWindow?
    private let textView = NSTextView()

    static var version: String { Bundle.main.infoDictionary?["CFBundleShortVersionString"] as? String ?? "" }

    func show() {
        if window == nil { build() }
        textView.textStorage?.setAttributedString(Self.styled(Self.changelog()))
        textView.scrollToBeginningOfDocument(nil)
        NSApp.activate(ignoringOtherApps: true)
        window?.center()
        window?.makeKeyAndOrderFront(nil)
    }

    private func build() {
        let window = NSWindow(contentRect: NSRect(x: 0, y: 0, width: 560, height: 520),
                              styleMask: [.titled, .closable, .resizable], backing: .buffered, defer: false)
        window.title = "What's new in Smugshot"
        window.isReleasedWhenClosed = false

        let scroll = NSScrollView(frame: window.contentLayoutRect)
        scroll.hasVerticalScroller = true
        scroll.autoresizingMask = [.width, .height]
        textView.isEditable = false
        textView.textContainerInset = NSSize(width: 16, height: 16)
        textView.autoresizingMask = [.width]
        textView.frame = scroll.contentView.bounds
        scroll.documentView = textView
        window.contentView = scroll
        self.window = window
    }

    private static func changelog() -> String {
        guard let url = Bundle.main.url(forResource: "WhatsNew", withExtension: "md"),
              let text = try? String(contentsOf: url, encoding: .utf8) else {
            return "This copy of Smugshot was built without its changelog. See CHANGELOG.md next to the source."
        }
        return text
    }

    /// Enough of Markdown for a changelog: "## " headings, "- " bullets, **bold** and `code`.
    static func styled(_ markdown: String) -> NSAttributedString {
        let result = NSMutableAttributedString()
        let body = NSFont.systemFont(ofSize: 13)
        for line in markdown.components(separatedBy: "\n") {
            if line.hasPrefix("# ") { continue }
            let paragraph = NSMutableParagraphStyle()
            paragraph.paragraphSpacing = 4
            var text = line
            var font = body
            if line.hasPrefix("## ") {
                text = String(line.dropFirst(3))
                font = .boldSystemFont(ofSize: 15)
                paragraph.paragraphSpacingBefore = 12
            } else if let dash = line.range(of: "- "), line[..<dash.lowerBound].allSatisfy({ $0 == " " }) {
                let indent = CGFloat(line[..<dash.lowerBound].count) * 6
                text = "•  " + line[dash.upperBound...]
                paragraph.firstLineHeadIndent = indent
                paragraph.headIndent = indent + 14
            }
            let inline = (try? AttributedString(markdown: text, options: .init(interpretedSyntax: .inlineOnlyPreservingWhitespace)))
                .map(NSMutableAttributedString.init) ?? NSMutableAttributedString(string: text)
            let whole = NSRange(location: 0, length: inline.length)
            inline.enumerateAttribute(.inlinePresentationIntent, in: whole) { value, range, _ in
                let intent = (value as? NSNumber).map { InlinePresentationIntent(rawValue: $0.uintValue) } ?? []
                var piece = font
                if intent.contains(.stronglyEmphasized) { piece = .boldSystemFont(ofSize: font.pointSize) }
                if intent.contains(.code) { piece = .monospacedSystemFont(ofSize: font.pointSize - 1, weight: .regular) }
                inline.addAttribute(.font, value: piece, range: range)
            }
            inline.addAttributes([.paragraphStyle: paragraph, .foregroundColor: NSColor.labelColor], range: whole)
            result.append(inline)
            result.append(NSAttributedString(string: "\n"))
        }
        return result
    }
}
