package webscraper

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/google/uuid"
	"github.com/gosom/scrapemate"
)

// GoogleSearchJob searches Google for a query and collects result URLs.
type GoogleSearchJob struct {
	scrapemate.Job

	Query      string
	MaxResults int
	LangCode   string
}

func NewGoogleSearchJob(query, langCode string, maxResults int) *GoogleSearchJob {
	encoded := url.QueryEscape(query)

	num := maxResults
	if num <= 0 || num > 100 {
		num = 10
	}

	searchURL := fmt.Sprintf("https://www.google.com/search?q=%s&num=%d&hl=%s", encoded, num, langCode)

	return &GoogleSearchJob{
		Job: scrapemate.Job{
			ID:         uuid.New().String(),
			Method:     http.MethodGet,
			URL:        searchURL,
			MaxRetries: 2,
			Priority:   scrapemate.PriorityLow,
		},
		Query:      query,
		MaxResults: maxResults,
		LangCode:   langCode,
	}
}

func (j *GoogleSearchJob) UseInResults() bool {
	return false
}

func (j *GoogleSearchJob) ProcessOnFetchError() bool {
	return true
}

func (j *GoogleSearchJob) Process(_ context.Context, resp *scrapemate.Response) (any, []scrapemate.IJob, error) {
	defer func() {
		resp.Document = nil
		resp.Body = nil
	}()

	if resp.Error != nil {
		log.Printf("[webscraper-search] ERROR query=%q: %v", j.Query, resp.Error)
		return nil, nil, resp.Error
	}

	log.Printf("[webscraper-search] processing query=%q body=%d bytes status=%d",
		j.Query, len(resp.Body), resp.StatusCode)

	// First try JS-extracted links (set by BrowserActions)
	if resp.Headers != nil {
		if linksStr := resp.Headers.Get("X-Extracted-Links"); linksStr != "" {
			var jsResults []jsSearchResult
			if json.Unmarshal([]byte(linksStr), &jsResults) == nil && len(jsResults) > 0 {
				var next []scrapemate.IJob
				for _, r := range jsResults {
					if j.MaxResults > 0 && len(next) >= j.MaxResults {
						break
					}
					next = append(next, NewWebScrapeJob(j.ID, r.URL, r.Title, j.Query))
				}
				if len(next) > 0 {
					log.Printf("[webscraper-search] query=%q found %d URLs via JavaScript extraction", j.Query, len(next))
					for i, job := range next {
						wsj := job.(*WebScrapeJob)
						log.Printf("[webscraper-search]   [%d] %s — %q", i+1, wsj.URL, wsj.SiteTitle)
					}
					return nil, next, nil
				}
			}
		}
	}

	doc, ok := resp.Document.(*goquery.Document)
	if !ok {
		log.Printf("[webscraper-search] ERROR query=%q: could not convert to goquery document", j.Query)
		return nil, nil, fmt.Errorf("could not convert to goquery document")
	}

	// Debug: dump some structure info
	searchDiv := doc.Find("#search")
	rsoDiv := doc.Find("#rso")
	gDivs := doc.Find("div.g")
	allLinks := doc.Find("a[href]")
	log.Printf("[webscraper-search] DEBUG query=%q — #search=%d #rso=%d div.g=%d total_links=%d",
		j.Query, searchDiv.Length(), rsoDiv.Length(), gDivs.Length(), allLinks.Length())

	// Check if Google is showing a CAPTCHA or block page
	pageTitle := strings.TrimSpace(doc.Find("title").Text())
	if strings.Contains(strings.ToLower(pageTitle), "unusual traffic") ||
		strings.Contains(strings.ToLower(pageTitle), "captcha") ||
		strings.Contains(strings.ToLower(pageTitle), "verify") {
		log.Printf("[webscraper-search] WARNING query=%q — Google CAPTCHA/block detected! title=%q", j.Query, pageTitle)
		return nil, nil, fmt.Errorf("google CAPTCHA detected for query %q", j.Query)
	}

	seen := make(map[string]bool)
	var next []scrapemate.IJob

	// Multiple selectors to handle different Google layouts
	selectors := []string{
		"div.g a[href]",           // classic layout
		"div[data-hveid] a[href]", // newer layout
		"a[data-ved][href]",       // data-ved links are organic results
		"#search a[href]",         // fallback: all links in search results
	}

	for selIdx, sel := range selectors {
		doc.Find(sel).Each(func(_ int, s *goquery.Selection) {
			if j.MaxResults > 0 && len(next) >= j.MaxResults {
				return
			}

			href, exists := s.Attr("href")
			if !exists {
				return
			}

			href = strings.TrimSpace(href)
			if href == "" {
				return
			}

			// Handle Google redirect URLs (/url?q=...)
			if strings.HasPrefix(href, "/url?") {
				if parsed, err := url.Parse("https://www.google.com" + href); err == nil {
					if q := parsed.Query().Get("q"); q != "" {
						href = q
					}
				}
			}

			// Skip relative, anchor, and Google internal links
			if strings.HasPrefix(href, "/") || strings.HasPrefix(href, "#") {
				return
			}

			parsed, err := url.Parse(href)
			if err != nil || parsed.Scheme == "" {
				return
			}

			host := strings.ToLower(parsed.Hostname())
			if host == "" {
				return
			}

			// Skip Google/YouTube/cache links
			skipDomains := []string{
				"google.com", "google.co", "google.fr", "google.de",
				"google.es", "google.it", "google.pt", "google.nl",
				"youtube.com", "googleapis.com", "gstatic.com",
				"webcache.googleusercontent.com", "translate.google",
				"accounts.google", "support.google", "maps.google",
			}
			skip := false
			for _, d := range skipDomains {
				if strings.Contains(host, d) {
					skip = true
					break
				}
			}
			if skip {
				return
			}

			// Normalize URL (remove fragments)
			parsed.Fragment = ""
			normalized := parsed.String()

			if seen[normalized] {
				return
			}
			seen[normalized] = true

			// Get title
			title := strings.TrimSpace(s.Find("h3").Text())
			if title == "" {
				title = strings.TrimSpace(s.Text())
			}
			// Truncate overly long titles (likely garbage)
			if len(title) > 200 {
				title = title[:200]
			}

			next = append(next, NewWebScrapeJob(j.ID, normalized, title, j.Query))
		})

		// If we found results with this selector, don't try others
		if len(next) > 0 {
			log.Printf("[webscraper-search] query=%q — selector[%d] %q matched %d URLs", j.Query, selIdx, sel, len(next))
			break
		}
	}

	if len(next) == 0 {
		// Debug: dump first 2000 chars of body to help diagnose
		bodySnippet := string(resp.Body)
		if len(bodySnippet) > 2000 {
			bodySnippet = bodySnippet[:2000]
		}
		log.Printf("[webscraper-search] WARNING query=%q — 0 URLs found! page_title=%q body_snippet:\n%s",
			j.Query, pageTitle, bodySnippet)
	} else {
		for i, job := range next {
			wsj := job.(*WebScrapeJob)
			log.Printf("[webscraper-search]   [%d] %s — %q", i+1, wsj.URL, wsj.SiteTitle)
		}
	}

	return nil, next, nil
}

