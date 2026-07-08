package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	cloudscraper "github.com/Advik-B/cloudscraper/lib"
)

type Request struct {
	URL string `json:"url"`
}

var (
	scraperMu sync.Mutex
	scraper   *cloudscraper.Scraper
)

func buildScraper() (*cloudscraper.Scraper, error) {
	return cloudscraper.New(
		cloudscraper.WithSessionConfig(true, 20*time.Minute, 3),
		cloudscraper.WithDelay(0),
	)
}

// getScraper 返回当前实例；如果不存在则创建。调用者需自行处理并发。
func getScraper() (*cloudscraper.Scraper, error) {
	if scraper != nil {
		return scraper, nil
	}
	sc, err := buildScraper()
	if err != nil {
		return nil, err
	}
	scraper = sc
	return scraper, nil
}

func resetScraper() {
	scraper = nil
}

func downloadHandler(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	if r.Method != http.MethodPost {
		http.Error(w, "only POST method allowed", http.StatusMethodNotAllowed)
		return
	}

	var req Request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	if req.URL == "" {
		http.Error(w, "url field is required", http.StatusBadRequest)
		return
	}

	parsed, err := url.ParseRequestURI(req.URL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		http.Error(w, "invalid url", http.StatusBadRequest)
		return
	}

	// 只有一个 host，用一把全局锁串行化 scraper.Get，避免 cloudscraper 内部并发不安全。
	// 拿到响应后就释放锁，io.Copy 阶段允许并发。
	scraperMu.Lock()
	sc, err := getScraper()
	if err != nil {
		scraperMu.Unlock()
		http.Error(w, fmt.Sprintf("scraper init failed: %v", err), http.StatusInternalServerError)
		return
	}

	// scraper.Get 不接 context，用 goroutine + select 实现请求阶段超时。
	type result struct {
		resp *http.Response
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		resp, err := sc.Get(req.URL)
		ch <- result{resp, err}
	}()

	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()

	var resp *http.Response
	select {
	case res := <-ch:
		if res.err != nil {
			resetScraper()
			scraperMu.Unlock()
			log.Printf("scraper.Get %s failed in %s: %v", req.URL, time.Since(start), res.err)
			http.Error(w, fmt.Sprintf("download failed: %v", res.err), http.StatusBadGateway)
			return
		}
		resp = res.resp
	case <-ctx.Done():
		resetScraper()
		scraperMu.Unlock()
		// 后台 goroutine 拿到响应后关掉 body，避免泄露
		go func() {
			if res := <-ch; res.resp != nil {
				res.resp.Body.Close()
			}
		}()
		log.Printf("scraper.Get %s timed out after %s", req.URL, time.Since(start))
		if errors.Is(ctx.Err(), context.Canceled) {
			http.Error(w, "client canceled", 499)
		} else {
			http.Error(w, "upstream timeout", http.StatusGatewayTimeout)
		}
		return
	}

	// 401/403/503 通常说明挑战失败或 session 挂了，丢掉 scraper 让下次重建
	if resp.StatusCode == http.StatusForbidden ||
		resp.StatusCode == http.StatusUnauthorized ||
		resp.StatusCode == http.StatusServiceUnavailable {
		resetScraper()
	}
	scraperMu.Unlock()

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
		filename := filepath.Base(parsed.Path)
		if filename == "" || filename == "/" || filename == "." {
			filename = "download"
		}
		contentDisposition = fmt.Sprintf(`attachment; filename="%s"`, filename)
	}

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", contentDisposition)
	if cl := resp.Header.Get("Content-Length"); cl != "" {
		w.Header().Set("Content-Length", cl)
	}

	n, copyErr := io.Copy(w, resp.Body)
	log.Printf("download %s status=%d bytes=%d elapsed=%s err=%v",
		req.URL, resp.StatusCode, n, time.Since(start), copyErr)
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, "ok")
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/download", downloadHandler)
	mux.HandleFunc("/healthz", healthHandler)

	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      0, // 下载不定长，用请求级 context 超时代替
		IdleTimeout:       120 * time.Second,
	}

	log.Printf("server starting on :%s...", port)
	if err := srv.ListenAndServe(); err != nil && !strings.Contains(err.Error(), "Server closed") {
		log.Fatalf("server error: %v", err)
	}
}
