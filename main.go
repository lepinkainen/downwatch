package main

import (
	"bytes"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/studio-b12/gowebdav"
	"gopkg.in/yaml.v3"
)

// Track files currently being processed to avoid duplicate handling
var processing sync.Map

type Rule struct {
	Name           string   `yaml:"name"`
	Patterns       []string `yaml:"patterns"`        // filepath.Match globs, matched against base filename
	Extensions     []string `yaml:"extensions"`      // like ["pdf","zip","jpg"], case-insensitive, no leading dot
	MIMEPrefixes   []string `yaml:"mime_prefixes"`   // e.g. ["image/","video/","application/pdf"]
	Action         string   `yaml:"action"`          // "move" (default) or "copy"
	Dest           string   `yaml:"dest"`            // destination directory (supports ~ expansion); for iCloud Drive, see notes below
	SkipDuplicates bool     `yaml:"skip_duplicates"` // if true, delete source (move) or skip (copy) when duplicate exists
	WebDAVUpload   bool     `yaml:"webdav_upload"`   // if true, also upload to DAV
	WebDAVPath     string   `yaml:"webdav_path"`     // remote path prefix (e.g. "/inbox/") for DAV upload
}

type WebDAVConfig struct {
	URL           string `yaml:"url"` // e.g. "https://copyparty.example.com/dav"
	Username      string `yaml:"username"`
	Password      string `yaml:"password"`
	SkipTLSVerify bool   `yaml:"skip_tls_verify"`
	TimeoutSec    int    `yaml:"timeout_sec"` // default 30
}

type Config struct {
	WatchDir       string       `yaml:"watch_dir"` // default: ~/Downloads
	Rules          []Rule       `yaml:"rules"`
	IgnoreExts     []string     `yaml:"ignore_exts"`   // default: [".crdownload",".download",".part",".partial"]
	SettleMillis   int          `yaml:"settle_millis"` // stability window before acting; default 1500
	PollMillis     int          `yaml:"poll_millis"`   // interval for size checks; default 250
	WebDAV         WebDAVConfig `yaml:"webdav"`
	LogJSON        bool         `yaml:"log_json"`         // future hook; currently plain log
	CreateDestDirs bool         `yaml:"create_dest_dirs"` // default true
	Notifications  bool         `yaml:"notifications"`    // show macOS notifications; default true
}

func expandHome(p string) (string, error) {
	if p == "" {
		return p, nil
	}
	if strings.HasPrefix(p, "~") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		if p == "~" {
			return home, nil
		}
		return filepath.Join(home, strings.TrimPrefix(p, "~/")), nil
	}
	return p, nil
}

func defaultConfig() Config {
	return Config{
		WatchDir:       "~/Downloads",
		IgnoreExts:     []string{".crdownload", ".download", ".part", ".partial"},
		SettleMillis:   1500,
		PollMillis:     250,
		CreateDestDirs: true,
		Notifications:  true,
		WebDAV: WebDAVConfig{
			TimeoutSec: 30,
		},
	}
}

// Basic MIME sniff (fallback to extension)
func detectMIME(path string) string {
	// Try extension first via mime.TypeByExtension
	ext := strings.ToLower(filepath.Ext(path))
	if ext != "" {
		if mt := mime.TypeByExtension(ext); mt != "" {
			return mt
		}
	}
	// Sniff first bytes if file is small-ish
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer func() { _ = f.Close() }()

	buf := make([]byte, 512)
	n, _ := f.Read(buf)
	return http.DetectContentType(buf[:n])
}

func hasIgnoredExt(path string, ignores []string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	for _, ig := range ignores {
		if strings.EqualFold(ext, ig) {
			return true
		}
	}
	return false
}

// notifyUser sends a macOS native notification using osascript.
// Only works on macOS; silently fails on other platforms.
// Runs asynchronously to avoid blocking file processing.
func notifyUser(title, message string) {
	if runtime.GOOS != "darwin" {
		return
	}

	// Escape quotes in strings for AppleScript
	title = strings.ReplaceAll(title, `"`, `\"`)
	message = strings.ReplaceAll(message, `"`, `\"`)

	script := fmt.Sprintf(`display notification %q with title %q`, message, title)
	cmd := exec.Command("osascript", "-e", script)

	// Run async in goroutine to avoid blocking
	go func() {
		if err := cmd.Run(); err != nil {
			log.Printf("notification failed: %v", err)
		}
	}()
}

