package queue

import "testing"

func TestParseSize(t *testing.T) {
	tests := []struct {
		input    string
		expected int64
	}{
		{"1024", 1024},
		{"1KiB", 1024},
		{"2MiB", 2 * 1024 * 1024},
		{"1.5GB", int64(1.5 * 1000 * 1000 * 1000)},
		{"", 2048},
	}

	for _, tt := range tests {
		got, err := ParseSize(tt.input, 2048)
		if err != nil {
			t.Fatalf("ParseSize(%q) returned error: %v", tt.input, err)
		}
		if got != tt.expected {
			t.Fatalf("ParseSize(%q) = %d want %d", tt.input, got, tt.expected)
		}
	}
}
