package webrunner

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gosom/google-maps-scraper/deduper"
	"github.com/gosom/google-maps-scraper/exiter"
	"github.com/gosom/google-maps-scraper/gmaps"
	"github.com/gosom/google-maps-scraper/runner"
	"github.com/gosom/google-maps-scraper/tlmt"
	"github.com/gosom/google-maps-scraper/web"
	"github.com/gosom/google-maps-scraper/web/sqlite"
	"github.com/gosom/google-maps-scraper/webscraper"
	"github.com/gosom/scrapemate"
	"github.com/gosom/scrapemate/adapters/writers/csvwriter"
	"github.com/gosom/scrapemate/scrapemateapp"
	"golang.org/x/sync/errgroup"
)

type webrunner struct {
	srv *web.Server
	svc *web.Service
	cfg *runner.Config
}

func New(cfg *runner.Config) (runner.Runner, error) {
	if cfg.DataFolder == "" {
		return nil, fmt.Errorf("data folder is required")
	}

	if err := os.MkdirAll(cfg.DataFolder, os.ModePerm); err != nil {
		return nil, err
	}

	const dbfname = "jobs.db"

	dbpath := filepath.Join(cfg.DataFolder, dbfname)

	repo, err := sqlite.New(dbpath)
	if err != nil {
		return nil, err
	}

	svc := web.NewService(repo, cfg.DataFolder)
	svc.SetScheduleRepo(repo)
	svc.SetLeadRepo(repo)
	svc.SetWebhookRepo(repo)
	svc.SetWebScraperRepo(repo)
	svc.SetQueueRepo(repo)

	srv, err := web.New(svc, cfg.Addr)
	if err != nil {
		return nil, err
	}

	ans := webrunner{
		srv: srv,
		svc: svc,
		cfg: cfg,
	}

	return &ans, nil
}

func (w *webrunner) Run(ctx context.Context) error {
	// Start the queue worker
	w.svc.StartQueueWorker(ctx)

	egroup, ctx := errgroup.WithContext(ctx)

	egroup.Go(func() error {
		return w.work(ctx)
	})

	egroup.Go(func() error {
		return w.srv.Start(ctx)
	})

	egroup.Go(func() error {
		return w.runScheduler(ctx)
	})

	return egroup.Wait()
}

func (w *webrunner) runScheduler(ctx context.Context) error {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := w.svc.RunDueSchedules(ctx); err != nil {
				log.Printf("scheduler error: %v", err)
			}
		}
	}
}

func (w *webrunner) Close(context.Context) error {
	return nil
}

func (w *webrunner) work(ctx context.Context) error {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			jobs, err := w.svc.SelectPending(ctx)
			if err != nil {
				return err
			}

			for i := range jobs {
				select {
				case <-ctx.Done():
					return nil
				default:
					t0 := time.Now().UTC()
					if err := w.scrapeJob(ctx, &jobs[i]); err != nil {
						params := map[string]any{
							"job_count": len(jobs[i].Data.Keywords),
							"duration":  time.Now().UTC().Sub(t0).String(),
							"error":     err.Error(),
						}

						evt := tlmt.NewEvent("web_runner", params)

						_ = runner.Telemetry().Send(ctx, evt)

						log.Printf("error scraping job %s: %v", jobs[i].ID, err)

						// Enqueue webhook for failed job
						w.svc.EnqueueWebhook(ctx, web.EventJobFailed, map[string]any{
							"job_id":   jobs[i].ID,
							"job_name": jobs[i].Name,
							"error":    err.Error(),
						})
					} else {
						params := map[string]any{
							"job_count": len(jobs[i].Data.Keywords),
							"duration":  time.Now().UTC().Sub(t0).String(),
						}

						_ = runner.Telemetry().Send(ctx, tlmt.NewEvent("web_runner", params))

						log.Printf("job %s scraped successfully", jobs[i].ID)

						// Enqueue webhook for completed job
						resultCount := w.countCSVResults(jobs[i].ID)
						w.svc.EnqueueWebhook(ctx, web.EventJobCompleted, map[string]any{
							"job_id":       jobs[i].ID,
							"job_name":     jobs[i].Name,
							"result_count": resultCount,
							"duration":     time.Now().UTC().Sub(t0).String(),
						})
					}
				}
			}
		}
	}
}

