package gmaps

import (
	"context"
	"log"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/google/uuid"
	"github.com/gosom/scrapemate"
	"github.com/mcnijman/go-emailaddress"

	"github.com/gosom/google-maps-scraper/exiter"
)

type EmailExtractJobOptions func(*EmailExtractJob)

type EmailExtractJob struct {
	scrapemate.Job

	Entry                   *Entry
	ExitMonitor             exiter.Exiter
	WriterManagedCompletion bool
	IsContactPage           bool // true if this is a follow-up job for /contact etc.
}

func NewEmailJob(parentID string, entry *Entry, opts ...EmailExtractJobOptions) *EmailExtractJob {
	const (
		defaultPrio       = scrapemate.PriorityHigh
		defaultMaxRetries = 1
	)

	job := EmailExtractJob{
		Job: scrapemate.Job{
			ID:         uuid.New().String(),
			ParentID:   parentID,
			Method:     "GET",
			URL:        normalizeGoogleURL(entry.WebSite),
			MaxRetries: defaultMaxRetries,
			Priority:   defaultPrio,
		},
	}

	job.Entry = entry

	for _, opt := range opts {
		opt(&job)
	}

	return &job
}

func WithEmailJobExitMonitor(exitMonitor exiter.Exiter) EmailExtractJobOptions {
	return func(j *EmailExtractJob) {
		j.ExitMonitor = exitMonitor
	}
}

func WithEmailJobWriterManagedCompletion() EmailExtractJobOptions {
	return func(j *EmailExtractJob) {
		j.WriterManagedCompletion = true
	}
}

func (j *EmailExtractJob) Process(ctx context.Context, resp *scrapemate.Response) (any, []scrapemate.IJob, error) {
	defer func() {
		resp.Document = nil
		resp.Body = nil
	}()

	logger := scrapemate.GetLoggerFromContext(ctx)
	logger.Info("Processing email job", "url", j.URL, "is_contact_page", j.IsContactPage)

	// If html fetch failed just return the entry as-is
	if resp.Error != nil {
		log.Printf("[email-extract] ERROR fetching %s: %v", j.URL, resp.Error)
		if j.ExitMonitor != nil && !j.WriterManagedCompletion {
			j.ExitMonitor.IncrPlacesCompleted(1)
		}
		return j.Entry, nil, nil
	}

	doc, ok := resp.Document.(*goquery.Document)
	if !ok {
		log.Printf("[email-extract] ERROR parsing HTML for %s", j.URL)
		if j.ExitMonitor != nil && !j.WriterManagedCompletion {
			j.ExitMonitor.IncrPlacesCompleted(1)
		}
		return j.Entry, nil, nil
	}

	bodyLen := len(resp.Body)
	log.Printf("[email-extract] DEBUG %s — body=%d bytes", j.URL, bodyLen)

	// 1. Extract emails (mailto + regex + obfuscated)
	emails := extractAllEmails(doc, resp.Body)
	if len(emails) > 0 {
		// Merge with existing emails (avoid duplicates)
		seen := make(map[string]bool)
		for _, e := range j.Entry.Emails {
			seen[strings.ToLower(e)] = true
		}
		for _, e := range emails {
			if !seen[strings.ToLower(e)] {
				j.Entry.Emails = append(j.Entry.Emails, e)
				seen[strings.ToLower(e)] = true
			}
		}
	}

	// 2. Extract WhatsApp numbers
	whatsapp := extractWhatsApp(doc, resp.Body)
	if whatsapp != "" && j.Entry.WhatsApp == "" {
		j.Entry.WhatsApp = whatsapp
	}

	// 3. Extract social media profiles
	socials := extractSocialProfiles(doc)
	if len(socials) > 0 {
		if j.Entry.SocialMedia == nil {
			j.Entry.SocialMedia = make(map[string]string)
		}
		for platform, link := range socials {
			if _, exists := j.Entry.SocialMedia[platform]; !exists {
				j.Entry.SocialMedia[platform] = link
			}
		}
	}

	log.Printf("[email-extract] RESULT %s — emails=%d whatsapp=%q socials=%d",
		j.URL, len(j.Entry.Emails), j.Entry.WhatsApp, len(j.Entry.SocialMedia))

	// 4. If we're on the homepage and found nothing, crawl contact/about pages
	if !j.IsContactPage && len(j.Entry.Emails) == 0 && j.Entry.WhatsApp == "" {
		contactPages := findContactLinks(doc, j.URL)
		if len(contactPages) > 0 {
			log.Printf("[email-extract] %s — no emails found, spawning %d contact page jobs", j.URL, len(contactPages))

			var childJobs []scrapemate.IJob
			for _, contactURL := range contactPages {
				childJob := &EmailExtractJob{
					Job: scrapemate.Job{
						ID:         uuid.New().String(),
						ParentID:   j.ParentID,
						Method:     "GET",
						URL:        contactURL,
						MaxRetries: 0,
						Priority:   scrapemate.PriorityHigh,
					},
					Entry:                   j.Entry,
					ExitMonitor:             j.ExitMonitor,
					WriterManagedCompletion: j.WriterManagedCompletion,
					IsContactPage:           true,
				}
				childJobs = append(childJobs, childJob)
			}

			// Don't mark as complete yet — the child jobs will emit the entry
			return nil, childJobs, nil
		}
	}

	// Mark as complete
	if j.ExitMonitor != nil && !j.WriterManagedCompletion {
		j.ExitMonitor.IncrPlacesCompleted(1)
	}

	return j.Entry, nil, nil
}

