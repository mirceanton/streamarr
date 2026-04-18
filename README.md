# StreamArr

StreamArr is a self-hosted web application for managing audio and subtitle tracks in your media library. It scans your movies and TV shows, inspects every stream with ffprobe, and lets you queue ffmpeg jobs to remove or extract tracks directly from the browser.

> [!WARNING]
> **AI Slop / Vibe Coded Project** — This is project is "AI Slop". This is something that was "built" in a weekend with heavy AI assistance to scratch a personal itch. The code works for my use case, but it has not been hardened, audited, or battle-tested. Deploy at your own risk, preferably not exposed to the open internet. No warranties, no support guarantees, no promises.

## Features

- **Library scanning**: Index movies and TV shows from configured folders
- **Stream inspection**: View all audio and subtitle tracks per file (codec, language, channels, flags)
- **Stream management**: Manage audio and subtitle tracks per file (embed subs, extract subs, remove audio/subtitle stream etc.)
- **Preferred languages**: Flag files that have no audio or subtitle track in your preferred languages
- **Preferred subtitle format**: Flag files that have no subtitle tracks in your preferred format

## Deployment

StreamArr is distributed as a Docker image. Mount your media and a config directory, then point it at your libraries from the Settings page.

```yaml
services:
  streamarr:
    image: ghcr.io/mirceanton/streamarr:latest
    ports:
      - "8080:8080"
    volumes:
      - ./config:/config       # SQLite database
      - /path/to/media:/media  # Your media files
    environment:
      STREAMARR_PORT: "8080"
      STREAMARR_CONFIG_PATH: /config/streamarr.db
    restart: unless-stopped
```

> [!IMPORTANT]
> The container runs as UID/GID `1000`. Make sure the `/config` and `/media` mounts are readable (and writable for `/config`) by that user.

## Configuration

| Environment Variable    | Description                         | Default                   |
|-------------------------|-------------------------------------|---------------------------|
| `STREAMARR_PORT`        | Port the web server listens on      | `8080`                    |
| `STREAMARR_CONFIG_PATH` | Path to the SQLite database file    | `/config/streamarr.db`    |

## Usage

1. Open the web UI and navigate to **Settings**
2. Add one or more library roots (a name, a path, and a type — `movies` or `shows`)
3. Click **Scan All** (or scan a single library) — StreamArr will walk the directory tree and probe every media file with ffprobe
4. Browse **Movies** or **Shows** — files flagged as needing attention are highlighted
5. Click a file to see its full track listing and queue jobs from the detail page
6. Monitor job progress under **Jobs**

## Building from Source

Requirements: Go 1.25+, ffmpeg/ffprobe on `$PATH`

```bash
git clone https://github.com/mirceanton/streamarr
cd streamarr
go build -o streamarr .
STREAMARR_CONFIG_PATH=./streamarr.db ./streamarr
```