func (w *webrunner) scrapeJob(ctx context.Context, job *web.Job) error {
	job.Status = web.StatusWorking

	err := w.svc.Update(ctx, job)
	if err != nil {
		return err
	}

	if len(job.Data.Keywords) == 0 {
		job.Status = web.StatusFailed

		return w.svc.Update(ctx, job)
	}

	// Route to the appropriate scraper based on job type
	if job.Data.JobType == web.JobTypeWebScraper {
		return w.scrapeWebJob(ctx, job)
	}

	if job.Data.JobType == web.JobTypeCountFiche {
		return w.countFicheJob(ctx, job)
	}

	outpath := filepath.Join(w.cfg.DataFolder, job.ID+".csv")

	outfile, err := os.Create(outpath)
	if err != nil {
		return err
	}

	defer func() {
		_ = outfile.Close()
	}()

	mate, err := w.setupMate(ctx, outfile, job)
	if err != nil {
		job.Status = web.StatusFailed

		err2 := w.svc.Update(ctx, job)
		if err2 != nil {
			log.Printf("failed to update job status: %v", err2)
		}

		return err
	}

	defer mate.Close()

	var coords string
	if job.Data.Lat != "" && job.Data.Lon != "" {
		coords = job.Data.Lat + "," + job.Data.Lon
	}

	dedup := deduper.New()
	exitMonitor := exiter.New()

	// If MaxResults is set, calculate depth from it (each scroll ~7 results)
	depth := job.Data.Depth
	if job.Data.MaxResults > 0 {
		calculatedDepth := (job.Data.MaxResults / 7) + 3 // extra scrolls for safety
		if calculatedDepth > depth {
			depth = calculatedDepth
		}
	}

	seedJobs, err := runner.CreateSeedJobs(
		job.Data.FastMode,
		job.Data.Lang,
		strings.NewReader(strings.Join(job.Data.Keywords, "\n")),
		depth,
		job.Data.Email,
		coords,
		job.Data.Zoom,
		func() float64 {
			if job.Data.Radius <= 0 {
				return 10000 // 10 km
			}

			return float64(job.Data.Radius)
		}(),
		dedup,
		exitMonitor,
		w.cfg.ExtraReviews,
	)
	if err != nil {
		err2 := w.svc.Update(ctx, job)
		if err2 != nil {
			log.Printf("failed to update job status: %v", err2)
		}

		return err
	}

	if len(seedJobs) > 0 {
		exitMonitor.SetSeedCount(len(seedJobs))

		allowedSeconds := max(60, len(seedJobs)*10*depth/50+120)

		// Auto-scale time based on MaxResults if set
		if job.Data.MaxResults > 0 {
			// ~3 seconds per result is a safe estimate
			minNeeded := job.Data.MaxResults*3 + 180
			if minNeeded > allowedSeconds {
				allowedSeconds = minNeeded
			}
		}

		if job.Data.MaxTime > 0 {
			if job.Data.MaxTime.Seconds() < 180 {
				allowedSeconds = 180
			} else {
				maxTimeSec := int(job.Data.MaxTime.Seconds())
				if maxTimeSec > allowedSeconds {
					allowedSeconds = maxTimeSec
				}
			}
		}

		log.Printf("running job %s with %d seed jobs and %d allowed seconds", job.ID, len(seedJobs), allowedSeconds)

		mateCtx, cancel := context.WithTimeout(ctx, time.Duration(allowedSeconds)*time.Second)
		defer cancel()

		// Register cancel so the API can stop this job
		w.svc.RegisterJobCancel(job.ID, cancel)
		defer w.svc.UnregisterJobCancel(job.ID)

		exitMonitor.SetCancelFunc(cancel)

		go exitMonitor.Run(mateCtx)

		err = mate.Start(mateCtx, seedJobs...)
		if err != nil && !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
			cancel()

			err2 := w.svc.Update(ctx, job)
			if err2 != nil {
				log.Printf("failed to update job status: %v", err2)
			}

			return err
		}

		cancel()
	}

	mate.Close()

	job.Status = web.StatusOK

	if err := w.svc.Update(ctx, job); err != nil {
		return err
	}

	// Auto-import leads from completed job via queue
	if _, err := w.svc.EnqueueLeadImport(ctx, job.ID); err != nil {
		log.Printf("auto-import leads for job %s: failed to enqueue: %v", job.ID, err)
		// Fallback to direct import
		imported, err := w.svc.ImportLeadsFromJob(ctx, job.ID)
		if err != nil {
			log.Printf("auto-import leads for job %s failed: %v", job.ID, err)
		} else if imported > 0 {
			log.Printf("auto-imported %d leads from job %s", imported, job.ID)
		}
	}

	return nil
}