func anyPatternMatch(name string, patterns []string) bool {
	if len(patterns) == 0 {
		return false
	}
	for _, g := range patterns {
		ok, _ := filepath.Match(g, name)
		if ok {
			return true
		}
	}
	return false
}

func extMatches(name string, exts []string) bool {
	if len(exts) == 0 {
		return false
	}
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(name)), ".")
	for _, e := range exts {
		if strings.EqualFold(e, ext) {
			return true
		}
	}
	return false
}

func mimePrefixMatches(mt string, prefixes []string) bool {
	if len(prefixes) == 0 || mt == "" {
		return false
	}
	for _, p := range prefixes {
		if strings.HasPrefix(mt, p) {
			return true
		}
	}
	return false
}

func chooseRule(path string, rules []Rule) *Rule {
	base := filepath.Base(path)
	mt := detectMIME(path)

	for i := range rules {
		r := &rules[i]
		if anyPatternMatch(base, r.Patterns) || extMatches(base, r.Extensions) || mimePrefixMatches(mt, r.MIMEPrefixes) {
			return r
		}
	}
	return nil
}

func waitUntilStable(path string, settle time.Duration, poll time.Duration) error {
	// Consider stable when size is unchanged across the settle window.
	deadline := time.Now().Add(5 * time.Minute) // safety
	var lastSize int64 = -1
	var stableFor time.Duration

	for time.Now().Before(deadline) {
		fi, err := os.Stat(path)
		if err != nil {
			return err
		}
		size := fi.Size()
		if size == lastSize {
			stableFor += poll
			if stableFor >= settle {
				return nil
			}
		} else {
			lastSize = size
			stableFor = 0
		}
		time.Sleep(poll)
	}
	return errors.New("file did not stabilize within 5 minutes")
}

func ensureDir(dir string) error {
	return os.MkdirAll(dir, 0o755)
}

func atomicMove(src, dst string) error {
	// Try rename first (same filesystem)
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	// Cross-filesystem: copy then remove
	sf, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = sf.Close() }()

	if errDir := ensureDir(filepath.Dir(dst)); errDir != nil {
		return errDir
	}

	df, err := os.Create(dst + ".tmp")
	if err != nil {
		return err
	}

	if _, err := io.Copy(df, sf); err != nil {
		_ = df.Close()
		_ = os.Remove(df.Name())
		return err
	}
	if err := df.Sync(); err != nil {
		_ = df.Close()
		_ = os.Remove(df.Name())
		return err
	}
	if err := df.Close(); err != nil {
		_ = os.Remove(df.Name())
		return err
	}
	if err := os.Rename(df.Name(), dst); err != nil {
		_ = os.Remove(df.Name())
		return err
	}
	return os.Remove(src)
}

func copyTo(src, dst string) error {
	sf, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = sf.Close() }()

	if errDir := ensureDir(filepath.Dir(dst)); errDir != nil {
		return errDir
	}

	df, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(df, sf); err != nil {
		_ = df.Close()
		return err
	}
	if err := df.Sync(); err != nil {
		_ = df.Close()
		return err
	}
	return df.Close()
}

func uniquePath(dst string) string {
	if _, err := os.Stat(dst); err != nil {
		return dst
	}
	dir := filepath.Dir(dst)
	base := filepath.Base(dst)
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	for i := 2; i < 10_000; i++ {
		candidate := filepath.Join(dir, fmt.Sprintf("%s (%d)%s", stem, i, ext))
		if _, err := os.Stat(candidate); err != nil {
			return candidate
		}
	}
	return dst + ".dup"
}

