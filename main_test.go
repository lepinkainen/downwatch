package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Test expandHome function
func TestExpandHome(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
		checkFn func(string) bool
		desc    string
	}{
		{
			name:    "empty string",
			input:   "",
			wantErr: false,
			checkFn: func(s string) bool { return s == "" },
			desc:    "should return empty string",
		},
		{
			name:    "tilde only",
			input:   "~",
			wantErr: false,
			checkFn: func(s string) bool { return s != "" && !strings.Contains(s, "~") },
			desc:    "should return home directory without tilde",
		},
		{
			name:    "tilde with path",
			input:   "~/Downloads",
			wantErr: false,
			checkFn: func(s string) bool { return strings.HasSuffix(s, "Downloads") && !strings.Contains(s, "~") },
			desc:    "should expand to home/Downloads",
		},
		{
			name:    "absolute path",
			input:   "/tmp/test",
			wantErr: false,
			checkFn: func(s string) bool { return s == "/tmp/test" },
			desc:    "should return unchanged",
		},
		{
			name:    "relative path",
			input:   "relative/path",
			wantErr: false,
			checkFn: func(s string) bool { return s == "relative/path" },
			desc:    "should return unchanged",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := expandHome(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("expandHome() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && !tt.checkFn(got) {
				t.Errorf("expandHome(%q) = %q; %s", tt.input, got, tt.desc)
			}
		})
	}
}

// Test hasIgnoredExt function
func TestHasIgnoredExt(t *testing.T) {
	ignores := []string{".crdownload", ".download", ".part", ".partial"}

	tests := []struct {
		name string
		path string
		want bool
	}{
		{"crdownload file", "file.crdownload", true},
		{"download file", "test.download", true},
		{"part file", "video.part", true},
		{"partial file", "doc.partial", true},
		{"UPPERCASE ext", "FILE.CRDOWNLOAD", true},
		{"mixed case", "Test.PaRt", true},
		{"normal file", "document.pdf", false},
		{"no extension", "filename", false},
		{"different ext", "file.tmp", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := hasIgnoredExt(tt.path, ignores); got != tt.want {
				t.Errorf("hasIgnoredExt(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

// Test anyPatternMatch function
func TestAnyPatternMatch(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		patterns []string
		want     bool
	}{
		{"empty patterns", "file.txt", []string{}, false},
		{"exact match", "test.pdf", []string{"test.pdf"}, true},
		{"wildcard prefix", "grok-model.bin", []string{"grok-*"}, true},
		{"wildcard suffix", "report.xlsx", []string{"*.xlsx"}, true},
		{"wildcard both", "my-doc-final.txt", []string{"*-doc-*"}, true},
		{"no match", "image.png", []string{"*.pdf", "*.docx"}, false},
		{"multiple patterns first", "data.csv", []string{"*.csv", "*.json"}, true},
		{"multiple patterns second", "config.json", []string{"*.csv", "*.json"}, true},
		{"case sensitive", "File.PDF", []string{"*.pdf"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := anyPatternMatch(tt.filename, tt.patterns); got != tt.want {
				t.Errorf("anyPatternMatch(%q, %v) = %v, want %v", tt.filename, tt.patterns, got, tt.want)
			}
		})
	}
}

// Test extMatches function
func TestExtMatches(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		exts     []string
		want     bool
	}{
		{"empty extensions", "file.txt", []string{}, false},
		{"exact match", "doc.pdf", []string{"pdf"}, true},
		{"case insensitive", "Image.JPG", []string{"jpg"}, true},
		{"multiple exts first", "video.mp4", []string{"mp4", "avi", "mov"}, true},
		{"multiple exts last", "sound.wav", []string{"mp3", "flac", "wav"}, true},
		{"no match", "file.txt", []string{"pdf", "docx"}, false},
		{"ext with dot", "test.png", []string{"png"}, true},
		{"no extension", "filename", []string{"txt"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := extMatches(tt.filename, tt.exts); got != tt.want {
				t.Errorf("extMatches(%q, %v) = %v, want %v", tt.filename, tt.exts, got, tt.want)
			}
		})
	}
}

// Test mimePrefixMatches function
func TestMimePrefixMatches(t *testing.T) {
	tests := []struct {
		name     string
		mimeType string
		prefixes []string
		want     bool
	}{
		{"empty prefixes", "image/jpeg", []string{}, false},
		{"empty mime", "", []string{"image/"}, false},
		{"exact image match", "image/jpeg", []string{"image/"}, true},
		{"video prefix", "video/mp4", []string{"video/"}, true},
		{"multiple prefixes first", "image/png", []string{"image/", "video/"}, true},
		{"multiple prefixes second", "application/pdf", []string{"image/", "application/"}, true},
		{"no match", "text/plain", []string{"image/", "video/"}, false},
		{"specific type", "application/pdf", []string{"application/pdf"}, true},
		{"partial match", "application/json", []string{"application/"}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := mimePrefixMatches(tt.mimeType, tt.prefixes); got != tt.want {
				t.Errorf("mimePrefixMatches(%q, %v) = %v, want %v", tt.mimeType, tt.prefixes, got, tt.want)
			}
		})
	}
}

// Test chooseRule function with temporary test files
func TestChooseRule(t *testing.T) {
	// Create temp directory for test files
	tmpDir := t.TempDir()

	// Create test files
	pdfFile := filepath.Join(tmpDir, "document.pdf")
	if err := os.WriteFile(pdfFile, []byte("%PDF-1.4"), 0644); err != nil {
		t.Fatalf("failed to create test PDF: %v", err)
	}

	jpgFile := filepath.Join(tmpDir, "image.jpg")
	// JPEG magic bytes
	if err := os.WriteFile(jpgFile, []byte{0xFF, 0xD8, 0xFF, 0xE0}, 0644); err != nil {
		t.Fatalf("failed to create test JPG: %v", err)
	}

	txtFile := filepath.Join(tmpDir, "notes.txt")
	if err := os.WriteFile(txtFile, []byte("hello world"), 0644); err != nil {
		t.Fatalf("failed to create test TXT: %v", err)
	}

	rules := []Rule{
		{Name: "PDFs", Extensions: []string{"pdf"}, Dest: "/docs"},
		{Name: "Images", MIMEPrefixes: []string{"image/"}, Dest: "/images"},
		{Name: "Patterns", Patterns: []string{"grok-*"}, Dest: "/models"},
	}

	tests := []struct {
		name     string
		path     string
		wantRule string // empty if nil expected
	}{
		{"PDF by extension", pdfFile, "PDFs"},
		{"JPG by MIME", jpgFile, "Images"},
		{"no match", txtFile, ""},
		{"pattern match", filepath.Join(tmpDir, "grok-model.bin"), "Patterns"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create pattern match file if needed
			if strings.HasPrefix(filepath.Base(tt.path), "grok-") {
				if err := os.WriteFile(tt.path, []byte("data"), 0644); err != nil {
					t.Fatalf("failed to create pattern test file: %v", err)
				}
				defer os.Remove(tt.path)
			}

			got := chooseRule(tt.path, rules)
			if tt.wantRule == "" {
				if got != nil {
					t.Errorf("chooseRule(%q) = %v, want nil", tt.path, got.Name)
				}
			} else {
				if got == nil {
					t.Errorf("chooseRule(%q) = nil, want %q", tt.path, tt.wantRule)
				} else if got.Name != tt.wantRule {
					t.Errorf("chooseRule(%q) = %q, want %q", tt.path, got.Name, tt.wantRule)
				}
			}
		})
	}
}

// Test defaultConfig returns expected defaults
func TestDefaultConfig(t *testing.T) {
	cfg := defaultConfig()

	if cfg.WatchDir != "~/Downloads" {
		t.Errorf("WatchDir = %q, want ~/Downloads", cfg.WatchDir)
	}

	expectedIgnores := []string{".crdownload", ".download", ".part", ".partial"}
	if len(cfg.IgnoreExts) != len(expectedIgnores) {
		t.Errorf("IgnoreExts length = %d, want %d", len(cfg.IgnoreExts), len(expectedIgnores))
	}

	if cfg.SettleMillis != 1500 {
		t.Errorf("SettleMillis = %d, want 1500", cfg.SettleMillis)
	}

	if cfg.PollMillis != 250 {
		t.Errorf("PollMillis = %d, want 250", cfg.PollMillis)
	}

	if !cfg.CreateDestDirs {
		t.Error("CreateDestDirs = false, want true")
	}

	if !cfg.Notifications {
		t.Error("Notifications = false, want true")
	}

	if cfg.WebDAV.TimeoutSec != 30 {
		t.Errorf("WebDAV.TimeoutSec = %d, want 30", cfg.WebDAV.TimeoutSec)
	}
}

// Test detectMIME basic functionality
func TestDetectMIME(t *testing.T) {
	tmpDir := t.TempDir()

	tests := []struct {
		name       string
		filename   string
		content    []byte
		wantPrefix string
	}{
		{"PDF file", "test.pdf", []byte("%PDF-1.4"), "application/pdf"},
		{"JPEG file", "image.jpg", []byte{0xFF, 0xD8, 0xFF, 0xE0}, "image/jpeg"},
		{"PNG file", "image.png", []byte{0x89, 0x50, 0x4E, 0x47}, "image/png"},
		{"text file", "file.txt", []byte("hello world"), "text/plain"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(tmpDir, tt.filename)
			if err := os.WriteFile(path, tt.content, 0644); err != nil {
				t.Fatalf("failed to create test file: %v", err)
			}
			defer os.Remove(path)

			got := detectMIME(path)
			if !strings.HasPrefix(got, tt.wantPrefix) {
				t.Errorf("detectMIME(%q) = %q, want prefix %q", tt.filename, got, tt.wantPrefix)
			}
		})
	}
}