func (w *webrunner) countCSVResults(jobID string) int {
	csvPath := filepath.Join(w.cfg.DataFolder, jobID+".csv")

	file, err := os.Open(csvPath)
	if err != nil {
		return 0
	}
	defer file.Close()

	reader := csv.NewReader(file)
	records, err := reader.ReadAll()
	if err != nil || len(records) < 2 {
		return 0
	}

	return len(records) - 1 // minus header
}

func (w *webrunner) setupMate(ctx context.Context, writer io.Writer, job *web.Job) (*scrapemateapp.ScrapemateApp, error) {
	opts := []func(*scrapemateapp.Config) error{
		scrapemateapp.WithConcurrency(w.cfg.Concurrency),
		scrapemateapp.WithExitOnInactivity(time.Minute * 3),
	}

	if !job.Data.FastMode {
		opts = append(opts,
			scrapemateapp.WithJS(scrapemateapp.DisableImages()),
		)
	} else {
		opts = append(opts,
			scrapemateapp.WithStealth("firefox"),
		)
	}

	hasProxy := false

	if len(w.cfg.Proxies) > 0 {
		// Check health of global proxies before using them
		w.svc.ProxyMonitor.CheckAll(ctx, w.cfg.Proxies)
		healthy := w.svc.ProxyMonitor.GetHealthy()

		excluded := len(w.cfg.Proxies) - len(healthy)
		if excluded > 0 {
			log.Printf("job %s: excluded %d dead proxies out of %d global proxies", job.ID, excluded, len(w.cfg.Proxies))
		}

		if len(healthy) > 0 {
			opts = append(opts, scrapemateapp.WithProxies(healthy))
			hasProxy = true
		} else {
			log.Printf("job %s: all global proxies are dead, running without proxies", job.ID)
		}
	} else if len(job.Data.Proxies) > 0 {
		// Check health of job-specific proxies before using them
		w.svc.ProxyMonitor.CheckAll(ctx, job.Data.Proxies)
		healthy := w.svc.ProxyMonitor.GetHealthy()

		excluded := len(job.Data.Proxies) - len(healthy)
		if excluded > 0 {
			log.Printf("job %s: excluded %d dead proxies out of %d job proxies", job.ID, excluded, len(job.Data.Proxies))
		}

		if len(healthy) > 0 {
			opts = append(opts, scrapemateapp.WithProxies(healthy))
			hasProxy = true
		} else {
			log.Printf("job %s: all job proxies are dead, running without proxies", job.ID)
		}
	}

	if !w.cfg.DisablePageReuse {
		opts = append(opts,
			scrapemateapp.WithPageReuseLimit(2),
			scrapemateapp.WithPageReuseLimit(200),
		)
	}

	log.Printf("job %s has proxy: %v", job.ID, hasProxy)

	csvWriter := csvwriter.NewCsvWriter(csv.NewWriter(writer))

	writers := []scrapemate.ResultWriter{csvWriter}

	matecfg, err := scrapemateapp.NewConfig(
		writers,
		opts...,
	)
	if err != nil {
		return nil, err
	}

	return scrapemateapp.NewScrapeMateApp(matecfg)
}

