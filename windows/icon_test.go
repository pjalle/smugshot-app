package main

import "testing"

func TestIconPixels(t *testing.T) {
	size := 64
	px := iconPixels(size, false)
	if len(px) != size*size*4 {
		t.Fatalf("got %d bytes, want %d", len(px), size*size*4)
	}
	at := func(x, y int) []byte { i := (y*size + x) * 4; return px[i : i+4] }
	if p := at(10, 5); p[3] < 200 || p[0] < 200 { // on the top-left corner's top arm: opaque white
		t.Errorf("top arm should be white and opaque, got BGRA %v", p)
	}
	if p := at(size/2, size/2); p[3] != 0 { // between the eyes: nothing
		t.Errorf("centre should be transparent, got BGRA %v", p)
	}
	if p := at(size/2, 2); p[3] != 0 { // the gap in the top side
		t.Errorf("top gap should be transparent, got BGRA %v", p)
	}
	if p := iconPixels(size, true); p[(5*size+10)*4] != 0 || p[(5*size+10)*4+3] < 200 {
		t.Errorf("dark glyph should be black and opaque on the arm, got BGRA %v", p[(5*size+10)*4:(5*size+10)*4+4])
	}
}