func (j *EmailExtractJob) ProcessOnFetchError() bool {
	return true
}

// BrowserActions uses a headless browser to fetch the page,
// so we get JavaScript-rendered content (emails hidden behind JS).
func (j *EmailExtractJob) BrowserActions(_ context.Context, page scrapemate.BrowserPage) scrapemate.Response {
	var resp scrapemate.Response

	pageResponse, err := page.Goto(j.URL, scrapemate.WaitUntilDOMContentLoaded)
	if err != nil {
		resp.Error = err
		log.Printf("[email-extract] browser ERROR navigating to %s: %v", j.URL, err)
		return resp
	}

	// Wait for dynamic content to render
	page.WaitForTimeout(3 * time.Second)

	resp.URL = pageResponse.URL
	resp.StatusCode = pageResponse.StatusCode
	resp.Headers = pageResponse.Headers

	body, err := page.Content()
	if err != nil {
		resp.Error = err
		log.Printf("[email-extract] browser ERROR getting content from %s: %v", j.URL, err)
		return resp
	}

	resp.Body = []byte(body)

	log.Printf("[email-extract] browser OK %s — %d bytes, status %d", j.URL, len(resp.Body), resp.StatusCode)

	return resp
}

// -------------------------------------------------------------------
// Email extraction
// -------------------------------------------------------------------

func extractAllEmails(doc *goquery.Document, body []byte) []string {
	seen := map[string]bool{}
	var emails []string

	// Method 1: mailto links
	doc.Find("a[href^='mailto:']").Each(func(_ int, s *goquery.Selection) {
		mailto, exists := s.Attr("href")
		if !exists {
			return
		}
		value := strings.TrimPrefix(mailto, "mailto:")
		value = strings.Split(value, "?")[0]
		email, err := getValidEmail(value)
		if err == nil && !seen[strings.ToLower(email)] {
			emails = append(emails, email)
			seen[strings.ToLower(email)] = true
		}
	})

	// Method 2: regex on full body (always, not just fallback)
	addresses := emailaddress.Find(body, false)
	for i := range addresses {
		addr := addresses[i].String()
		lower := strings.ToLower(addr)
		if !seen[lower] && !isFalsePositiveEmail(addr) {
			emails = append(emails, addr)
			seen[lower] = true
		}
	}

	// Method 3: obfuscated emails (info [at] domain [dot] com)
	obfuscated := findObfuscatedEmails(string(body))
	for _, addr := range obfuscated {
		lower := strings.ToLower(addr)
		if !seen[lower] {
			emails = append(emails, addr)
			seen[lower] = true
		}
	}

	// Limit
	if len(emails) > 10 {
		emails = emails[:10]
	}

	return emails
}

