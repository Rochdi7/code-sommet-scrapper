package webscraper

import (
	"context"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/google/uuid"
	"github.com/gosom/scrapemate"
	"github.com/mcnijman/go-emailaddress"
)

// WebScrapeJob visits a website and extracts contact information.
type WebScrapeJob struct {
	scrapemate.Job

	SiteTitle   string
	SearchQuery string
}

func NewWebScrapeJob(parentID, siteURL, title, searchQuery string) *WebScrapeJob {
	return &WebScrapeJob{
		Job: scrapemate.Job{
			ID:         uuid.New().String(),
			ParentID:   parentID,
			Method:     http.MethodGet,
			URL:        siteURL,
			MaxRetries: 1,
			Priority:   scrapemate.PriorityHigh,
		},
		SiteTitle:   title,
		SearchQuery: searchQuery,
	}
}

func (j *WebScrapeJob) UseInResults() bool {
	return true
}

func (j *WebScrapeJob) ProcessOnFetchError() bool {
	return true
}

func (j *WebScrapeJob) Process(_ context.Context, resp *scrapemate.Response) (any, []scrapemate.IJob, error) {
	defer func() {
		resp.Document = nil
		resp.Body = nil
	}()

	result := &WebResult{
		URL:         j.URL,
		Title:       j.SiteTitle,
		SearchQuery: j.SearchQuery,
	}

	if resp.Error != nil {
		result.Error = resp.Error.Error()
		log.Printf("[webscraper] ERROR fetching %s: %v", j.URL, resp.Error)
		return result, nil, nil
	}

	doc, ok := resp.Document.(*goquery.Document)
	if !ok {
		result.Error = "could not parse HTML"
		log.Printf("[webscraper] ERROR parsing HTML for %s", j.URL)
		return result, nil, nil
	}

	bodyLen := len(resp.Body)
	log.Printf("[webscraper] DEBUG %s — body=%d bytes, status=%d", j.URL, bodyLen, resp.StatusCode)

	// Extract page title if we don't have one
	if result.Title == "" {
		result.Title = strings.TrimSpace(doc.Find("title").Text())
	}

	// Extract meta description — try multiple selectors
	result.Description = doc.Find(`meta[name="description"]`).AttrOr("content", "")
	if result.Description == "" {
		result.Description = doc.Find(`meta[property="og:description"]`).AttrOr("content", "")
	}

	// Extract emails from everywhere (mailto + regex on full body)
	emails := extractEmails(doc, resp.Body)
	if len(emails) > 0 {
		result.Emails = strings.Join(emails, ", ")
	}

	// Extract phone numbers from everywhere (tel: + regex on full body)
	phones := extractPhones(doc, resp.Body)
	if len(phones) > 0 {
		result.Phones = strings.Join(phones, ", ")
	}

	// Extract social links
	socials := extractSocials(doc)
	if len(socials) > 0 {
		result.Socials = strings.Join(socials, ", ")
	}

	log.Printf("[webscraper] RESULT %s — title=%q emails=%d phones=%d socials=%d desc_len=%d",
		j.URL, truncStr(result.Title, 50), len(emails), len(phones), len(socials), len(result.Description))

	// If we found nothing useful, try contact/about pages
	if len(emails) == 0 && len(phones) == 0 {
		var childJobs []scrapemate.IJob
		childJobs = findContactPages(doc, j.URL, j.ID, j.SearchQuery)
		if len(childJobs) > 0 {
			log.Printf("[webscraper] %s — no contacts on main page, spawning %d contact page jobs", j.URL, len(childJobs))
			return result, childJobs, nil
		}
	}

	return result, nil, nil
}

// findContactPages looks for /contact, /about, /impressum etc. links on the page
// and spawns child scrape jobs for them.
func findContactPages(doc *goquery.Document, parentURL, parentID, searchQuery string) []scrapemate.IJob {
	parsedParent, err := url.Parse(parentURL)
	if err != nil {
		return nil
	}

	contactPatterns := []string{
		"contact", "kontakt", "nous-contacter", "contacto",
		"about", "about-us", "a-propos", "uber-uns",
		"impressum", "imprint", "legal",
	}

	seen := make(map[string]bool)
	var jobs []scrapemate.IJob

	doc.Find("a[href]").Each(func(_ int, s *goquery.Selection) {
		if len(jobs) >= 3 {
			return
		}

		href, exists := s.Attr("href")
		if !exists {
			return
		}

		href = strings.TrimSpace(href)
		if href == "" || strings.HasPrefix(href, "#") || strings.HasPrefix(href, "javascript:") {
			return
		}

		// Resolve relative URLs
		resolved, err := url.Parse(href)
		if err != nil {
			return
		}
		absoluteURL := parsedParent.ResolveReference(resolved)

		// Only follow links on the same domain
		if absoluteURL.Hostname() != parsedParent.Hostname() {
			return
		}

		normalized := absoluteURL.String()
		if seen[normalized] {
			return
		}

		pathLower := strings.ToLower(absoluteURL.Path)
		linkText := strings.ToLower(strings.TrimSpace(s.Text()))

		for _, pattern := range contactPatterns {
			if strings.Contains(pathLower, pattern) || strings.Contains(linkText, pattern) {
				seen[normalized] = true
				jobs = append(jobs, NewWebScrapeJob(parentID, normalized, "", searchQuery))
				log.Printf("[webscraper] found contact page: %s (matched %q)", normalized, pattern)
				break
			}
		}
	})

	return jobs
}

