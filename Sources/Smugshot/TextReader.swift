import Vision

/// Reads the text in the dragged area on the Mac itself. Works even in apps that publish no controls.
enum TextReader {
    static func read(_ image: CGImage) -> [String] {
        let request = VNRecognizeTextRequest()
        request.recognitionLevel = .accurate
        // Language correction "fixes" code and identifiers into dictionary words.
        request.usesLanguageCorrection = false
        do { try VNImageRequestHandler(cgImage: image, options: [:]).perform([request]) } catch { return [] }
        let found = (request.results ?? []).compactMap { obs -> (String, CGRect)? in
            guard let top = obs.topCandidates(1).first, top.confidence > 0.3 else { return nil }
            return (top.string, obs.boundingBox)
        }
        // Vision counts from the bottom-left. Words on the same line are joined, lines go top to bottom.
        var rows: [[(String, CGRect)]] = []
        for item in found.sorted(by: { $0.1.midY > $1.1.midY }) {
            if let last = rows.last?.first, abs(last.1.midY - item.1.midY) < max(last.1.height, item.1.height) * 0.6 {
                rows[rows.count - 1].append(item)
            } else {
                rows.append([item])
            }
        }
        return rows.map { $0.sorted { $0.1.minX < $1.1.minX }.map(\.0).joined(separator: "   ") }
    }
}
