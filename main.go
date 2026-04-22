package main

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
)

func downloadHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "only POST method allowed", http.StatusMethodNotAllowed)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form data", http.StatusBadRequest)
		return
	}

	targetURL := r.FormValue("url")
	if targetURL == "" {
		http.Error(w, "url parameter is required", http.StatusBadRequest)
		return
	}

	if _, err := url.ParseRequestURI(targetURL); err != nil {
		http.Error(w, "invalid url", http.StatusBadRequest)
		return
	}

	resp, err := http.Get(targetURL)
	if err != nil {
		http.Error(w, fmt.Sprintf("download failed: %v", err), http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		http.Error(w, fmt.Sprintf("remote server returned: %s", resp.Status), resp.StatusCode)
		return
	}

	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	contentDisposition := resp.Header.Get("Content-Disposition")
	if contentDisposition == "" {
		u, _ := url.Parse(targetURL)
		filename := filepath.Base(u.Path)
		if filename == "" || filename == "/" {
			filename = "download"
		}
		contentDisposition = fmt.Sprintf(`attachment; filename="%s"`, filename)
	}

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", contentDisposition)
	if cl := resp.Header.Get("Content-Length"); cl != "" {
		w.Header().Set("Content-Length", cl)
	}

	io.Copy(w, resp.Body)
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	http.HandleFunc("/download", downloadHandler)
	fmt.Printf("server starting on :%s...\n", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		fmt.Printf("server error: %v\n", err)
	}
}
