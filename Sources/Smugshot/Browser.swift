import AppKit

/// Asks the browser what page element sits under the drag, by running a small script in the front tab.
/// This is what gets copied by hand today with right-click, Inspect, copy element.
/// Needs "Allow JavaScript from Apple Events" switched on in the browser, and a one-time "allow Smugshot to control …" prompt.
enum Browser {
    enum Dialect { case chromium, safari }

    static let known: [String: (name: String, dialect: Dialect, setting: String)] = [
        "com.brave.Browser": ("Brave", .chromium, "View > Developer > Allow JavaScript from Apple Events"),
        "com.google.Chrome": ("Chrome", .chromium, "View > Developer > Allow JavaScript from Apple Events"),
        "com.microsoft.edgemac": ("Edge", .chromium, "Tools > Developer > Allow JavaScript from Apple Events"),
        "company.thebrowser.Browser": ("Arc", .chromium, "View > Developer > Allow JavaScript from Apple Events"),
        "com.vivaldi.Vivaldi": ("Vivaldi", .chromium, "Tools > Developer > Allow JavaScript from Apple Events"),
        "com.apple.Safari": ("Safari", .safari, "Settings > Developer > Allow JavaScript from Apple Events"),
    ]

    private static var asking = Set<String>()

    private static func permission(for bundleID: String, ask: Bool) -> OSStatus {
        let target = NSAppleEventDescriptor(bundleIdentifier: bundleID)
        return AEDeterminePermissionToAutomateTarget(target.aeDesc, typeWildCard, typeWildCard, ask)
    }

    struct Result {
        var lines: [String] = []
        var url: String?
        var note: String?
    }

    /// `region` and `webArea` are in global screen points with a top-left origin. Must run on the main thread.
    static func element(bundleID: String, region: CGRect, webArea: CGRect?, screenScale: CGFloat) -> Result? {
        guard let browser = known[bundleID] else { return nil }

        // macOS takes its "may Smugshot control this browser?" question down again the moment the request
        // times out, so asking it as part of a 3-second request leaves no time to click. Ask it separately,
        // with no time limit, and let this smugshot go without the web element.
        switch permission(for: bundleID, ask: false) {
        case noErr: break
        case OSStatus(errAEEventNotPermitted):
            return Result(note: "Smugshot is not allowed to control \(browser.name). Allow it under System Settings > Privacy & Security > Automation.")
        case OSStatus(procNotFound): return nil
        default:
            if !asking.contains(bundleID) {
                asking.insert(bundleID)
                DispatchQueue.global(qos: .userInitiated).async {
                    _ = permission(for: bundleID, ask: true) // waits for the click, however long it takes
                    DispatchQueue.main.async { asking.remove(bundleID) }
                }
            }
            return Result(note: "macOS is asking whether Smugshot may control \(browser.name). Click Allow, then take the smugshot again to get the web element.")
        }

        let area = webArea.map { "{x:\($0.minX),y:\($0.minY),w:\($0.width),h:\($0.height)}" } ?? "null"
        let call = "(\(script))({x:\(region.minX),y:\(region.minY),w:\(region.width),h:\(region.height)},\(area),\(screenScale))"
        let escaped = call.replacingOccurrences(of: "\\", with: "\\\\").replacingOccurrences(of: "\"", with: "\\\"")
            .replacingOccurrences(of: "\n", with: " ")
        let run = browser.dialect == .chromium
            ? "execute active tab of front window javascript \"\(escaped)\""
            : "do JavaScript \"\(escaped)\" in current tab of front window"
        let source = "with timeout of 3 seconds\ntell application id \"\(bundleID)\" to \(run)\nend timeout"

        var error: NSDictionary?
        let output = NSAppleScript(source: source)?.executeAndReturnError(&error)
        guard let json = output?.stringValue, let data = json.data(using: .utf8),
              let object = try? JSONSerialization.jsonObject(with: data) as? [String: Any] else {
            let number = error?[NSAppleScript.errorNumber] as? Int
            let message = error?[NSAppleScript.errorMessage] as? String ?? "no answer"
            if number == -1743 {
                return Result(note: "Smugshot is not allowed to control \(browser.name). Allow it under System Settings > Privacy & Security > Automation.")
            }
            if number == -1712 {
                return Result(note: "\(browser.name) did not answer in time. If macOS just asked whether Smugshot may control \(browser.name), say yes and take the smugshot again.")
            }
            return Result(note: "\(browser.name) did not run the page script (\(message)). Switch on \(browser.setting) in \(browser.name).")
        }
        return format(object)
    }

    private static func format(_ o: [String: Any]) -> Result {
        var r = Result(url: o["url"] as? String)
        if let problem = o["error"] as? String { r.note = problem; return r }
        if let html = o["html"] as? String { r.lines.append(html) }
        if let v = o["selector"] as? String { r.lines.append("selector: \(v)") }
        if let v = o["testid"] as? String, !v.isEmpty { r.lines.append("data-testid: \(v)") }
        if let v = o["aria"] as? String, !v.isEmpty { r.lines.append("aria: \(v)") }
        if let v = o["components"] as? String, !v.isEmpty { r.lines.append("React components (innermost first): \(v)") }
        if let v = o["source"] as? String, !v.isEmpty { r.lines.append("source: \(v)") }
        if let v = o["text"] as? String, !v.isEmpty { r.lines.append("text: \(v)") }
        if let v = o["box"] as? String { r.lines.append("box on page (CSS px): \(v)") }
        if let v = o["how"] as? String { r.lines.append("found by: \(v)") }
        if let v = o["frame"] as? String, !v.isEmpty { r.note = v }
        return r
    }

