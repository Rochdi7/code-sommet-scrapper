package web

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"sort"
	"sync"
	"time"
)

// ProxyStatus holds the health status of a single proxy.
type ProxyStatus struct {
	URL          string        `json:"url"`
	Alive        bool          `json:"alive"`
	ResponseTime time.Duration `json:"response_time_ms"`
	LastChecked  time.Time     `json:"last_checked"`
	SuccessCount int           `json:"success_count"`
	FailCount    int           `json:"fail_count"`
	SuccessRate  float64       `json:"success_rate"`
}

// ProxyMonitor manages proxy health checks.
type ProxyMonitor struct {
	mu       sync.RWMutex
	statuses map[string]*ProxyStatus
}

// NewProxyMonitor creates a new ProxyMonitor.
func NewProxyMonitor() *ProxyMonitor {
	return &ProxyMonitor{
		statuses: make(map[string]*ProxyStatus),
	}
}

// CheckProxy tests a single proxy by making an HTTP GET request to Google Maps through it.
func (pm *ProxyMonitor) CheckProxy(ctx context.Context, proxyURL string) ProxyStatus {
	status := ProxyStatus{
		URL:         proxyURL,
		LastChecked: time.Now().UTC(),
	}

	parsedURL, err := url.Parse(proxyURL)
	if err != nil {
		log.Printf("proxy-monitor: invalid proxy URL %q: %v", proxyURL, err)

		status.Alive = false

		pm.recordResult(proxyURL, &status)

		return status
	}

	transport := &http.Transport{
		Proxy: http.ProxyURL(parsedURL),
	}

	client := &http.Client{
		Transport: transport,
		Timeout:   15 * time.Second,
	}

	start := time.Now()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://www.google.com/maps", nil)
	if err != nil {
		status.Alive = false

		pm.recordResult(proxyURL, &status)

		return status
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")

	resp, err := client.Do(req)
	if err != nil {
		status.Alive = false
		status.ResponseTime = time.Since(start)

		pm.recordResult(proxyURL, &status)

		return status
	}

	defer resp.Body.Close()

	status.ResponseTime = time.Since(start)
	status.Alive = resp.StatusCode >= 200 && resp.StatusCode < 400

	pm.recordResult(proxyURL, &status)

	return status
}

func (pm *ProxyMonitor) recordResult(proxyURL string, status *ProxyStatus) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	existing, ok := pm.statuses[proxyURL]
	if !ok {
		existing = &ProxyStatus{URL: proxyURL}
		pm.statuses[proxyURL] = existing
	}

	existing.Alive = status.Alive
	existing.ResponseTime = status.ResponseTime
	existing.LastChecked = status.LastChecked

	if status.Alive {
		existing.SuccessCount++
	} else {
		existing.FailCount++
	}

	total := existing.SuccessCount + existing.FailCount
	if total > 0 {
		existing.SuccessRate = float64(existing.SuccessCount) / float64(total) * 100
	}

	status.SuccessCount = existing.SuccessCount
	status.FailCount = existing.FailCount
	status.SuccessRate = existing.SuccessRate
}

// CheckAll checks all proxies concurrently with a max of 5 goroutines.
func (pm *ProxyMonitor) CheckAll(ctx context.Context, proxies []string) []ProxyStatus {
	results := make([]ProxyStatus, len(proxies))
	sem := make(chan struct{}, 5)

	var wg sync.WaitGroup

	for i, p := range proxies {
		wg.Add(1)

		go func(idx int, proxyURL string) {
			defer wg.Done()

			sem <- struct{}{}
			defer func() { <-sem }()

			results[idx] = pm.CheckProxy(ctx, proxyURL)
		}(i, p)
	}

	wg.Wait()

	return results
}

// GetStatuses returns all proxy statuses sorted by success rate descending.
func (pm *ProxyMonitor) GetStatuses() []ProxyStatus {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	statuses := make([]ProxyStatus, 0, len(pm.statuses))
	for _, s := range pm.statuses {
		statuses = append(statuses, *s)
	}

	sort.Slice(statuses, func(i, j int) bool {
		return statuses[i].SuccessRate > statuses[j].SuccessRate
	})

	return statuses
}

// GetHealthy returns only alive proxy URLs.
func (pm *ProxyMonitor) GetHealthy() []string {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	var healthy []string

	for _, s := range pm.statuses {
		if s.Alive {
			healthy = append(healthy, s.URL)
		}
	}

	return healthy
}

// RemoveDead removes proxies with 0% success rate after 3+ checks.
func (pm *ProxyMonitor) RemoveDead() int {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	removed := 0

	for url, s := range pm.statuses {
		total := s.SuccessCount + s.FailCount
		if total >= 3 && s.SuccessRate == 0 {
			delete(pm.statuses, url)

			removed++
		}
	}

	return removed
}

// FormatResponseTime returns a human-readable response time string.
func FormatResponseTime(d time.Duration) string {
	ms := d.Milliseconds()
	if ms < 1000 {
		return fmt.Sprintf("%dms", ms)
	}

	return fmt.Sprintf("%.1fs", d.Seconds())
}

// ResponseTimeClass returns a CSS class based on the response time.
func ResponseTimeClass(d time.Duration) string {
	switch {
	case d < 2*time.Second:
		return "rt-fast"
	case d < 5*time.Second:
		return "rt-medium"
	default:
		return "rt-slow"
	}
}

// ProxyRowClass returns a CSS class for the table row based on proxy status.
func ProxyRowClass(s ProxyStatus) string {
	if !s.Alive {
		return "proxy-dead"
	}

	if s.ResponseTime > 5*time.Second {
		return "proxy-slow"
	}

	return "proxy-alive"
}
