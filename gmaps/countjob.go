package gmaps

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/google/uuid"
	"github.com/gosom/scrapemate"
)

// CountFicheJob scrolls a Google Maps search to the very end and counts listings.
// It does NOT create PlaceJobs — it only returns the total count.
type CountFicheJob struct {
	scrapemate.Job

	LangCode string
	Count    int
}

func NewCountFicheJob(id, langCode, query, geoCoordinates string, zoom int) *CountFicheJob {
	query = url.QueryEscape(query)

	const (
		maxRetries = 3
		prio       = scrapemate.PriorityHigh
	)

	if id == "" {
		id = uuid.New().String()
	}

	mapURL := ""
	if geoCoordinates != "" && zoom > 0 {
		mapURL = fmt.Sprintf("https://www.google.com/maps/search/%s/@%s,%dz", query, strings.ReplaceAll(geoCoordinates, " ", ""), zoom)
	} else {
		mapURL = fmt.Sprintf("https://www.google.com/maps/search/%s", query)
	}

	return &CountFicheJob{
		Job: scrapemate.Job{
			ID:         id,
			Method:     http.MethodGet,
			URL:        mapURL,
			URLParams:  map[string]string{"hl": langCode},
			MaxRetries: maxRetries,
			Priority:   prio,
		},
		LangCode: langCode,
	}
}

func (j *CountFicheJob) UseInResults() bool {
	return false
}

func (j *CountFicheJob) ProcessOnFetchError() bool {
	return true
}

func (j *CountFicheJob) Process(_ context.Context, resp *scrapemate.Response) (any, []scrapemate.IJob, error) {
	defer func() {
		resp.Document = nil
		resp.Body = nil
	}()

	if resp.Error != nil {
		return nil, nil, resp.Error
	}

	doc, ok := resp.Document.(*goquery.Document)
	if !ok {
		return nil, nil, fmt.Errorf("could not convert to goquery document")
	}

	// Count all place links in the feed — this IS the fiche count
	count := 0

	if strings.Contains(resp.URL, "/maps/place/") {
		// Single place result
		count = 1
	} else {
		doc.Find(`div[role=feed] div[jsaction]>a`).Each(func(_ int, s *goquery.Selection) {
			if href := s.AttrOr("href", ""); href != "" {
				count++
			}
		})
	}

	j.Count = count

	// Return no next jobs — we only needed the count
	return nil, nil, nil
}

func (j *CountFicheJob) BrowserActions(ctx context.Context, page scrapemate.BrowserPage) scrapemate.Response {
	var resp scrapemate.Response

	pageResponse, err := page.Goto(j.GetFullURL(), scrapemate.WaitUntilDOMContentLoaded)
	if err != nil {
		resp.Error = err
		return resp
	}

	clickRejectCookiesIfRequired(page)

	const defaultTimeout = 5 * time.Second

	_ = page.WaitForURL(page.URL(), defaultTimeout)

	resp.URL = pageResponse.URL
	resp.StatusCode = pageResponse.StatusCode
	resp.Headers = pageResponse.Headers

	sel := `div[role='feed']`
	err = page.WaitForSelector(sel, 10*time.Second)

	var singlePlace bool

	if err != nil {
		waitCtx, waitCancel := context.WithTimeout(ctx, time.Second*5)
		defer waitCancel()

		singlePlace = waitUntilURLContains(waitCtx, page, "/maps/place/")
		waitCancel()
	}

	if singlePlace {
		resp.URL = page.URL()

		body, err := page.Content()
		if err != nil {
			resp.Error = err
			return resp
		}

		resp.Body = []byte(body)
		return resp
	}

	// Scroll with very high depth (200) to reach the absolute end of results
	scrollSelector := `div[role='feed']`

	_, err = scroll(ctx, page, 200, scrollSelector)
	if err != nil {
		resp.Error = err
		return resp
	}

	body, err := page.Content()
	if err != nil {
		resp.Error = err
		return resp
	}

	resp.Body = []byte(body)

	return resp
}