type jsSearchResult struct {
	URL   string `json:"url"`
	Title string `json:"title"`
}

func (j *GoogleSearchJob) BrowserActions(_ context.Context, page scrapemate.BrowserPage) scrapemate.Response {
	var resp scrapemate.Response

	log.Printf("[webscraper-search] browser navigating to %s", j.GetFullURL())

	pageResponse, err := page.Goto(j.GetFullURL(), scrapemate.WaitUntilDOMContentLoaded)
	if err != nil {
		resp.Error = err
		log.Printf("[webscraper-search] browser ERROR: %v", err)
		return resp
	}

	// Dismiss cookie consent if present
	_, _ = page.Eval(`() => {
		const buttons = document.querySelectorAll('button');
		for (const btn of buttons) {
			const text = (btn.textContent || '').toLowerCase();
			if (text.includes('reject') || text.includes('decline') || text.includes('refuse') || text.includes('ablehnen') || text.includes('refuser')) {
				btn.click();
				return true;
			}
		}
		return false;
	}`)

	page.WaitForTimeout(3 * time.Second)

	// Extract links via JavaScript from the rendered page
	linksRaw, err := page.Eval(`() => {
		const results = [];
		const seen = new Set();

		// Method 1: search result containers
		document.querySelectorAll('#search a[href], #rso a[href], .g a[href]').forEach(a => {
			const href = a.href;
			if (!href || seen.has(href)) return;
			if (href.includes('google.com') || href.includes('youtube.com') || href.includes('googleapis.com')) return;
			if (href.startsWith('javascript:') || href.startsWith('#')) return;
			try {
				const u = new URL(href);
				if (u.protocol !== 'http:' && u.protocol !== 'https:') return;
			} catch(e) { return; }

			seen.add(href);
			const h3 = a.querySelector('h3');
			const title = h3 ? h3.innerText : (a.innerText || '').substring(0, 200);
			if (title) {
				results.push({url: href, title: title.trim()});
			}
		});

		return JSON.stringify(results);
	}`)

	if err == nil && linksRaw != nil {
		linksStr, ok := linksRaw.(string)
		if ok && linksStr != "" {
			var jsResults []jsSearchResult
			if json.Unmarshal([]byte(linksStr), &jsResults) == nil && len(jsResults) > 0 {
				// Store the JS-extracted links in the response headers so Process() can use them
				resp.Headers = make(http.Header)
				resp.Headers.Set("X-Extracted-Links", linksStr)
				log.Printf("[webscraper-search] browser JS extracted %d links for query=%q", len(jsResults), j.Query)
			}
		}
	} else if err != nil {
		log.Printf("[webscraper-search] browser JS extraction error: %v", err)
	}

	resp.URL = pageResponse.URL
	resp.StatusCode = pageResponse.StatusCode
	if resp.Headers == nil {
		resp.Headers = pageResponse.Headers
	}

	body, err := page.Content()
	if err != nil {
		resp.Error = err
		log.Printf("[webscraper-search] browser ERROR getting content: %v", err)
		return resp
	}

	resp.Body = []byte(body)

	log.Printf("[webscraper-search] browser OK %s — %d bytes, status %d", j.GetFullURL(), len(resp.Body), resp.StatusCode)

	return resp
}