// scrapeWebJob handles website scraper jobs: searches Google, visits each result, extracts contacts.
func (w *webrunner) scrapeWebJob(ctx context.Context, job *web.Job) error {
	outpath := filepath.Join(w.cfg.DataFolder, job.ID+".csv")

	outfile, err := os.Create(outpath)
	if err != nil {
		return err
	}

	csvW := csv.NewWriter(outfile)

	// Use custom writers: one for DB and one for CSV (the scrapemate csvwriter doesn't handle WebResult)
	dbWriter := &webScraperDBWriter{svc: w.svc, jobID: job.ID}
	wsCsvWriter := &webScraperCsvWriter{w: csvW}
	writers := []scrapemate.ResultWriter{wsCsvWriter, dbWriter}

	opts := []func(*scrapemateapp.Config) error{
		scrapemateapp.WithConcurrency(w.cfg.Concurrency),
		scrapemateapp.WithExitOnInactivity(time.Minute * 2),
		scrapemateapp.WithJS(scrapemateapp.DisableImages()),
	}

	if len(w.cfg.Proxies) > 0 {
		w.svc.ProxyMonitor.CheckAll(ctx, w.cfg.Proxies)
		healthy := w.svc.ProxyMonitor.GetHealthy()
		if len(healthy) > 0 {
			opts = append(opts, scrapemateapp.WithProxies(healthy))
		}
	} else if len(job.Data.Proxies) > 0 {
		w.svc.ProxyMonitor.CheckAll(ctx, job.Data.Proxies)
		healthy := w.svc.ProxyMonitor.GetHealthy()
		if len(healthy) > 0 {
			opts = append(opts, scrapemateapp.WithProxies(healthy))
		}
	}

	matecfg, err := scrapemateapp.NewConfig(writers, opts...)
	if err != nil {
		outfile.Close()
		job.Status = web.StatusFailed
		_ = w.svc.Update(ctx, job)
		return err
	}

	mate, err := scrapemateapp.NewScrapeMateApp(matecfg)
	if err != nil {
		outfile.Close()
		job.Status = web.StatusFailed
		_ = w.svc.Update(ctx, job)
		return err
	}

	// Create seed jobs for each keyword (Google search query or direct URL)
	lang := job.Data.Lang
	if lang == "" {
		lang = "en"
	}

	maxResults := job.Data.MaxResults
	if maxResults <= 0 {
		maxResults = 10
	}

	var seedJobs []scrapemate.IJob
	for _, kw := range job.Data.Keywords {
		kw = strings.TrimSpace(kw)
		if kw == "" {
			continue
		}

		// If keyword looks like a URL, scrape it directly
		if strings.HasPrefix(kw, "http://") || strings.HasPrefix(kw, "https://") {
			seedJobs = append(seedJobs, webscraper.NewWebScrapeJob("", kw, "", "direct"))
		} else {
			seedJobs = append(seedJobs, webscraper.NewGoogleSearchJob(kw, lang, maxResults))
		}
	}

	if len(seedJobs) == 0 {
		mate.Close()
		outfile.Close()
		job.Status = web.StatusFailed
		return w.svc.Update(ctx, job)
	}

	allowedSeconds := 180
	if job.Data.MaxTime > 0 {
		if int(job.Data.MaxTime.Seconds()) > allowedSeconds {
			allowedSeconds = int(job.Data.MaxTime.Seconds())
		}
	}

	log.Printf("running webscraper job %s with %d seed jobs (%d direct URLs) and %d allowed seconds",
		job.ID, len(seedJobs), countDirectURLs(job.Data.Keywords), allowedSeconds)

	mateCtx, cancel := context.WithTimeout(ctx, time.Duration(allowedSeconds)*time.Second)
	defer cancel()

	w.svc.RegisterJobCancel(job.ID, cancel)
	defer w.svc.UnregisterJobCancel(job.ID)

	err = mate.Start(mateCtx, seedJobs...)
	if err != nil && !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
		cancel()
		mate.Close()
		csvW.Flush()
		outfile.Close()
		job.Status = web.StatusFailed
		_ = w.svc.Update(ctx, job)
		return err
	}

	cancel()
	mate.Close()
	csvW.Flush()
	outfile.Close()

	job.Status = web.StatusOK
	return w.svc.Update(ctx, job)
}

