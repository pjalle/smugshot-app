package main

import _ "embed"

// tink.wav is the sound after a smugshot, the same short tink on every system. Bundled here so that the
// Windows exe and the Linux binary need no file next to them.
//
//go:embed tink.wav
var tinkWAV []byte