// fileExistsWithSameSize checks if a file with the same name and size already exists in destDir.
// Returns true if found (skip copying), false otherwise.
func fileExistsWithSameSize(srcPath, destDir string) bool {
	srcStat, err := os.Stat(srcPath)
	if err != nil {
		return false
	}
	srcSize := srcStat.Size()
	baseName := filepath.Base(srcPath)

	// Check exact name match
	candidate := filepath.Join(destDir, baseName)
	if dstStat, err := os.Stat(candidate); err == nil {
		if dstStat.Size() == srcSize && srcSize > 0 {
			return true
		}
	}

	// Check numbered variants: filename (2).ext, filename (3).ext, etc.
	ext := filepath.Ext(baseName)
	stem := strings.TrimSuffix(baseName, ext)
	for i := 2; i < 10_000; i++ {
		candidate := filepath.Join(destDir, fmt.Sprintf("%s (%d)%s", stem, i, ext))
		dstStat, err := os.Stat(candidate)
		if err != nil {
			break // No more numbered variants exist
		}
		if dstStat.Size() == srcSize && srcSize > 0 {
			return true
		}
	}

	return false
}

func davClient(cfg WebDAVConfig) *gowebdav.Client {
	c := gowebdav.NewClient(cfg.URL, cfg.Username, cfg.Password)
	if cfg.SkipTLSVerify {
		tr := &http.Transport{
			// #nosec G402 - InsecureSkipVerify is intentional when user configures skip_tls_verify
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		}
		c.SetTransport(tr)
	}
	return c
}

func davUpload(c *gowebdav.Client, localPath, remotePrefix string, timeout time.Duration) error {
	data, err := os.ReadFile(localPath)
	if err != nil {
		return err
	}
	rp := filepath.ToSlash(filepath.Join(remotePrefix, filepath.Base(localPath)))
	// Make sure remote dirs exist
	dir := filepath.Dir(rp)
	if dir != "." && dir != "/" {
		_ = c.MkdirAll(dir, 0o755)
	}
	// Put with timeout
	done := make(chan error, 1)
	go func() {
		done <- c.Write(rp, data, 0o644)
	}()
	select {
	case err := <-done:
		return err
	case <-time.After(timeout):
		return errors.New("webdav upload timed out")
	}
}

func loadConfig(path string) (Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	cfg := defaultConfig()
	dec := yaml.NewDecoder(bytes.NewReader(b))
	dec.KnownFields(true)
	if errDec := dec.Decode(&cfg); errDec != nil {
		return Config{}, errDec
	}
	// Expand paths
	wd, err := expandHome(cfg.WatchDir)
	if err != nil {
		return Config{}, err
	}
	cfg.WatchDir = wd
	for i := range cfg.Rules {
		d, err := expandHome(cfg.Rules[i].Dest)
		if err != nil {
			return Config{}, err
		}
		cfg.Rules[i].Dest = d
	}
	// Sanitize rule actions
	for i := range cfg.Rules {
		a := strings.ToLower(strings.TrimSpace(cfg.Rules[i].Action))
		if a == "" {
			a = "move"
		}
		if a != "move" && a != "copy" {
			return Config{}, fmt.Errorf("rule %q has invalid action %q", cfg.Rules[i].Name, cfg.Rules[i].Action)
		}
		cfg.Rules[i].Action = a
	}
	// Normalize ignore exts
	if len(cfg.IgnoreExts) == 0 {
		cfg.IgnoreExts = defaultConfig().IgnoreExts
	}
	return cfg, nil
}

