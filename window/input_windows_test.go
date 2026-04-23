//go:build windows

package window

import (
	"bytes"
	"image/png"
	"testing"
	"unsafe"
)

func TestRawInputLayoutMatchesSendInputSize(t *testing.T) {
	// SendInput requires cbSize == 40 on x64 (28 on x86). We assume x64.
	size := unsafe.Sizeof(rawInput{})
	if size != 40 {
		t.Fatalf("rawInput size = %d, want 40 for x64 SendInput", size)
	}
}

func TestResolveVK(t *testing.T) {
	cases := []struct {
		in   string
		want uint16
		ok   bool
	}{
		{"a", 0x41, true},
		{"Z", 0x5A, true},
		{"0", 0x30, true},
		{"9", 0x39, true},
		{"enter", 0x0D, true},
		{"ENTER", 0x0D, true},
		{"return", 0x0D, true},
		{"esc", 0x1B, true},
		{"escape", 0x1B, true},
		{"f1", 0x70, true},
		{"f12", 0x7B, true},
		{"control", 0x11, true},
		{"ctrl", 0x11, true},
		{"alt", 0x12, true},
		{"shift", 0x10, true},
		{"win", 0x5B, true},
		{"up", 0x26, true},
		{"down", 0x28, true},
		{"", 0, false},
		{"not-a-key", 0, false},
		{"ff", 0, false}, // two chars, no table entry
	}
	for _, c := range cases {
		got, ok := resolveVK(c.in)
		if ok != c.ok || got != c.want {
			t.Errorf("resolveVK(%q) = (0x%X, %v), want (0x%X, %v)", c.in, got, ok, c.want, c.ok)
		}
	}
}

func TestClipboardRoundTrip(t *testing.T) {
	// Preserve whatever's on the clipboard so we don't nuke the user's state.
	original, _ := clipboardGetText()
	defer clipboardSetText(original)

	cases := []string{
		"hello world",
		"",
		"multi\nline\r\ntext",
		"unicode: ğüşçöı 日本語 — em dash",
		"tab\there",
	}
	for _, want := range cases {
		if err := clipboardSetText(want); err != nil {
			t.Fatalf("set %q: %v", want, err)
		}
		got, err := clipboardGetText()
		if err != nil {
			t.Fatalf("get after %q: %v", want, err)
		}
		if got != want {
			t.Errorf("round-trip: got %q, want %q", got, want)
		}
	}
}

func TestCaptureRegionPNGSmoke(t *testing.T) {
	// Grab a small 10x10 region at 0,0 — every display has at least that.
	data, err := captureRegionPNG(0, 0, 10, 10)
	if err != nil {
		t.Fatalf("capture failed: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("capture returned empty bytes")
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("output is not valid PNG: %v", err)
	}
	if img.Bounds().Dx() != 10 || img.Bounds().Dy() != 10 {
		t.Errorf("decoded PNG is %v, want 10x10", img.Bounds())
	}
}

func TestCaptureRegionPNGRejectsBadDims(t *testing.T) {
	if _, err := captureRegionPNG(0, 0, 0, 10); err == nil {
		t.Error("want error for zero width")
	}
	if _, err := captureRegionPNG(0, 0, 10, -1); err == nil {
		t.Error("want error for negative height")
	}
}

func TestListWindowsNonEmpty(t *testing.T) {
	ws, err := ListWindows()
	if err != nil {
		t.Fatalf("ListWindows: %v", err)
	}
	if len(ws) == 0 {
		t.Skip("no windows visible — skipping (expected in headless CI)")
	}
	for _, w := range ws {
		if w.Handle == "" || w.Title == "" {
			t.Errorf("window has empty handle/title: %+v", w)
		}
	}
}
