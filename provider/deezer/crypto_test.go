package deezer

import (
	"testing"
)

func TestDeriveBlowfishKey(t *testing.T) {
	key := DeriveBlowfishKey("123456")
	if len(key) != 16 {
		t.Fatalf("expected key length 16, got %d", len(key))
	}

	// Verify deterministic: same input produces same key
	key2 := DeriveBlowfishKey("123456")
	for i := range key {
		if key[i] != key2[i] {
			t.Fatalf("key derivation is not deterministic at byte %d", i)
		}
	}

	// Different track ID produces different key
	key3 := DeriveBlowfishKey("654321")
	same := true
	for i := range key {
		if key[i] != key3[i] {
			same = false
			break
		}
	}
	if same {
		t.Fatal("different track IDs produced same key")
	}
}

func TestQualityToCode(t *testing.T) {
	tests := []struct {
		quality string
		code    string
	}{
		{"FLAC", "9"},
		{"MP3_320", "3"},
		{"MP3_128", "1"},
		{"", "1"},
		{"UNKNOWN", "1"},
	}

	for _, tt := range tests {
		got := qualityToCode(tt.quality)
		if got != tt.code {
			t.Errorf("qualityToCode(%q) = %q, want %q", tt.quality, got, tt.code)
		}
	}
}