func (j *WebScrapeJob) BrowserActions(_ context.Context, page scrapemate.BrowserPage) scrapemate.Response {
	var resp scrapemate.Response

	pageResponse, err := page.Goto(j.URL, scrapemate.WaitUntilDOMContentLoaded)
	if err != nil {
		resp.Error = err
		log.Printf("[webscraper] browser ERROR navigating to %s: %v", j.URL, err)
		return resp
	}

	// Wait for page to fully render (some sites load content dynamically)
	page.WaitForTimeout(3 * time.Second)

	resp.URL = pageResponse.URL
	resp.StatusCode = pageResponse.StatusCode
	resp.Headers = pageResponse.Headers

	body, err := page.Content()
	if err != nil {
		resp.Error = err
		log.Printf("[webscraper] browser ERROR getting content from %s: %v", j.URL, err)
		return resp
	}

	resp.Body = []byte(body)

	log.Printf("[webscraper] browser OK %s — %d bytes, status %d", j.URL, len(resp.Body), resp.StatusCode)

	return resp
}

// WebResult is the data extracted from a single website.
type WebResult struct {
	URL         string `json:"url"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Emails      string `json:"emails"`
	Phones      string `json:"phones"`
	Socials     string `json:"socials"`
	SearchQuery string `json:"search_query"`
	Error       string `json:"error,omitempty"`
}

func (r *WebResult) CsvHeaders() []string {
	return []string{"url", "title", "description", "emails", "phones", "socials", "search_query"}
}

func (r *WebResult) CsvRow() []string {
	return []string{r.URL, r.Title, r.Description, r.Emails, r.Phones, r.Socials, r.SearchQuery}
}

func extractEmails(doc *goquery.Document, body []byte) []string {
	seen := map[string]bool{}
	var emails []string

	// Method 1: mailto links
	doc.Find("a[href^='mailto:']").Each(func(_ int, s *goquery.Selection) {
		mailto, exists := s.Attr("href")
		if !exists {
			return
		}
		value := strings.TrimPrefix(mailto, "mailto:")
		value = strings.Split(value, "?")[0] // remove query params
		email, err := emailaddress.Parse(strings.TrimSpace(value))
		if err == nil && !seen[email.String()] {
			emails = append(emails, email.String())
			seen[email.String()] = true
		}
	})

	// Method 2: ALWAYS also scan full body text with regex (not just as fallback)
	addresses := emailaddress.Find(body, false)
	for i := range addresses {
		addr := addresses[i].String()
		if !seen[addr] {
			// Filter out common false positives
			if isLikelyFalseEmail(addr) {
				continue
			}
			emails = append(emails, addr)
			seen[addr] = true
		}
	}

	// Method 3: Look for obfuscated emails like "info [at] domain [dot] com"
	obfuscated := findObfuscatedEmails(string(body))
	for _, addr := range obfuscated {
		if !seen[addr] {
			emails = append(emails, addr)
			seen[addr] = true
		}
	}

	if len(emails) > 10 {
		emails = emails[:10]
	}

	log.Printf("[webscraper] extractEmails: mailto=%d, regex=%d, obfuscated=%d, total=%d",
		countMailto(doc), len(addresses), len(obfuscated), len(emails))

	return emails
}

// isLikelyFalseEmail filters out common false-positive email addresses.
func isLikelyFalseEmail(email string) bool {
	lower := strings.ToLower(email)
	falseDomains := []string{
		"example.com", "sentry.io", "wixpress.com", "googleapis.com",
		"w3.org", "schema.org", "gravatar.com", "wordpress.org",
		"jquery.com", "cloudflare.com",
	}
	for _, d := range falseDomains {
		if strings.HasSuffix(lower, "@"+d) || strings.Contains(lower, d) {
			return true
		}
	}
	// Filter out very long "emails" that are likely CSS class names or hashes
	if len(email) > 60 {
		return true
	}
	return false
}

// findObfuscatedEmails looks for patterns like "info [at] domain [dot] com"
// or "info(at)domain(dot)com" or "info AT domain DOT com".
var obfuscatedEmailRegex = regexp.MustCompile(
	`(?i)([a-zA-Z0-9._%+\-]+)\s*[\[\(]?\s*(?:at|@)\s*[\]\)]?\s*([a-zA-Z0-9.\-]+)\s*[\[\(]?\s*(?:dot|\.)\s*[\]\)]?\s*([a-zA-Z]{2,6})`,
)

func findObfuscatedEmails(body string) []string {
	var emails []string
	matches := obfuscatedEmailRegex.FindAllStringSubmatch(body, 5)
	for _, m := range matches {
		if len(m) >= 4 {
			email := m[1] + "@" + m[2] + "." + m[3]
			parsed, err := emailaddress.Parse(email)
			if err == nil {
				emails = append(emails, parsed.String())
			}
		}
	}
	return emails
}

func countMailto(doc *goquery.Document) int {
	count := 0
	doc.Find("a[href^='mailto:']").Each(func(_ int, _ *goquery.Selection) {
		count++
	})
	return count
}

var phoneRegex = regexp.MustCompile(`(?:(?:\+|00)\d{1,3}[\s.\-]?)?\(?\d{2,4}\)?[\s.\-]?\d{2,4}[\s.\-]?\d{2,4}(?:[\s.\-]?\d{1,4})?`)

func extractPhones(doc *goquery.Document, body []byte) []string {
	seen := map[string]bool{}
	var phones []string

	// Method 1: tel: links (always)
	doc.Find("a[href^='tel:']").Each(func(_ int, s *goquery.Selection) {
		tel, exists := s.Attr("href")
		if !exists {
			return
		}
		value := strings.TrimPrefix(tel, "tel:")
		value = strings.TrimSpace(value)
		if value != "" && !seen[value] {
			phones = append(phones, value)
			seen[value] = true
		}
	})

	telCount := len(phones)

	// Method 2: ALWAYS also scan body text with regex (not just as fallback)
	bodyStr := string(body)
	// Strip HTML tags for cleaner phone detection
	stripped := stripHTMLTags(bodyStr)
	matches := phoneRegex.FindAllString(stripped, 20)
	for _, m := range matches {
		m = strings.TrimSpace(m)
		// Filter: must have 7-15 digits
		digits := digitRegex.FindAllString(m, -1)
		if len(digits) < 7 || len(digits) > 15 {
			continue
		}
		// Normalize for dedup
		normalized := strings.Join(digits, "")
		if !seen[normalized] && !seen[m] {
			phones = append(phones, m)
			seen[normalized] = true
			seen[m] = true
		}
	}

	// Limit to 5 phones
	if len(phones) > 5 {
		phones = phones[:5]
	}

	log.Printf("[webscraper] extractPhones: tel_links=%d, regex=%d, total=%d",
		telCount, len(phones)-telCount, len(phones))

	return phones
}

var digitRegex = regexp.MustCompile(`\d`)

// stripHTMLTags removes HTML tags from a string for cleaner text extraction.
var htmlTagRegex = regexp.MustCompile(`<[^>]*>`)

func stripHTMLTags(s string) string {
	return htmlTagRegex.ReplaceAllString(s, " ")
}

func extractSocials(doc *goquery.Document) []string {
	seen := map[string]bool{}
	var socials []string

	socialDomains := []string{
		"facebook.com", "fb.com",
		"twitter.com", "x.com",
		"linkedin.com",
		"instagram.com",
		"tiktok.com",
		"pinterest.com",
		"wa.me", "whatsapp.com",
	}

	doc.Find("a[href]").Each(func(_ int, s *goquery.Selection) {
		href, exists := s.Attr("href")
		if !exists {
			return
		}

		href = strings.TrimSpace(href)
		hrefLower := strings.ToLower(href)

		// Skip sharing/intent links (not profile links)
		if strings.Contains(hrefLower, "share") && strings.Contains(hrefLower, "?") {
			return
		}
		if strings.Contains(hrefLower, "/intent/") {
			return
		}
		if strings.Contains(hrefLower, "/sharer") {
			return
		}

		for _, domain := range socialDomains {
			if strings.Contains(hrefLower, domain) && !seen[href] {
				socials = append(socials, href)
				seen[href] = true
				break
			}
		}
	})

	// Limit to 10 social links
	if len(socials) > 10 {
		socials = socials[:10]
	}

	return socials
}

func truncStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