// Test notifyUser doesn't panic (can't easily test actual notification)
func TestNotifyUser(t *testing.T) {
	// This just ensures the function doesn't panic
	// Actual notification only works on macOS
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("notifyUser panicked: %v", r)
		}
	}()

	notifyUser("Test Title", "Test Message")

	// If we're on macOS, wait a bit for goroutine
	if runtime.GOOS == "darwin" {
		// Just verify it doesn't panic; can't reliably test notification
	}
}

// Benchmark rule matching
func BenchmarkChooseRule(b *testing.B) {
	tmpDir := b.TempDir()
	testFile := filepath.Join(tmpDir, "document.pdf")
	if err := os.WriteFile(testFile, []byte("%PDF-1.4"), 0644); err != nil {
		b.Fatalf("failed to create test file: %v", err)
	}

	rules := []Rule{
		{Name: "Rule1", Patterns: []string{"*.txt"}},
		{Name: "Rule2", Extensions: []string{"jpg", "png"}},
		{Name: "Rule3", Extensions: []string{"pdf"}},
		{Name: "Rule4", MIMEPrefixes: []string{"video/"}},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		chooseRule(testFile, rules)
	}
}

// Benchmark pattern matching
func BenchmarkAnyPatternMatch(b *testing.B) {
	patterns := []string{"grok-*", "model-*", "*.bin", "data-*"}
	filename := "grok-model-v2.bin"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		anyPatternMatch(filename, patterns)
	}
}
