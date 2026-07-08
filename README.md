# Download Proxy

A fast and modern download proxy server built with FastAPI and curl_cffi, designed to bypass Cloudflare protection.

## Features

- 🚀 Built with FastAPI for high performance
- 🌐 Automatically bypasses Cloudflare protection using curl_cffi
- 💾 Supports streaming downloads for large files
- ⚡ Health check endpoint for monitoring
- 🔄 Automatic session management and recovery
- ⏱️ Request timeout protection

## Prerequisites

- Python 3.8 or higher
- UV (dependency manager) - automatically installed if not present

## Installation

```bash
# Initialize and install dependencies
uv sync
```

## Usage

```bash
# Run the server (default port: 8080)
uv run python download_proxy.py

# Run with custom port
PORT=8000 uv run python download_proxy.py
```

## API Endpoints

### Download File

**POST /download**

Request body:
```json
{
  "url": "https://example.com/file_to_download"
}
```

Response: Streaming download with appropriate headers

### Health Check

**GET /healthz**

Response:
```json
{"status": "ok"}
```

## Example Request

```bash
curl -X POST -H "Content-Type: application/json" -d '{"url": "https://example.com/file"}' http://localhost:8080/download -o downloaded_file
```

## License

MIT