// isFalsePositiveEmail filters out common false positives.
func isFalsePositiveEmail(email string) bool {
	lower := strings.ToLower(email)

	// Skip known non-business domains
	falseDomains := []string{
		"example.com", "sentry.io", "wixpress.com", "googleapis.com",
		"w3.org", "schema.org", "gravatar.com", "wordpress.org",
		"jquery.com", "cloudflare.com", "google.com", "gstatic.com",
		"amazonaws.com", "bootstrapcdn.com", "fontawesome.com",
		"wp.com", "squarespace.com", "shopify.com",
	}

	for _, d := range falseDomains {
		if strings.HasSuffix(lower, "@"+d) || strings.Contains(lower, "."+d) {
			return true
		}
	}

	// Very long = likely a hash or CSS class
	if len(email) > 60 {
		return true
	}

	// Starts with common non-person patterns
	nonPersonPrefixes := []string{
		"noreply@", "no-reply@", "mailer-daemon@", "postmaster@",
		"webmaster@", "root@", "nobody@",
	}
	for _, p := range nonPersonPrefixes {
		if strings.HasPrefix(lower, p) {
			return true
		}
	}

	return false
}

var obfuscatedEmailRegex = regexp.MustCompile(
	`(?i)([a-zA-Z0-9._%+\-]+)\s*[\[\(]?\s*(?:at|@)\s*[\]\)]?\s*([a-zA-Z0-9.\-]+)\s*[\[\(]?\s*(?:dot|\.)\s*[\]\)]?\s*([a-zA-Z]{2,6})`,
)

func findObfuscatedEmails(body string) []string {
	var emails []string
	matches := obfuscatedEmailRegex.FindAllStringSubmatch(body, 5)
	for _, m := range matches {
		if len(m) >= 4 {
			email := m[1] + "@" + m[2] + "." + m[3]
			parsed, err := getValidEmail(email)
			if err == nil {
				emails = append(emails, parsed)
			}
		}
	}
	return emails
}

// Legacy extractors kept for compatibility
func docEmailExtractor(doc *goquery.Document) []string {
	return extractAllEmails(doc, nil)
}

func regexEmailExtractor(body []byte) []string {
	seen := map[string]bool{}
	var emails []string

	addresses := emailaddress.Find(body, false)
	for i := range addresses {
		addr := addresses[i].String()
		if !seen[addr] && !isFalsePositiveEmail(addr) {
			emails = append(emails, addr)
			seen[addr] = true
		}
	}

	return emails
}

func getValidEmail(s string) (string, error) {
	email, err := emailaddress.Parse(strings.TrimSpace(s))
	if err != nil {
		return "", err
	}

	return email.String(), nil
}

// -------------------------------------------------------------------
// WhatsApp detection
// -------------------------------------------------------------------

// WhatsApp URL patterns:
// - wa.me/+212600000000
// - api.whatsapp.com/send?phone=212600000000
// - web.whatsapp.com/send?phone=...
// - wa.me/message/... (click to chat)
var whatsappLinkRegex = regexp.MustCompile(`(?i)(?:wa\.me|api\.whatsapp\.com/send|web\.whatsapp\.com/send)[/?].*?(?:phone=|\+?)(\d{7,15})`)
var whatsappHrefRegex = regexp.MustCompile(`(?i)(?:https?://)?(?:wa\.me|api\.whatsapp\.com/send\?phone=|web\.whatsapp\.com/send\?phone=)\+?(\d{7,15})`)

