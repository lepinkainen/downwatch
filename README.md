# downwatch

A file organizer daemon that watches a directory (typically `~/Downloads`) and automatically moves or copies files to designated destinations based on configurable rules.

## Features

- **Automatic file organization** - Watch directories and route files based on patterns, extensions, or MIME types
- **Flexible rule matching** - Match files using glob patterns, file extensions, or MIME type prefixes
- **Stability detection** - Waits for downloads to complete before processing files
- **Atomic operations** - Ensures safe cross-filesystem moves via copy+sync+rename
- **WebDAV integration** - Optional upload to WebDAV servers (like copyparty, Nextcloud)
- **Duplicate handling** - Automatic file renaming when destination files exist
- **macOS notifications** - Native notifications for file operations (macOS only)
- **Cross-platform** - Supports Linux, macOS, and Windows

## Installation

### From Releases

Download the latest binary for your platform from the [Releases](https://github.com/lepinkainen/downwatch/releases) page:

- **Linux**: `downwatch-linux-amd64` or `downwatch-linux-arm64`
- **macOS**: `downwatch-macos-amd64` or `downwatch-macos-arm64`
- **Windows**: `downwatch-windows-amd64.exe`

Make the binary executable (Linux/macOS):

```bash
chmod +x downwatch-*
```

### From Source

```bash
# Clone the repository
git clone https://github.com/lepinkainen/downwatch.git
cd downwatch

# Build using Task
task build

# Or build with Go directly
go build -o downwatch .
```

## Usage

### Quick Start

Create a `config.yaml` file:

```yaml
watch_dir: ~/Downloads

rules:
  - name: PDFs
    extensions: ["pdf"]
    dest: ~/Documents/PDFs

  - name: Images
    mime_prefixes: ["image/"]
    dest: ~/Pictures

  - name: Videos
    extensions: ["mp4", "mkv", "avi"]
    action: copy  # Keep original in Downloads
    dest: ~/Videos
```

Run downwatch:

```bash
./downwatch config.yaml
```

### Configuration

#### Basic Options

```yaml
watch_dir: ~/Downloads           # Directory to watch (default: ~/Downloads)
settle_millis: 1500              # Wait time for file stability (default: 1500)
poll_millis: 250                 # Polling interval for size checks (default: 250)
create_dest_dirs: true           # Auto-create destination directories (default: true)
notifications: true              # Show macOS notifications (default: true)
ignore_exts:                     # Extensions to ignore (defaults shown)
  - .crdownload
  - .download
  - .part
  - .partial
```

#### Rule Configuration

Rules are evaluated in order. First match wins.

```yaml
rules:
  - name: "Rule Name"
    # Match by glob patterns (matched against base filename)
    patterns:
      - "grok-*"
      - "model-*.bin"

    # Match by file extensions (case-insensitive, no leading dot)
    extensions:
      - pdf
      - jpg
      - png

    # Match by MIME type prefixes
    mime_prefixes:
      - "image/"
      - "video/"
      - "application/pdf"

    # Action: "move" (default) or "copy"
    action: move

    # Destination directory (~ expansion supported)
    dest: ~/Documents

    # Skip if duplicate exists (delete source for move, skip for copy)
    skip_duplicates: false

    # Optional: Upload to WebDAV after local operation
    webdav_upload: false
    webdav_path: /inbox/
```

#### WebDAV Configuration

```yaml
webdav:
  url: "https://copyparty.example.com/dav"
  username: "user"
  password: "pass"
  skip_tls_verify: false  # Only for self-signed certs
  timeout_sec: 30         # Upload timeout (default: 30)
```

### Example Configurations

#### Document Organizer

```yaml
watch_dir: ~/Downloads
rules:
  - name: Work Documents
    patterns: ["*-invoice-*", "*-receipt-*"]
    dest: ~/Documents/Work

  - name: PDFs
    extensions: ["pdf"]
    dest: ~/Documents/PDFs

  - name: Office Documents
    extensions: ["docx", "xlsx", "pptx"]
    dest: ~/Documents/Office
```

#### Media Library

```yaml
watch_dir: ~/Downloads
rules:
  - name: Photos
    mime_prefixes: ["image/"]
    dest: ~/Pictures/Inbox
    webdav_upload: true
    webdav_path: /photos/

  - name: Videos
    extensions: ["mp4", "mkv", "avi", "mov"]
    action: copy  # Keep original
    dest: ~/Videos

  - name: Music
    extensions: ["mp3", "flac", "wav", "m4a"]
    dest: ~/Music/Inbox
```

## Development

This project uses [Task](https://taskfile.dev/) for build automation.

### Requirements

- Go 1.25+
- Task (install from https://taskfile.dev/)
- golangci-lint (for linting)
- goimports (for formatting)

### Available Commands

```bash
# Build the project (runs tests and lint first)
task build

# Run tests
task test

# Run tests with coverage (for CI)
task test-ci

# Run linter
task lint

# Build without tests/lint (for CI)
task build-ci

# Clean build artifacts
task clean

# Run in development mode
task dev
```

### Project Structure

```
downwatch/
├── main.go           # Single-file application
├── main_test.go      # Unit tests
├── Taskfile.yml      # Build automation
├── .golangci.yml     # Linter configuration
├── go.mod            # Go dependencies
├── .github/
│   └── workflows/
│       ├── ci.yml        # PR/push validation
│       └── release.yml   # Multi-platform releases
└── build/            # Build artifacts (not in git)
```

### Contributing

1. Fork the repository
2. Create a feature branch (`git checkout -b feature/amazing-feature`)
3. Make your changes
4. Run tests and linting: `task build`
5. Commit your changes using [Conventional Commits](https://www.conventionalcommits.org/)
6. Push to the branch (`git push origin feature/amazing-feature`)
7. Open a Pull Request

### Testing

```bash
# Run all tests
task test

# Run specific test
go test -run TestExpandHome

# Run with verbose output
go test -v ./...

# Run benchmarks
go test -bench=.
```

## How It Works

1. **Watches directory** - Uses fsnotify to monitor filesystem events
2. **Stability checking** - Polls file size to ensure downloads are complete
3. **Rule matching** - Evaluates rules in order using patterns, extensions, or MIME types
4. **File operations** - Performs atomic moves/copies with duplicate handling
5. **Optional WebDAV** - Uploads files to remote DAV servers if configured
6. **Notifications** - Shows native macOS notifications for file operations

### Initial Scan Behavior

On startup, downwatch processes all existing files in the watch directory without stability checks (assumes files are complete). This "catch-up" mode:

- Skips stability polling
- For `copy` actions, skips files already present with same name+size
- Useful for recovering from daemon restarts

### Duplicate Handling

When a destination file exists, downwatch automatically renames files:

- `filename.ext` → `filename (2).ext`
- `filename (2).ext` → `filename (3).ext`
- And so on...

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

## Changelog

See [CHANGELOG.md](CHANGELOG.md) for a list of changes and version history.
