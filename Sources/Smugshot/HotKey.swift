import Carbon.HIToolbox

/// A system-wide hotkey with key-down and key-up callbacks.
/// Uses the Carbon hotkey service, which needs no permission.
final class HotKey {
    private static var registry: [UInt32: HotKey] = [:]
    private static var nextID: UInt32 = 1
    private static var handlerInstalled = false

    private let id: UInt32
    private var ref: EventHotKeyRef?
    let keyCode: UInt32
    let modifiers: UInt32
    private let onDown: () -> Void
    private let onUp: () -> Void

    init?(keyCode: UInt32, modifiers: UInt32, onDown: @escaping () -> Void, onUp: @escaping () -> Void = {}) {
        self.id = HotKey.nextID
        HotKey.nextID += 1
        self.keyCode = keyCode
        self.modifiers = modifiers
        self.onDown = onDown
        self.onUp = onUp
        HotKey.installHandlerIfNeeded()

        let hotKeyID = EventHotKeyID(signature: OSType(0x534D5547), id: id) // 'SMUG'
        let status = RegisterEventHotKey(keyCode, modifiers, hotKeyID, GetEventDispatcherTarget(), 0, &ref)
        guard status == noErr else { return nil }
        HotKey.registry[id] = self
    }

    func unregister() {
        if let ref { UnregisterEventHotKey(ref) }
        ref = nil
        HotKey.registry[id] = nil
    }

    private static func installHandlerIfNeeded() {
        guard !handlerInstalled else { return }
        handlerInstalled = true
        let specs = [
            EventTypeSpec(eventClass: OSType(kEventClassKeyboard), eventKind: UInt32(kEventHotKeyPressed)),
            EventTypeSpec(eventClass: OSType(kEventClassKeyboard), eventKind: UInt32(kEventHotKeyReleased)),
        ]
        InstallEventHandler(GetEventDispatcherTarget(), { _, event, _ in
            guard let event else { return OSStatus(eventNotHandledErr) }
            var hotKeyID = EventHotKeyID()
            let status = GetEventParameter(event, EventParamName(kEventParamDirectObject), EventParamType(typeEventHotKeyID),
                                           nil, MemoryLayout<EventHotKeyID>.size, nil, &hotKeyID)
            guard status == noErr, let hotKey = HotKey.registry[hotKeyID.id] else { return OSStatus(eventNotHandledErr) }
            if GetEventKind(event) == UInt32(kEventHotKeyPressed) { hotKey.onDown() } else { hotKey.onUp() }
            return noErr
        }, specs.count, specs, nil, nil)
    }
}
