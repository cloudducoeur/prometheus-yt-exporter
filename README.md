# Prometheus YouTube Exporter

YouTube Data API v3 - Prometheus exporter

## Build and run code

```bash
# Build code
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o prometheus-yt-exporter

# Run locally
export YOUTUBE_API_KEY=""
export YOUTUBE_VIDEO_ID=""
./prometheus-yt-exporter
```

