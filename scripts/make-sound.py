#!/usr/bin/env python3
"""Writes windows/tink.wav, the sound the Windows version plays after a smugshot: a short, soft "tink", a
high note with a quick decay, like the Mac's Tink. Run from the repo root; standard library only."""
import math
import struct
import wave

RATE = 22050
LENGTH = 0.22          # seconds
NOTE = 1318.5          # E6
frames = bytearray()
for i in range(int(RATE * LENGTH)):
    t = i / RATE
    envelope = math.exp(-t * 22)                      # quick decay
    attack = min(1.0, t / 0.004)                      # no click at the start
    tone = math.sin(2 * math.pi * NOTE * t) + 0.35 * math.sin(2 * math.pi * NOTE * 2.0 * t) * math.exp(-t * 40)
    frames += struct.pack("<h", int(tone * envelope * attack * 0.45 * 32767))

with wave.open("windows/tink.wav", "wb") as w:
    w.setnchannels(1)
    w.setsampwidth(2)
    w.setframerate(RATE)
    w.writeframes(bytes(frames))
print("wrote windows/tink.wav")