// DirectScrapeJob directly scrapes a given URL (no Google search needed).
type DirectScrapeJob struct {
	scrapemate.Job

	SiteURL     string
	SearchQuery string
}

func NewDirectScrapeJob(siteURL, label string) *DirectScrapeJob {
	return &DirectScrapeJob{
		Job: scrapemate.Job{
			ID:         uuid.New().String(),
			Method:     http.MethodGet,
			URL:        siteURL,
			MaxRetries: 1,
			Priority:   scrapemate.PriorityHigh,
		},
		SiteURL:     siteURL,
		SearchQuery: label,
	}
}

func (j *DirectScrapeJob) UseInResults() bool {
	return false
}

func (j *DirectScrapeJob) ProcessOnFetchError() bool {
	return true
}

func (j *DirectScrapeJob) Process(_ context.Context, resp *scrapemate.Response) (any, []scrapemate.IJob, error) {
	defer func() {
		resp.Document = nil
		resp.Body = nil
	}()

	// Just spawn a WebScrapeJob for the URL
	title := ""
	if resp.Error == nil {
		if doc, ok := resp.Document.(*goquery.Document); ok {
			title = strings.TrimSpace(doc.Find("title").Text())
		}
	}
	if title == "" {
		title = j.SiteURL
	}

	log.Printf("[webscraper] direct scrape spawning job for %s — title=%q", j.SiteURL, title)

	return nil, []scrapemate.IJob{NewWebScrapeJob(j.ID, j.SiteURL, title, j.SearchQuery)}, nil
}

func (j *DirectScrapeJob) BrowserActions(_ context.Context, page scrapemate.BrowserPage) scrapemate.Response {
	var resp scrapemate.Response

	pageResponse, err := page.Goto(j.URL, scrapemate.WaitUntilDOMContentLoaded)
	if err != nil {
		resp.Error = err
		return resp
	}

	page.WaitForTimeout(2 * time.Second)

	resp.URL = pageResponse.URL
	resp.StatusCode = pageResponse.StatusCode
	resp.Headers = pageResponse.Headers

	body, err := page.Content()
	if err != nil {
		resp.Error = err
		return resp
	}

	resp.Body = []byte(body)
	return resp
}