func extractWhatsApp(doc *goquery.Document, body []byte) string {
	// Method 1: Direct wa.me / whatsapp links in the DOM
	var whatsappNumber string

	doc.Find("a[href]").Each(func(_ int, s *goquery.Selection) {
		if whatsappNumber != "" {
			return // already found
		}

		href, exists := s.Attr("href")
		if !exists {
			return
		}

		href = strings.TrimSpace(href)
		hrefLower := strings.ToLower(href)

		// wa.me links
		if strings.Contains(hrefLower, "wa.me/") {
			num := extractWhatsAppNumber(href)
			if num != "" {
				whatsappNumber = num
				return
			}
		}

		// api.whatsapp.com/send?phone=
		if strings.Contains(hrefLower, "whatsapp.com") {
			num := extractWhatsAppNumber(href)
			if num != "" {
				whatsappNumber = num
				return
			}
		}
	})

	if whatsappNumber != "" {
		log.Printf("[email-extract] found WhatsApp via link: %s", whatsappNumber)
		return whatsappNumber
	}

	// Method 2: Scan full body for WhatsApp URLs
	bodyStr := string(body)
	matches := whatsappHrefRegex.FindStringSubmatch(bodyStr)
	if len(matches) >= 2 {
		whatsappNumber = "+" + strings.TrimLeft(matches[1], "+")
		log.Printf("[email-extract] found WhatsApp via body regex: %s", whatsappNumber)
		return whatsappNumber
	}

	// Method 3: Look for WhatsApp icons/buttons with nearby phone numbers
	doc.Find(`a[href*="whatsapp"], a[class*="whatsapp"], a[class*="wa-"], a[aria-label*="WhatsApp"], a[title*="WhatsApp"]`).Each(func(_ int, s *goquery.Selection) {
		if whatsappNumber != "" {
			return
		}

		href, _ := s.Attr("href")
		if href != "" {
			num := extractWhatsAppNumber(href)
			if num != "" {
				whatsappNumber = num
				return
			}
		}

		// Check surrounding text for a phone number
		text := strings.TrimSpace(s.Text())
		if num := extractPhoneFromText(text); num != "" {
			whatsappNumber = num
		}
	})

	if whatsappNumber != "" {
		log.Printf("[email-extract] found WhatsApp via button/icon: %s", whatsappNumber)
	}

	return whatsappNumber
}

// extractWhatsAppNumber parses a WhatsApp URL and returns the phone number.
func extractWhatsAppNumber(rawURL string) string {
	// Try wa.me/{number}
	if strings.Contains(rawURL, "wa.me/") {
		parsed, err := url.Parse(rawURL)
		if err == nil {
			path := strings.TrimPrefix(parsed.Path, "/")
			// Remove any query params or path segments after the number
			path = strings.Split(path, "/")[0]
			path = strings.Split(path, "?")[0]
			num := cleanPhoneNumber(path)
			if len(num) >= 7 {
				return "+" + strings.TrimLeft(num, "+")
			}
		}
	}

	// Try ?phone= parameter
	parsed, err := url.Parse(rawURL)
	if err == nil {
		phone := parsed.Query().Get("phone")
		if phone != "" {
			num := cleanPhoneNumber(phone)
			if len(num) >= 7 {
				return "+" + strings.TrimLeft(num, "+")
			}
		}
	}

	// Regex fallback
	matches := whatsappLinkRegex.FindStringSubmatch(rawURL)
	if len(matches) >= 2 {
		return "+" + strings.TrimLeft(matches[1], "+")
	}

	return ""
}

var phoneCleanRegex = regexp.MustCompile(`[^\d+]`)
var phoneExtractRegex = regexp.MustCompile(`\+?\d[\d\s.\-]{6,14}\d`)

func cleanPhoneNumber(s string) string {
	return phoneCleanRegex.ReplaceAllString(s, "")
}

