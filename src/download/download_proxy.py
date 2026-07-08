#!/usr/bin/env python3
"""
Download proxy server using curl_cffi to bypass Cloudflare protection with FastAPI
"""

import os
import time
import logging
import urllib.parse
import threading
from fastapi import FastAPI, HTTPException
from fastapi.responses import StreamingResponse
from curl_cffi import requests
from pydantic import BaseModel

app = FastAPI(
    title="Download Proxy", description="Download proxy with Cloudflare bypass"
)
logging.basicConfig(
    level=logging.INFO, format="%(asctime)s - %(levelname)s - %(message)s"
)
logger = logging.getLogger(__name__)

# Global session for curl_cffi
session = None
session_lock = threading.Lock()


def build_session():
    """Build a new curl_cffi session"""
    try:
        return requests.Session(impersonate="chrome110")
    except Exception as e:
        logger.error(f"Failed to build session: {e}")
        raise


def get_session():
    """Get current session, create new one if needed. Thread-safe."""
    global session
    with session_lock:
        if session is None:
            session = build_session()
        return session


def reset_session():
    """Reset session to force creation of new one"""
    global session
    with session_lock:
        session = None


class RequestBody(BaseModel):
    """Request body model for download endpoint"""

    url: str


@app.post("/download")
async def download_handler(body: RequestBody):
    """
    Download handler - accepts JSON request with URL and streams response
    """
    start_time = time.time()
    logger.info("Received download request")

    url = body.url
    if not url:
        raise HTTPException(status_code=400, detail="URL field is required")

    parsed_url = urllib.parse.urlparse(url)
    if parsed_url.scheme not in ("http", "https"):
        raise HTTPException(
            status_code=400, detail="Invalid URL scheme, must be http or https"
        )

    logger.info(f"Downloading URL: {url}")

    try:
        # Create context with timeout
        session_obj = get_session()

        # Download with timeout
        response = session_obj.get(url, stream=True, timeout=45)
        logger.info(f"Received response with status: {response.status_code}")

        # Check for forbidden status and reset session if needed
        if response.status_code in (401, 403, 503):
            logger.warning(f"Received {response.status_code}, resetting session")
            reset_session()

        # Build response headers
        headers = {
            "Content-Type": response.headers.get(
                "Content-Type", "application/octet-stream"
            ),
        }

        if "Content-Disposition" not in response.headers:
            filename = os.path.basename(parsed_url.path)
            if not filename or filename in ("", "/", "."):
                filename = "download"
            headers["Content-Disposition"] = f'attachment; filename="{filename}"'

        if "Content-Length" in response.headers:
            headers["Content-Length"] = response.headers["Content-Length"]

        # Stream response
        def generate():
            bytes_written = 0
            try:
                for chunk in response.iter_content(chunk_size=4096):
                    if chunk:
                        yield chunk
                        bytes_written += len(chunk)
            finally:
                response.close()
                logger.info(
                    f"Download completed: URL={url}, Status={response.status_code}, "
                    f"Bytes={bytes_written}, Elapsed={time.time() - start_time:.2f}s"
                )

        return StreamingResponse(
            generate(), headers=headers, status_code=response.status_code
        )

    except Exception as e:
        logger.error(f"Download failed for URL={url}: {e}")
        reset_session()
        raise HTTPException(status_code=502, detail=f"Download failed: {e}")


@app.get("/healthz")
async def health_handler():
    """Health check endpoint"""
    return {"status": "ok"}


def main():
    """Main entry point for running the server"""
    port = os.getenv("PORT", "8080")
    logger.info(f"Server starting on port {port}")
    import uvicorn

    uvicorn.run(app, host="0.0.0.0", port=int(port))


if __name__ == "__main__":
    main()
