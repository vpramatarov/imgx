# imgx

Batch image processor written in Go. Resizes, renames, converts format, and
strips EXIF from every image in a folder. Also ships an HTTP upload UI that
takes images (or a ZIP of images) and returns processed batches as a ZIP.
Originals are never modified.

## Install / run

### Via Docker (no Go toolchain required)

```bash
# Default: HTTP server on http://localhost:8080
docker compose up --build

# One-shot CLI batch run (reads ./input, writes ./output)
docker compose --profile cli run --rm imgx-cli \
    --out /data/output --width 1200 --format jpg /data/input

# Development container with hot reload via Air
docker compose -f docker-compose.dev.yml up --build
```

Change the host port / bind interface by copying `.env.example` to `.env` and
editing `IMGX_PORT` / `IMGX_BIND` (default `0.0.0.0:8080`). The container
always listens on `8080` internally.

### From source

```bash
# Without HEIF/AVIF (no CGO, no libheif needed)
go build -o imgx ./cmd/imgx

# With HEIF decode + AVIF encode/decode (needs libheif, libaom, libde265)
go build -tags heif -o imgx ./cmd/imgx

./imgx --out ./out --width 1200 --format jpg ./photos
```

## Usage

```
imgx [flags] <input-dir>

Flags:
  -o, --out          Output directory (required)
  -n, --name         Base name for output files (default: input folder name)
      --keep-names   Keep each file's original name (sanitized); --name is ignored
  -w, --width        Target width in px (keeps aspect ratio)
      --height       Target height in px (keeps aspect ratio)
      --fit          Fit within bounding box WxH (e.g. 1200x1200, never upscales)
  -f, --format       Output format: jpg, png, webp, avif, gif, tiff (default: keep original)
  -q, --quality      Quality 1-100, applies to JPEG and AVIF (default: 85)
      --sort         Sort order: alpha, mtime (default: OS order)
  -r, --recursive    Process subdirectories recursively
  -c, --config       Path to YAML/TOML config file
      --dry-run      Print what would happen without writing
  -v, --verbose      Verbose logging
      --concurrency  Parallel workers (default: NumCPU)
```

Resize flags (`--width`, `--height`, `--fit`) are mutually exclusive.

Per-image failures don't abort the batch — imgx logs each one as it happens
and, at the end, prints a consolidated `Failed (N):` block to **stderr** with
one line per failed input (base name + reason) before the summary line on
stdout. Exit code is `2` whenever any file failed.

## Config file

```yaml
# imgx.yaml
input: ./photos
out: ./out
name: my-images
# keep-names: true   # keep original (sanitized) file names; ignores name:
width: 1200
format: jpg
quality: 85
sort: alpha
recursive: true
concurrency: 4
```

CLI flags override config file values. Environment variables `IMGX_*` also
override file values but are overridden by CLI flags.

## Output naming

`<sanitised-name>-<n>.<ext>`. The base name is Cyrillic-transliterated
via BGN/PCGN, all other non-ASCII runes via go-unidecode, then lowercased
and sanitised to `[a-z0-9.-]` (other characters become `-`). If the result
already exists in the output directory, `-1`, `-2`, ... are appended until
a free slot is found.

With `--keep-names` (or the "Keep original file names" checkbox in the web
UI), each output instead keeps its input's file name — run through the same
transliteration/sanitisation — and only the extension changes with the
output format. Stem collisions get `-1`, `-2`, ... suffixes.

## Supported formats

| Format      | Read | Write | Notes                                                                  |
|-------------|------|-------|------------------------------------------------------------------------|
| JPEG        | yes  | yes   | Quality via `--quality`. Alpha-bearing sources (e.g. transparent PNG) are flattened onto **white** before encoding. |
| PNG         | yes  | yes   | Lossless.                                                              |
| GIF         | yes  | yes   | First frame only for animated input.                                   |
| WebP        | yes  | yes   | Pure-Go encoder is lossless; `--quality` ignored.                      |
| TIFF        | yes  | yes   | Deflate compression.                                                   |
| BMP         | yes  | no    | Decode-only; pass `--format` to convert.                               |
| **AVIF**    | yes  | yes   | Via libheif + aom (AV1). Quality via `--quality`. Needs `heif` tag.    |
| **HEIC/HEIF** | yes | no   | Decode-only (brands `heic`, `heix`, `mif1`, `msf1`). Needs `heif` tag. |