    /// Runs in the page. Single quotes only and no backslashes, so it survives being wrapped in AppleScript.
    private static let script = #"""
    function(region, area, screenScale) {
      try {
        var zoom = window.devicePixelRatio / screenScale;
        if (!(zoom > 0.2 && zoom < 6)) zoom = 1;
        var ox, oy, how;
        if (area && Math.abs(area.w - window.innerWidth * zoom) < area.w * 0.03) {
          ox = area.x; oy = area.y; how = 'page position from the accessibility layer';
        } else {
          ox = window.screenX + (window.outerWidth - window.innerWidth * zoom) / 2;
          oy = window.screenY + (window.outerHeight - window.innerHeight * zoom);
          how = 'page position estimated from the window size (less exact)';
        }
        var r = { x: (region.x - ox) / zoom, y: (region.y - oy) / zoom, w: region.w / zoom, h: region.h / zoom };
        var cx = r.x + r.w / 2, cy = r.y + r.h / 2;
        if (cx < 0 || cy < 0 || cx > window.innerWidth || cy > window.innerHeight) {
          return JSON.stringify({ url: location.href, error: 'The drag was outside the page area of the front tab.' });
        }
        function fit(el) {
          var b = el.getBoundingClientRect();
          var w = Math.min(b.right, r.x + r.w) - Math.max(b.left, r.x);
          var h = Math.min(b.bottom, r.y + r.h) - Math.max(b.top, r.y);
          if (w <= 0 || h <= 0) return 0;
          return (w * h) / (b.width * b.height + r.w * r.h - w * h);
        }
        function stack(root) {
          var list = root.elementsFromPoint(cx, cy), out = [];
          for (var i = 0; i < list.length; i++) {
            out.push(list[i]);
            if (list[i].shadowRoot && i === 0) { out = stack(list[i].shadowRoot).concat(out); }
          }
          return out;
        }
        var candidates = stack(document), best = null, score = -1;
        for (var i = 0; i < candidates.length; i++) {
          var s = fit(candidates[i]);
          if (s > score) { score = s; best = candidates[i]; }
        }
        if (!best) return JSON.stringify({ url: location.href, error: 'No page element found under the drag.' });

        function selector(el) {
          var parts = [];
          while (el && el.nodeType === 1 && parts.length < 6) {
            if (el.id) { parts.unshift('#' + CSS.escape(el.id)); break; }
            var part = el.localName, tid = el.getAttribute('data-testid');
            if (tid) { parts.unshift(part + '[data-testid=' + JSON.stringify(tid) + ']'); break; }
            var parent = el.parentElement;
            if (parent) {
              var same = Array.prototype.filter.call(parent.children, function (c) { return c.localName === el.localName; });
              if (same.length > 1) part += ':nth-of-type(' + (same.indexOf(el) + 1) + ')';
            }
            parts.unshift(part);
            el = parent;
          }
          return parts.join(' > ');
        }
        function react(el) {
          var names = [], source = '';
          for (var node = el; node && !names.length; node = node.parentElement) {
            var key = Object.keys(node).filter(function (k) { return k.indexOf('__reactFiber$') === 0 || k.indexOf('__reactInternalInstance$') === 0; })[0];
            if (!key) continue;
            for (var f = node[key]; f && names.length < 6; f = f.return) {
              var t = f.type, n = t && (typeof t === 'function' || typeof t === 'object') ? (t.displayName || t.name || (t.render && t.render.name)) : '';
              if (n && names.indexOf(n) < 0) names.push(n);
              if (!source && f._debugSource) source = f._debugSource.fileName + ':' + f._debugSource.lineNumber;
            }
          }
          return { names: names.join(' < '), source: source };
        }
        var html = best.outerHTML || '';
        if (html.length > 1500) {
          var open = html.slice(0, html.indexOf('>') + 1);
          html = open.length < 1200 ? open + ' … (' + best.children.length + ' children, ' + html.length + ' characters) … </' + best.localName + '>' : html.slice(0, 1200) + ' …';
        }
        var b = best.getBoundingClientRect(), rc = react(best);
        var aria = [best.getAttribute('role'), best.getAttribute('aria-label')].filter(Boolean).join(' / ');
        var text = (best.innerText || best.value || '').trim().split(/\s+/).join(' ');
        return JSON.stringify({
          url: location.href, html: html, selector: selector(best),
          testid: best.getAttribute('data-testid') || '', aria: aria,
          components: rc.names, source: rc.source,
          text: text.length > 300 ? text.slice(0, 300) + ' …' : text,
          box: Math.round(b.left) + ',' + Math.round(b.top) + ' ' + Math.round(b.width) + 'x' + Math.round(b.height),
          how: how + ', match ' + Math.round(score * 100) + '%',
          frame: best.localName === 'iframe' ? 'The drag is over an embedded frame; what is inside it cannot be read from here.' : ''
        });
      } catch (e) { return JSON.stringify({ url: location.href, error: 'Page script failed: ' + e }); }
    }
    """#
}
