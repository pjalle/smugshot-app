#!/bin/bash
# Builds Smugshot.app from the command line, signs it with the self-signed
# certificate from make-cert.sh, and (with --install) puts it in /Applications.
set -euo pipefail
cd "$(dirname "$0")/.."

NAME="Smugshot"
# A release build sets these; on their own they give the everyday build for this Mac.
IDENTITY="${SMUGSHOT_IDENTITY:-Smugshot Self-Signed}"
VERSION="${SMUGSHOT_VERSION:-0.2.0}"
APP="build/$NAME.app"

if [[ "${SMUGSHOT_UNIVERSAL:-}" == "1" ]]; then
  # Intel and Apple silicon in one file. Needs the full Xcode, not only the command line tools.
  swift build -c release --arch arm64 --arch x86_64
  BINARY=".build/apple/Products/Release/$NAME"
else
  swift build -c release
  BINARY=".build/release/$NAME"
fi

rm -rf "$APP"
mkdir -p "$APP/Contents/MacOS" "$APP/Contents/Resources"
cp "$BINARY" "$APP/Contents/MacOS/$NAME"
cp Resources/AppIcon.icns "$APP/Contents/Resources/AppIcon.icns"

cat > "$APP/Contents/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>CFBundleIdentifier</key><string>com.pjalle.smugshot</string>
  <key>CFBundleName</key><string>$NAME</string>
  <key>CFBundleExecutable</key><string>$NAME</string>
  <key>CFBundleIconFile</key><string>AppIcon</string>
  <key>CFBundlePackageType</key><string>APPL</string>
  <key>CFBundleShortVersionString</key><string>$VERSION</string>
  <key>CFBundleVersion</key><string>2</string>
  <key>LSMinimumSystemVersion</key><string>15.0</string>
  <key>LSUIElement</key><true/>
  <key>NSAppleEventsUsageDescription</key><string>Smugshot asks your browser which page element you dragged over, so the agent gets its HTML.</string>
  <key>NSHighResolutionCapable</key><true/>
</dict>
</plist>
PLIST

if [[ "$IDENTITY" == "Developer ID Application"* ]]; then
  # What Apple's notary service asks for: the hardened runtime and a trusted timestamp.
  codesign --force --options runtime --timestamp --entitlements Resources/Smugshot.entitlements \
    --sign "$IDENTITY" --identifier com.pjalle.smugshot "$APP"
else
  codesign --force --sign "$IDENTITY" --identifier com.pjalle.smugshot "$APP"
fi
codesign --verify --verbose=2 "$APP"

if [[ "${1:-}" == "--install" ]]; then
  pkill -x "$NAME" 2>/dev/null || true
  # Starting the new copy while the old one is still quitting fails (error -600).
  for _ in $(seq 1 50); do pgrep -x "$NAME" >/dev/null || break; sleep 0.1; done
  rm -rf "/Applications/$NAME.app"
  cp -R "$APP" "/Applications/$NAME.app"
  for _ in 1 2 3; do open "/Applications/$NAME.app" && break; sleep 1; done
  pgrep -x "$NAME" >/dev/null || { sleep 1; pgrep -x "$NAME" >/dev/null; } || { echo "Smugshot did not start" >&2; exit 1; }
  echo "Installed and started /Applications/$NAME.app"
fi