func handleFile(path string, cfg Config, dav *gowebdav.Client, skipStabilityCheck bool) {
	// Check if this file is already being processed
	if _, exists := processing.LoadOrStore(path, time.Now()); exists {
		return // Already being handled by another goroutine
	}
	defer processing.Delete(path)

	// Ignore directories and hidden temp files
	st, err := os.Stat(path)
	if err != nil || st.IsDir() {
		return
	}
	if hasIgnoredExt(path, cfg.IgnoreExts) {
		log.Printf("skip (ignored ext): %s", filepath.Base(path))
		return
	}
	// wait for stability (skip for existing files during initial scan)
	if !skipStabilityCheck {
		settle := time.Duration(cfg.SettleMillis) * time.Millisecond
		poll := time.Duration(cfg.PollMillis) * time.Millisecond
		if err := waitUntilStable(path, settle, poll); err != nil {
			log.Printf("skip (not stable): %s (%v)", filepath.Base(path), err)
			return
		}
	}

	r := chooseRule(path, cfg.Rules)
	if r == nil {
		log.Printf("no rule matched: %s", filepath.Base(path))
		return
	}

	destDir := r.Dest
	if destDir == "" {
		log.Printf("rule %q has empty dest; skipping %s", r.Name, filepath.Base(path))
		return
	}
	if cfg.CreateDestDirs {
		if err := ensureDir(destDir); err != nil {
			log.Printf("dest mkdir failed: %v", err)
			return
		}
	}

	// Check for duplicates if skip_duplicates is enabled
	if r.SkipDuplicates {
		if fileExistsWithSameSize(path, destDir) {
			if r.Action == "move" {
				// Delete source file when duplicate exists
				if err := os.Remove(path); err != nil {
					log.Printf("failed to delete duplicate source: %v", err)
					return
				}
				log.Printf("deleted (duplicate): %s (rule: %s)", filepath.Base(path), r.Name)
			} else {
				// Skip for copy action
				log.Printf("skip (already exists): %s (rule: %s)", filepath.Base(path), r.Name)
			}
			return
		}
	}

	dst := filepath.Join(destDir, filepath.Base(path))
	if _, err := os.Stat(dst); err == nil {
		dst = uniquePath(dst)
	}

	switch r.Action {
	case "move":
		if err := atomicMove(path, dst); err != nil {
			log.Printf("move failed: %v", err)
			return
		}
		log.Printf("moved: %s -> %s (rule: %s)", filepath.Base(path), destDir, r.Name)
		if cfg.Notifications {
			notifyUser("downwatch", fmt.Sprintf("Moved %s to %s", filepath.Base(path), destDir))
		}
	case "copy":
		if err := copyTo(path, dst); err != nil {
			log.Printf("copy failed: %v", err)
			return
		}
		log.Printf("copied: %s -> %s (rule: %s)", filepath.Base(path), destDir, r.Name)
		if cfg.Notifications {
			notifyUser("downwatch", fmt.Sprintf("Copied %s to %s", filepath.Base(path), destDir))
		}
	default:
		// unreachable due to validation
	}

	// Optional DAV upload
	if r.WebDAVUpload && dav != nil {
		timeout := time.Duration(cfg.WebDAV.TimeoutSec) * time.Second
		target := dst
		// If action == copy, upload the original path to avoid double-read? Either is fine.
		// Use dst so we upload exactly what we filed.
		if err := davUpload(dav, target, r.WebDAVPath, timeout); err != nil {
			log.Printf("webdav upload failed: %v", err)
		} else {
			log.Printf("webdav uploaded: %s -> %s", filepath.Base(target), r.WebDAVPath)
		}
	}
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "usage: %s /path/to/config.yaml\n", filepath.Base(os.Args[0]))
		os.Exit(2)
	}
	cfgPath := os.Args[1]
	cfg, err := loadConfig(cfgPath)
	if err != nil {
		log.Fatalf("config error: %v", err)
	}

	watch := cfg.WatchDir
	if fi, errStat := os.Stat(watch); errStat != nil || !fi.IsDir() {
		log.Fatalf("watch_dir is not a directory: %s", watch)
	}

	log.Printf("watching: %s", watch)

	var dav *gowebdav.Client
	if cfg.WebDAV.URL != "" {
		dav = davClient(cfg.WebDAV)
	}

	// Eagerly process existing files (optional; common quality-of-life)
	entries, _ := os.ReadDir(watch)
	for _, e := range entries {
		if !e.IsDir() {
			handleFile(filepath.Join(watch, e.Name()), cfg, dav, true)
		}
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Fatal(err)
	}
	defer func() { _ = watcher.Close() }()

	if err := watcher.Add(watch); err != nil {
		log.Fatal(err)
	}

	for {
		select {
		case ev := <-watcher.Events:
			// We act on Create & Rename; Write can be noisy during downloads
			if ev.Op&(fsnotify.Create|fsnotify.Rename) != 0 {
				go handleFile(ev.Name, cfg, dav, false)
			}
		case err := <-watcher.Errors:
			log.Printf("watch error: %v", err)
		}
	}
}