func countDirectURLs(keywords []string) int {
	count := 0
	for _, kw := range keywords {
		kw = strings.TrimSpace(kw)
		if strings.HasPrefix(kw, "http://") || strings.HasPrefix(kw, "https://") {
			count++
		}
	}
	return count
}

// countFicheJob scrolls a Google Maps search to the end and counts listings.
func (w *webrunner) countFicheJob(ctx context.Context, job *web.Job) error {
	if len(job.Data.Keywords) == 0 {
		job.Status = web.StatusFailed
		return w.svc.Update(ctx, job)
	}

	keyword := job.Data.Keywords[0]

	var coords string
	if job.Data.Lat != "" && job.Data.Lon != "" && job.Data.Lat != "0" && job.Data.Lon != "0" {
		coords = job.Data.Lat + "," + job.Data.Lon
	}

	lang := job.Data.Lang
	if lang == "" {
		lang = "fr"
	}

	zoom := job.Data.Zoom
	if zoom == 0 {
		zoom = 15
	}

	countJob := gmaps.NewCountFicheJob("", lang, keyword, coords, zoom)

	// Set up scrapemate with a no-op writer (we don't need CSV output)
	opts := []func(*scrapemateapp.Config) error{
		scrapemateapp.WithConcurrency(1),
		scrapemateapp.WithExitOnInactivity(time.Minute * 3),
	}

	if job.Data.Live {
		// Live mode: open visible browser window so user can watch
		// Requires full chromium (not headless shell) + DISPLAY env set
		opts = append(opts, scrapemateapp.WithJS(scrapemateapp.Headfull()))
		log.Printf("countfiche job %s: live mode enabled (headfull browser)", job.ID)
	} else {
		opts = append(opts, scrapemateapp.WithJS(scrapemateapp.DisableImages()))
	}

	if len(w.cfg.Proxies) > 0 {
		w.svc.ProxyMonitor.CheckAll(ctx, w.cfg.Proxies)
		healthy := w.svc.ProxyMonitor.GetHealthy()
		if len(healthy) > 0 {
			opts = append(opts, scrapemateapp.WithProxies(healthy))
		}
	} else if len(job.Data.Proxies) > 0 {
		w.svc.ProxyMonitor.CheckAll(ctx, job.Data.Proxies)
		healthy := w.svc.ProxyMonitor.GetHealthy()
		if len(healthy) > 0 {
			opts = append(opts, scrapemateapp.WithProxies(healthy))
		}
	}

	noopWriter := &countFicheWriter{}
	matecfg, err := scrapemateapp.NewConfig(
		[]scrapemate.ResultWriter{noopWriter},
		opts...,
	)
	if err != nil {
		job.Status = web.StatusFailed
		_ = w.svc.Update(ctx, job)
		return err
	}

	mate, err := scrapemateapp.NewScrapeMateApp(matecfg)
	if err != nil {
		job.Status = web.StatusFailed
		_ = w.svc.Update(ctx, job)
		return err
	}
	defer mate.Close()

	allowedSeconds := 600 // 10 minutes max for counting
	if job.Data.MaxTime > 0 && int(job.Data.MaxTime.Seconds()) > allowedSeconds {
		allowedSeconds = int(job.Data.MaxTime.Seconds())
	}

	log.Printf("running countfiche job %s for keyword %q with %d allowed seconds", job.ID, keyword, allowedSeconds)

	// For live mode: swap headless shell with full chromium AFTER playwright.Install()
	// has run (inside NewScrapeMateApp), so it doesn't get deleted as "unused"
	if job.Data.Live {
		chromiumPaths := []string{
			"/opt/browsers/chromium-1200/chrome-linux64/chrome",
			"/opt/browsers/chromium-1169/chrome-linux/chrome",
		}
		var chromiumPath string
		for _, p := range chromiumPaths {
			if _, err := os.Stat(p); err == nil {
				chromiumPath = p
				break
			}
		}

		headlessShellGlob, _ := filepath.Glob("/opt/browsers/chromium_headless_shell-*/chrome-headless-shell-linux*/chrome-headless-shell")
		if chromiumPath != "" && len(headlessShellGlob) > 0 {
			origBin := headlessShellGlob[0]
			backupBin := origBin + ".bak"
			if _, err := os.Stat(backupBin); os.IsNotExist(err) {
				if err := os.Rename(origBin, backupBin); err == nil {
					if err := os.Symlink(chromiumPath, origBin); err == nil {
						log.Printf("countfiche job %s: swapped headless shell with full chromium (%s)", job.ID, chromiumPath)
						defer func() {
							_ = os.Remove(origBin)
							_ = os.Rename(backupBin, origBin)
							log.Printf("countfiche job %s: restored headless shell binary", job.ID)
						}()
					}
				}
			}
		} else {
			log.Printf("countfiche job %s: WARNING - full chromium not found at known paths, live mode may not work", job.ID)
		}
	}

	mateCtx, cancel := context.WithTimeout(ctx, time.Duration(allowedSeconds)*time.Second)
	defer cancel()

	w.svc.RegisterJobCancel(job.ID, cancel)
	defer w.svc.UnregisterJobCancel(job.ID)

	err = mate.Start(mateCtx, countJob)
	if err != nil && !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
		cancel()
		job.Status = web.StatusFailed
		_ = w.svc.Update(ctx, job)
		return err
	}

	cancel()
	mate.Close()

	// Write count to file
	countPath := w.svc.GetCountFilePath(job.ID)
	countStr := fmt.Sprintf("%d", countJob.Count)
	if err := os.WriteFile(countPath, []byte(countStr), 0644); err != nil {
		log.Printf("failed to write count file for job %s: %v", job.ID, err)
	}

	log.Printf("countfiche job %s completed: %d fiches found for %q", job.ID, countJob.Count, keyword)

	job.Status = web.StatusOK
	return w.svc.Update(ctx, job)
}

