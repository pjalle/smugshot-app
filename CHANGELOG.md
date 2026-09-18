# Changelog

Newest first. Written for the person using the tool, not the person reading the code.

## Unreleased


## 0.3.1 (2026-09-18)

The first version with a Windows download.

- Windows has now run on a real Windows machine (2026-09-18), and the gesture works. Two things the first tester ran into, fixed and seen working by her:
  - **Starting Smugshot now says so.** A banner at the bottom right reads "Smugshot is running. Press Ctrl + Shift + 1, then drag." Before, nothing showed but a small tray icon, and it looked as if nothing had happened.
  - **Only one Smugshot runs at a time.** Starting it again shows "Smugshot is already running" instead of adding another copy (the tester ended up with ten).

## 0.3.0 (2026-09-18)

The first version with a download: https://smugshot.io

- **Sound** in Settings: choose the sound after a smugshot from Screen Capture (the new default, drier than the old Tink), Shutter, Frog, Pop, Sent or Tink. Picking one plays it. All of them ship with macOS. Also `defaults write com.pjalle.smugshot sound -string pop` (`screen-capture`, `shutter`, `frog`, `pop`, `sent`, `tink`).
- You can now take a smugshot of Smugshot's own Settings window. Before, its own windows were left out of every picture.
- **The screen freezes while you drag**, like the Mac's own Command + Shift + 4. Hover over something so its buttons or tooltip show, press the hotkey, and they stay visible while you drag. Before, they vanished from view as the drag layer appeared (the picture itself already kept them).
- Settings: the tick-box labels are shorter so they are no longer cut off; what they need moved into the note below them.
- **Settings.** "Settings…" in the menu (or ⌘,) opens a small window. Each change applies at once:
  - **Shortcut:** click, press the new keys. It needs Command, Control or Option. The menu says so if another app has it.
  - **Save to:** the folder smugshots go in. Default ~/.smugshots. Only folders named like a smugshot are ever deleted, so a folder that holds other things is safe.
  - **Keep for:** 1 minute, 2, 5, 10, 30 minutes, 1 hour, 1 day, 1 week, or Forever. Default 1 day. Short settings work: the sweep now runs every 30 seconds, not only after each shot.
  - **Clipboard text:** the text in front of the path, default `smugshot: `. A preview shows what will be pasted.
  - **Gather:** switch off "What was there", "Text read from the close-up" or "Web element" if you do not want them, or want a smugshot to land faster. With "What was there" off, Smugshot no longer asks for the Accessibility permission.
- **Copy last smugshot again** in the menu, for one more question about the same thing.
- **Recent smugshots** in the menu: the last five, each with a thumbnail of the close-up and the app it was in. Choosing one copies its path again; holding Option shows it in Finder.
- **Quiet**, in the menu and in Settings: no sound and no icon flash, for calls and screen shares.
- The icon is now drawn from geometry (`scripts/draw-icon.py`): flat near-black tile, a brighter red, crisp edges. The README shows it instead of the old character picture. Rebuild the .icns on a Mac with `swift scripts/make-icon.swift`.
- Windows: after a smugshot, a small banner at the bottom right says "Copied. Paste it into the chat." and fades out, and a soft tink plays. Each can be switched off: `sound` and `banner` in settings.json, or the two toggles in the tray menu. (These replace `quiet`.)
- Windows: Smugshot now has its own icon in the system tray instead of the stock one: the crop corners and the smirk, white on a dark taskbar and black on a light one, filling the square like the system's own tray glyphs. (The exe file itself still shows the stock icon in Explorer.)
- The same settings exist with `defaults write` (`folder`, `keepFor`, `prefix`, `nameControls`, `readText`, `webElement`, and the hotkey as before); see the README.
- Windows: the same settings where they apply (folder, keepFor, prefix, hotkey, quiet) in `%APPDATA%\Smugshot\settings.json`, opened from "Settings…" in the tray menu; "Copy last smugshot again" and the last five smugshots in the tray menu.

## 0.2.0 (2026-09-17)

- **shot.md now says what you pointed at.** It names the control under your drag (its role, label and state), its parents up to the window, and what else is inside the dragged area, in any app. Needs the Accessibility permission; the menu tells you if it is missing.
- **The text in the dragged area is read from the picture** and added to shot.md, so even apps that publish nothing give the agent something to read.
- **In a browser, shot.md carries the page element's HTML**, a selector, `data-testid` and, on React dev sites, the component names, plus the page address. Works in Brave, Chrome, Edge, Arc, Vivaldi and Safari once "Allow JavaScript from Apple Events" is on. When it is off, shot.md says exactly what to switch on.
- Fixed: the "may Smugshot control your browser?" question vanished after three seconds, before it could be clicked. It now stays up until you answer.
- shot.md names the app and window under the drag, not the app that happened to be in front.
- **A Claude Code skill** teaches the agent to read a smugshot: `./scripts/install-claude-skill.sh`. It also lets Claude Code read `~/.smugshots` without asking.
- **Smugshot has a real app icon.**
- **A first Windows version** (Ctrl + Shift + 1, gesture only). Compiled and partly tested, never yet run on real Windows.
- A hands-free test trigger for development, off by default.
- Smugshot has its own menu bar icon: four crop corners around a smirk with one raised eyebrow. It follows light and dark menu bars.
- The hotkey now works like the Mac's own Command + Shift + 4: press Command + Shift + 1 once, let go, then drag when you're ready. Esc, a right-click, a click without dragging, or the hotkey again cancels.
- With several screens, you can start the drag on any of them.
- Reinstalling no longer sometimes leaves Smugshot stopped: the install waits for the old copy to quit before starting the new one.

## 0.1.0 (2026-09-17)

- First version: press the hotkey, drag, and the path to a small text file lands on your clipboard. Next to it are a picture of the whole screen with your part outlined and a sharp close-up.
- The clipboard text starts with `smugshot:` so it is never mistaken for a slash command.
- Smugshots live in `~/.smugshots/`, private to you, and delete themselves after a day.
- Signed with a self-signed certificate so rebuilds keep the Screen Recording permission.
