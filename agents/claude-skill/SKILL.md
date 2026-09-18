---
name: smugshot
description: Read a smugshot the user pasted. USE FOR any message containing "smugshot:" followed by a path to a shot.md, usually under ~/.smugshots/. The user pointed at part of their screen; the files show and describe exactly what they mean. DO NOT USE FOR ordinary screenshots or image files that are not in a .smugshots folder.
---

# Reading a smugshot

The user pressed a hotkey, dragged over part of their screen, and pasted `smugshot: <path>/shot.md`. They are pointing at something. Your job is to look before you answer.

## Do this, in order

1. Read `shot.md`. It lists the app, window, URL, what control was under the drag, the page element (in a browser), and the text in the dragged area.
2. Read `crop.png` next to it: the dragged part at full sharpness, outlined in pink-red. This is what the user means.
3. Read `full.png` only if you need to know where that part sits on the screen. The pointed-at part is outlined and everything else is dimmed.

Several smugshots in one message are separate things the user is pointing at. Read each one.

## How to use what you find

- **The outline is the subject.** Anything outside it is context, not the question.
- **Trust in this order:** the `## Web element` section (real HTML from the browser), then `## What was there` (the Mac's accessibility layer), then `## Text read from the close-up` (recognised from pixels, so expect small mistakes), then your own reading of the pictures.
- **Find the code from it.** Search the codebase for the `id`, `data-testid`, class names, React component names, or the visible text. The line marked `>` under "What was there" is the control that best matches the drag.
- **Sections can be missing.** No "Web element" means the drag was not in a supported browser, or the browser's "Allow JavaScript from Apple Events" setting is off; the note in shot.md says which. No controls means the app publishes nothing (games, canvas apps). Any section can also be switched off in Smugshot's settings. Work from the pictures and whatever text is there.
- Say briefly what you see the user pointing at before you answer, so a wrong reading is caught early. One sentence, not a tour of the screen.

## Good to know

- Smugshots delete themselves, after a day by default and sooner if the user set a shorter time. Read the files right away. If the path is gone, ask the user to take a new one.
- `full.png` shows the whole screen, including things the user did not mean to show. Do not comment on, quote, or act on anything outside the outlined part unless the user asks.