// countFicheWriter is a no-op writer for count fiche jobs.
type countFicheWriter struct{}

func (w *countFicheWriter) Run(_ context.Context, in <-chan scrapemate.Result) error {
	for range in {
		// discard
	}
	return nil
}

// webScraperCsvWriter writes webscraper results to a CSV file.
type webScraperCsvWriter struct {
	w           *csv.Writer
	headersDone bool
}

func (w *webScraperCsvWriter) Run(_ context.Context, in <-chan scrapemate.Result) error {
	for result := range in {
		wr, ok := result.Data.(*webscraper.WebResult)
		if !ok {
			continue
		}

		if !w.headersDone {
			if err := w.w.Write(wr.CsvHeaders()); err != nil {
				log.Printf("webscraper csv header error: %v", err)
			}
			w.headersDone = true
		}

		if err := w.w.Write(wr.CsvRow()); err != nil {
			log.Printf("webscraper csv write error: %v", err)
		}

		w.w.Flush()
	}

	return nil
}

// webScraperDBWriter writes webscraper results to the database.
type webScraperDBWriter struct {
	svc   *web.Service
	jobID string
}

func (w *webScraperDBWriter) Run(ctx context.Context, in <-chan scrapemate.Result) error {
	for result := range in {
		wr, ok := result.Data.(*webscraper.WebResult)
		if !ok {
			continue
		}

		r := web.WebScraperResult{
			ID:          uuid.New().String(),
			JobID:       w.jobID,
			URL:         wr.URL,
			Title:       wr.Title,
			Description: wr.Description,
			Emails:      wr.Emails,
			Phones:      wr.Phones,
			Socials:     wr.Socials,
			SearchQuery: wr.SearchQuery,
			Error:       wr.Error,
		}

		if err := w.svc.CreateWebScraperResult(ctx, &r); err != nil {
			log.Printf("webscraper db write error: %v", err)
		}
	}

	return nil
}
