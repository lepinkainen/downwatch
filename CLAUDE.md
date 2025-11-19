# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

**downwatch** is a file organizer daemon that watches a directory (typically `~/Downloads`) and automatically moves or copies files to designated destinations based on configurable rules. It supports pattern matching (globs), file extensions, MIME type detection, and optional WebDAV uploads.

## Architecture

- **Single-file Go application** (`main.go:1-526`) - no internal packages
- **fsnotify-based watcher** monitors filesystem events (`main.go:504-524`)
- **Rule-based file routing** matches files against patterns, extensions, or MIME prefixes (`main.go:151-162`)
- **Stability checking** polls file size to detect when downloads complete (`main.go:164-188`)
- **Atomic operations** ensures cross-filesystem moves via copy+sync+rename (`main.go:194-234`)
- **Optional WebDAV integration** uploads files to remote DAV servers (`main.go:314-347`)

## Key Workflows

### Building

This project uses [Task](https://taskfile.dev/) for build automation:

```bash
# Build with tests and linting (recommended)
task build

# Build without tests/linting (for CI)
task build-ci

# Run tests
task test

# Run tests with coverage
task test-ci

# Run linter
task lint

# Clean build artifacts
task clean
```

Traditional Go commands also work:

```bash
# Direct Go build (outputs to build/ directory)
go build -o build/downwatch .

# Cross-platform builds
GOOS=darwin GOARCH=arm64 go build -o build/downwatch-macos-arm64 .
```

### Testing Configuration

```bash
# Run with custom config
./build/downwatch /path/to/config.yaml

# Default config location (not enforced by code)
./build/downwatch config.yaml

# Development mode (runs from source)
task dev
```

### Release Process

- **Automated via GitHub Actions** (`.github/workflows/release.yml:1-98`)
- Pushes to `main` trigger builds for 5 platforms: Linux (amd64/arm64), macOS (amd64/arm64), Windows (amd64)
- Version format: `YYYYMMDD-{short-git-hash}` (e.g., `20231115-a1b2c3d`)
- Binaries published as GitHub releases with auto-generated notes

## Configuration Patterns

### Rule Matching Priority

Rules are evaluated **in order** (`main.go:411`). First match wins. A rule matches if ANY of these conditions are true:

1. **Patterns** - filepath.Match globs against base filename (e.g., `grok-*`)
2. **Extensions** - case-insensitive extension matching (e.g., `["jpg","pdf"]`)
3. **MIME prefixes** - matches detected MIME type (e.g., `["image/","video/"]`)

### Action Types

- `move` (default) - Atomic move via rename or copy+delete (`main.go:443-448`)
- `copy` - Preserves source file (`main.go:449-454`)

### Path Expansion

- `~` expands to user home directory (`main.go:52-67`)
- Destination directories auto-created if `create_dest_dirs: true` (default)

### Stability Detection

Files aren't processed until size is stable for `settle_millis` (default 1500ms), checked every `poll_millis` (default 250ms). This prevents acting on incomplete downloads (`main.go:402-409`).

### Duplicate Handling

- Destination files get numbered suffixes if they exist: `filename (2).ext`, `filename (3).ext`, etc. (`main.go:262-277`)
- Copy action during initial scan skips files already present with matching name+size (`main.go:430-435`)

## Project Conventions

- **No external CLI frameworks** - Uses stdlib `os.Args` (`main.go:474-478`)
- **Structured logging** - Plain text via `log` package; `log_json` field exists but unused
- **Config validation** - Uses `yaml.v3` with `KnownFields(true)` to catch typos (`main.go:356`)
- **Error handling** - Logs errors but continues processing other files (non-fatal by design)
- **MIME detection** - Two-phase: extension lookup via `mime.TypeByExtension`, then 512-byte sniffing via `http.DetectContentType` (`main.go:83-101`)

## Integration Points

### WebDAV Upload

When `webdav_upload: true` in a rule:

1. File is moved/copied to local destination first
2. Then uploaded to `webdav_path` on configured DAV server (`main.go:460-470`)
3. Failures logged but don't block local file operations
4. Timeout configurable via `webdav.timeout_sec` (default 30s)

### Initial Scan Behavior

On startup, processes **all existing files** in watch directory without stability checks (`main.go:496-502`). This "catch-up" mode:

- Skips stability polling (assumes files are complete)
- For `copy` actions, skips files already present with same name+size
- Useful for recovering from daemon restarts

## Dependencies

- `github.com/fsnotify/fsnotify` v1.9.0 - Filesystem event monitoring
- `github.com/studio-b12/gowebdav` v0.11.0 - WebDAV client
- `gopkg.in/yaml.v3` - Config parsing
- Go 1.25+ required (per `go.mod:3`)