func extractPhoneFromText(text string) string {
	match := phoneExtractRegex.FindString(text)
	if match != "" {
		cleaned := cleanPhoneNumber(match)
		if len(cleaned) >= 7 && len(cleaned) <= 15 {
			return "+" + strings.TrimLeft(cleaned, "+")
		}
	}
	return ""
}

// -------------------------------------------------------------------
// Social media profile extraction
// -------------------------------------------------------------------

type socialPattern struct {
	platform string
	domains  []string
}

var socialPatterns = []socialPattern{
	{platform: "facebook", domains: []string{"facebook.com", "fb.com"}},
	{platform: "instagram", domains: []string{"instagram.com"}},
	{platform: "twitter", domains: []string{"twitter.com", "x.com"}},
	{platform: "linkedin", domains: []string{"linkedin.com"}},
	{platform: "tiktok", domains: []string{"tiktok.com"}},
	{platform: "pinterest", domains: []string{"pinterest.com"}},
	{platform: "youtube", domains: []string{"youtube.com"}},
}

func extractSocialProfiles(doc *goquery.Document) map[string]string {
	socials := make(map[string]string)

	doc.Find("a[href]").Each(func(_ int, s *goquery.Selection) {
		href, exists := s.Attr("href")
		if !exists {
			return
		}

		href = strings.TrimSpace(href)
		hrefLower := strings.ToLower(href)

		// Skip share/intent links
		if strings.Contains(hrefLower, "/sharer") ||
			strings.Contains(hrefLower, "/intent/") ||
			strings.Contains(hrefLower, "share?") ||
			strings.Contains(hrefLower, "share=") {
			return
		}

		for _, sp := range socialPatterns {
			// Already found this platform
			if _, exists := socials[sp.platform]; exists {
				continue
			}

			for _, domain := range sp.domains {
				if strings.Contains(hrefLower, domain) {
					socials[sp.platform] = href
					break
				}
			}
		}
	})

	return socials
}

// -------------------------------------------------------------------
// Contact page discovery
// -------------------------------------------------------------------

func findContactLinks(doc *goquery.Document, parentURL string) []string {
	parsedParent, err := url.Parse(parentURL)
	if err != nil {
		return nil
	}

	contactPatterns := []string{
		"contact", "kontakt", "nous-contacter", "contacto", "contatti",
		"about", "about-us", "a-propos", "uber-uns", "chi-siamo",
		"impressum", "imprint", "legal", "mentions-legales",
	}

	seen := make(map[string]bool)
	var urls []string

	doc.Find("a[href]").Each(func(_ int, s *goquery.Selection) {
		if len(urls) >= 3 {
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

		// Same domain only
		if absoluteURL.Hostname() != parsedParent.Hostname() {
			return
		}

		normalized := absoluteURL.String()
		if seen[normalized] || normalized == parentURL {
			return
		}

		pathLower := strings.ToLower(absoluteURL.Path)
		linkText := strings.ToLower(strings.TrimSpace(s.Text()))

		for _, pattern := range contactPatterns {
			if strings.Contains(pathLower, pattern) || strings.Contains(linkText, pattern) {
				seen[normalized] = true
				urls = append(urls, normalized)
				log.Printf("[email-extract] found contact page: %s (pattern: %q)", normalized, pattern)
				break
			}
		}
	})

	return urls
}

// -------------------------------------------------------------------
// URL normalization
// -------------------------------------------------------------------

// normalizeGoogleURL extracts the actual target URL from Google redirect URLs.
func normalizeGoogleURL(rawURL string) string {
	if rawURL == "" {
		return rawURL
	}

	if strings.HasPrefix(rawURL, "/url?q=") {
		fullURL := "https://www.google.com" + rawURL

		parsed, err := url.Parse(fullURL)
		if err != nil {
			return rawURL
		}

		if target := parsed.Query().Get("q"); target != "" {
			return target
		}
	}

	if strings.HasPrefix(rawURL, "/") {
		return "https://www.google.com" + rawURL
	}

	return rawURL
}