Format is detected by **magic bytes**, not by file extension.

### The `heif` build tag

HEIC/AVIF support is gated behind the `heif` build tag because it pulls in
libheif (C library) via CGO. The Docker image **always** builds with
`-tags heif`, so the published image has full HEIF/AVIF support out of the
box. Local `go build`/`go test` without the tag still compiles and is free
of CGO — HEIC/AVIF inputs will simply fail to decode and `--format avif`
returns an explanatory error. To build with HEIF locally:

```bash
# Debian / Ubuntu
sudo apt install libheif-dev libaom-dev libde265-dev pkg-config

go build -tags heif -o imgx ./cmd/imgx
go test  -tags heif ./...
```

HEIC write support is **intentionally omitted**. Debian's libheif pulls in
libx265 for HEVC encoding, which is GPL-licensed. If you need a modern
HEIF-family output, use `--format avif`, which goes through libaom's AV1
encoder (royalty-free).

### WebP

Encoding uses `github.com/HugoSmits86/nativewebp`, which is lossless-only,
so `--quality` is silently ignored for WebP output.

## Concurrency model

A single `imgx` process spawns a fixed worker pool of goroutines
(default = `NumCPU`). Workers pull jobs from one channel; each job is one
image (decode → resize → encode → write). The output index `{n}` is
pre-assigned from sorted scan order before jobs are dispatched, so
filenames stay deterministic regardless of which worker completes first.

## HTTP UI (`imgx serve`)

```bash
./imgx serve                       # listens on http://127.0.0.1:8080
./imgx serve --addr 0.0.0.0:8080   # expose on all interfaces
```

Flags:

```
--addr             Listen address (default 127.0.0.1:8080)
--max-file-size    Max size per uploaded file in bytes (default 52428800 = 50 MB)
--max-files        Max number of files per request (default 100)
--shutdown-timeout How long to wait for in-flight requests on SIGINT/SIGTERM (default 30s)
```

Open the listed URL in a browser, drop images — or a ZIP of images — on the
form, pick options, and the processed batch comes back as a ZIP. Uploads live
in a per-request temp dir and are deleted as soon as the response is written —
nothing is persisted.

ZIP uploads are expanded server-side: entries are detected by magic bytes,
non-image entries are skipped (logged and listed in `errors.txt` in the
result), and OS metadata junk (`__MACOSX/`, `._*`, `.DS_Store`, `Thumbs.db`)
is dropped silently. Each extracted image counts toward `--max-files` and must
fit `--max-file-size` uncompressed; the archive itself is also subject to
`--max-file-size` as an upload.

If some files are skipped or fail, the returned ZIP also contains an
`errors.txt` listing each one and why; the count is also exposed via an
`X-Imgx-Failed` response header, which the form surfaces as an inline
warning under the submit button. Selecting **Keep original**
while uploading HEIC/BMP (both decode-only) is rejected with HTTP 400; the
browser form auto-swaps the format to JPEG when it can see such filenames,
but contents hidden inside a ZIP are only caught by the server check.

Under Docker, this is the default entrypoint — `docker compose up --build`
starts the server on `http://localhost:${IMGX_PORT:-8080}`. There is **no
authentication**; set `IMGX_BIND=127.0.0.1` in `.env` to restrict to
loopback, or front the container with a reverse proxy before exposing it.

## Development

```bash
# Tests without HEIF (no libheif required)
go test ./...

# Tests with HEIF/AVIF (includes AVIF round-trip)
go test -tags heif ./...

# Hot reload via Air inside Docker (the Air build cmd uses -tags heif)
docker compose -f docker-compose.dev.yml up --build
```

## Project layout

```
cmd/imgx/            CLI entrypoint (root command + `serve` subcommand)
internal/config/     Flag + YAML/TOML merge (viper)
internal/scanner/    Directory walk + sort
internal/format/     Magic-byte detection / format registry
internal/processor/  Resize, encode, EXIF helper
                     convert_heif.go  (+heif tag: libheif decoders + AVIF encoder)
                     convert_noheif.go (!heif tag: stub)
internal/naming/     BGN/PCGN translit, sanitise, conflict resolve
internal/pipeline/   Worker pool orchestration
internal/server/     HTTP server, upload handler (incl. ZIP upload extraction), streamed ZIP response
```
