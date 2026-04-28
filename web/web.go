package web

import (
	"context"
	"embed"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

//go:embed static
var static embed.FS

type Server struct {
	tmpl map[string]*template.Template
	srv  *http.Server
	svc  *Service
}

func New(svc *Service, addr string) (*Server, error) {
	ans := Server{
		svc:  svc,
		tmpl: make(map[string]*template.Template),
		srv: &http.Server{
			Addr:              addr,
			ReadHeaderTimeout: 10 * time.Second,
			ReadTimeout:       60 * time.Second,
			WriteTimeout:      60 * time.Second,
			IdleTimeout:       120 * time.Second,
			MaxHeaderBytes:    1 << 20,
		},
	}

	staticFS, err := fs.Sub(static, "static")
	if err != nil {
		return nil, err
	}

	fileServer := http.FileServer(http.FS(staticFS))
	mux := http.NewServeMux()

	mux.Handle("/static/", http.StripPrefix("/static/", fileServer))
	mux.HandleFunc("/scrape", ans.scrape)
	mux.HandleFunc("/download", func(w http.ResponseWriter, r *http.Request) {
		r = requestWithID(r)

		ans.download(w, r)
	})
	mux.HandleFunc("/delete", func(w http.ResponseWriter, r *http.Request) {
		r = requestWithID(r)

		ans.delete(w, r)
	})
	mux.HandleFunc("/jobs", ans.getJobs)
	mux.HandleFunc("/results-preview", func(w http.ResponseWriter, r *http.Request) {
		r = requestWithID(r)
		ans.resultsPreview(w, r)
	})
	mux.HandleFunc("/results", func(w http.ResponseWriter, r *http.Request) {
		r = requestWithID(r)
		ans.resultsPage(w, r)
	})
	mux.HandleFunc("/job-progress", func(w http.ResponseWriter, r *http.Request) {
		r = requestWithID(r)
		ans.jobProgress(w, r)
	})
	mux.HandleFunc("/", ans.index)

	// Dashboard page
	mux.HandleFunc("/dashboard", ans.dashboardPage)

	// Proxy Monitor
	mux.HandleFunc("/proxies", ans.proxiesPage)
	mux.HandleFunc("/proxies/check", ans.proxiesCheck)

	// Schedules
	mux.HandleFunc("/schedules", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			ans.schedulesPage(w, r)
		case http.MethodPost:
			ans.createSchedule(w, r)
		default:
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	})
	mux.HandleFunc("/schedules/{id}/delete", func(w http.ResponseWriter, r *http.Request) {
		r = requestWithID(r)
		ans.deleteSchedule(w, r)
	})
	mux.HandleFunc("/schedules/{id}/toggle", func(w http.ResponseWriter, r *http.Request) {
		r = requestWithID(r)
		ans.toggleSchedule(w, r)
	})

	// Leads CRM
	mux.HandleFunc("/leads", ans.leadsPage)
	mux.HandleFunc("/leads/import", ans.leadsImport)
	mux.HandleFunc("/leads/{id}/tags", ans.leadsUpdateTags)
	mux.HandleFunc("/leads/{id}/notes", ans.leadsUpdateNotes)
	mux.HandleFunc("/leads/{id}/status", ans.leadsUpdateStatus)
	mux.HandleFunc("/leads/{id}/delete", func(w http.ResponseWriter, r *http.Request) {
		r = requestWithID(r)
		ans.leadsDelete(w, r)
	})

	// Count Fiche
	mux.HandleFunc("/countfiche", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			ans.countFichePage(w, r)
		case http.MethodPost:
			ans.countFicheCreate(w, r)
		default:
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	})
	mux.HandleFunc("/countfiche/status", func(w http.ResponseWriter, r *http.Request) {
		r = requestWithID(r)
		ans.countFicheStatus(w, r)
	})
	mux.HandleFunc("/countfiche/history", ans.countFicheHistory)
	mux.HandleFunc("/countfiche/delete", func(w http.ResponseWriter, r *http.Request) {
		r = requestWithID(r)
		ans.countFicheDelete(w, r)
	})

	// Website Scraper
	mux.HandleFunc("/webscraper", ans.webScraperPage)
	mux.HandleFunc("/webscraper/scrape", ans.webScraperScrape)
	mux.HandleFunc("/webscraper/results", ans.webScraperResults)
	mux.HandleFunc("/webscraper/jobs", ans.webScraperJobs)
	mux.HandleFunc("/webscraper/download", ans.webScraperDownload)

	// Queue
	mux.HandleFunc("/queue", ans.queuePage)
	mux.HandleFunc("/queue/{id}/delete", func(w http.ResponseWriter, r *http.Request) {
		r = requestWithID(r)
		ans.queueDelete(w, r)
	})

	// WhatsApp page (UI only — API calls go to Node.js bridge at port 3001)
	mux.HandleFunc("/whatsapp", ans.whatsappPage)
	// Same template as /whatsapp, but JS detects this path and preselects the
	// Desktop auto-sender mode (no QR / bridge connection required).
	mux.HandleFunc("/whatsapp-desktop", ans.whatsappPage)

	// WhatsApp Desktop auto-sender (drives local WhatsApp Desktop via
	// whatsapp:// URI + OS-level Enter keypress; no QR/bridge required).
	mux.HandleFunc("/whatsapp-auto/start", ans.whatsappAutoStart)
	mux.HandleFunc("/whatsapp-auto/status", ans.whatsappAutoStatus)
	mux.HandleFunc("/whatsapp-auto/pause", ans.whatsappAutoPause)
	mux.HandleFunc("/whatsapp-auto/resume", ans.whatsappAutoResume)
	mux.HandleFunc("/whatsapp-auto/stop", ans.whatsappAutoStop)
	mux.HandleFunc("/whatsapp-auto/campaigns", ans.whatsappAutoCampaigns)
	mux.HandleFunc("/whatsapp-auto/resume-campaign", ans.whatsappAutoResumeCampaign)
	mux.HandleFunc("/whatsapp-auto/contacted-phones", ans.whatsappAutoContactedPhones)
	mux.HandleFunc("/whatsapp-auto/delete-campaign", ans.whatsappAutoDeleteCampaign)

	// Email page (UI only — API calls go to Node.js bridge at port 3002)
	mux.HandleFunc("/email", ans.emailPage)

	// api routes
	mux.HandleFunc("/api/docs", ans.redocHandler)
	mux.HandleFunc("/api/v1/jobs", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			ans.apiScrape(w, r)
		case http.MethodGet:
			ans.apiGetJobs(w, r)
		default:
			ans := apiError{
				Code:    http.StatusMethodNotAllowed,
				Message: "Method not allowed",
			}

			renderJSON(w, http.StatusMethodNotAllowed, ans)
		}
	})

	mux.HandleFunc("/api/v1/dashboard", ans.apiDashboard)

	mux.HandleFunc("/api/v1/jobs/{id}", func(w http.ResponseWriter, r *http.Request) {
		r = requestWithID(r)

		switch r.Method {
		case http.MethodGet:
			ans.apiGetJob(w, r)
		case http.MethodDelete:
			ans.apiDeleteJob(w, r)
		default:
			ans := apiError{
				Code:    http.StatusMethodNotAllowed,
				Message: "Method not allowed",
			}

			renderJSON(w, http.StatusMethodNotAllowed, ans)
		}
	})

	mux.HandleFunc("/api/v1/jobs/{id}/stop", func(w http.ResponseWriter, r *http.Request) {
		r = requestWithID(r)

		if r.Method != http.MethodPost {
			renderJSON(w, http.StatusMethodNotAllowed, apiError{Code: http.StatusMethodNotAllowed, Message: "Method not allowed"})
			return
		}

		ans.apiStopJob(w, r)
	})

	mux.HandleFunc("/api/v1/jobs/{id}/download", func(w http.ResponseWriter, r *http.Request) {
		r = requestWithID(r)

		if r.Method != http.MethodGet {
			ans := apiError{
				Code:    http.StatusMethodNotAllowed,
				Message: "Method not allowed",
			}

			renderJSON(w, http.StatusMethodNotAllowed, ans)

			return
		}

		ans.download(w, r)
	})

	mux.HandleFunc("/api/v1/proxies/status", ans.apiProxyStatus)

	mux.HandleFunc("/api/v1/leads", ans.apiGetLeads)
	mux.HandleFunc("/api/v1/leads/stats", ans.apiGetLeadStats)

	mux.HandleFunc("/api/v1/schedules", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			renderJSON(w, http.StatusMethodNotAllowed, apiError{
				Code:    http.StatusMethodNotAllowed,
				Message: "Method not allowed",
			})
			return
		}
		ans.apiGetSchedules(w, r)
	})

	// Webhooks
	mux.HandleFunc("/webhooks", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			ans.webhooksPage(w, r)
		case http.MethodPost:
			ans.createWebhook(w, r)
		default:
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	})
	mux.HandleFunc("/webhooks/{id}/delete", func(w http.ResponseWriter, r *http.Request) {
		r = requestWithID(r)
		ans.deleteWebhook(w, r)
	})
	mux.HandleFunc("/webhooks/{id}/toggle", func(w http.ResponseWriter, r *http.Request) {
		r = requestWithID(r)
		ans.toggleWebhook(w, r)
	})
	mux.HandleFunc("/webhooks/{id}/test", func(w http.ResponseWriter, r *http.Request) {
		r = requestWithID(r)
		ans.testWebhook(w, r)
	})

	mux.HandleFunc("/api/v1/queue", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			renderJSON(w, http.StatusMethodNotAllowed, apiError{
				Code:    http.StatusMethodNotAllowed,
				Message: "Method not allowed",
			})
			return
		}
		ans.apiGetQueue(w, r)
	})
	mux.HandleFunc("/api/v1/queue/stats", ans.apiGetQueueStats)

	mux.HandleFunc("/api/v1/webhooks", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			renderJSON(w, http.StatusMethodNotAllowed, apiError{
				Code:    http.StatusMethodNotAllowed,
				Message: "Method not allowed",
			})
			return
		}
		ans.apiGetWebhooks(w, r)
	})

	handler := securityHeaders(mux)
	ans.srv.Handler = handler

	tmplsKeys := []string{
		"static/templates/index.html",
		"static/templates/job_rows.html",
		"static/templates/job_row.html",
		"static/templates/redoc.html",
		"static/templates/whatsapp.html",
		"static/templates/dashboard.html",
		"static/templates/proxies.html",
		"static/templates/schedules.html",
		"static/templates/leads.html",
		"static/templates/webhooks.html",
		"static/templates/email.html",
		"static/templates/webscraper.html",
		"static/templates/countfiche.html",
		"static/templates/queue.html",
		"static/templates/results.html",
	}

	funcMap := template.FuncMap{
		"split": func(s, sep string) []string {
			return strings.Split(s, sep)
		},
		"truncate": func(s string, n int) string {
			if len(s) <= n {
				return s
			}
			return s[:n] + "..."
		},
		"add": func(a, b int) int {
			return a + b
		},
		"subtract": func(a, b int) int {
			return a - b
		},
		"trimSpace": strings.TrimSpace,
		"formatDate": func(t time.Time) string {
			return t.Format("Jan 02, 2006 15:04:05")
		},
	}

	for _, key := range tmplsKeys {
		tmp, err := template.New(filepath.Base(key)).Funcs(funcMap).ParseFS(static, key)
		if err != nil {
			return nil, err
		}

		ans.tmpl[key] = tmp
	}

	return &ans, nil
}



func (s *Server) Start(ctx context.Context) error {
	go func() {
		<-ctx.Done()

		err := s.srv.Shutdown(context.Background())
		if err != nil {
			log.Println(err)

			return
		}

		log.Println("server stopped")
	}()

	fmt.Fprintf(os.Stderr, "visit http://localhost%s\n", s.srv.Addr)

	err := s.srv.ListenAndServe()
	if err != nil && err != http.ErrServerClosed {
		return err
	}

	return nil
}

type formData struct {
	Name       string
	MaxTime    string
	Keywords   []string
	Language   string
	Zoom       int
	FastMode   bool
	Radius     int
	Lat        string
	Lon        string
	Depth      int
	Email      bool
	Proxies    []string
	Country    string
	MaxResults int
}

type ctxKey string

const idCtxKey ctxKey = "id"

func requestWithID(r *http.Request) *http.Request {
	id := r.PathValue("id")
	if id == "" {
		id = r.URL.Query().Get("id")
	}

	parsed, err := uuid.Parse(id)
	if err == nil {
		r = r.WithContext(context.WithValue(r.Context(), idCtxKey, parsed))
	}

	return r
}

func getIDFromRequest(r *http.Request) (uuid.UUID, bool) {
	id, ok := r.Context().Value(idCtxKey).(uuid.UUID)

	return id, ok
}

//nolint:gocritic // this is used in template
func (f formData) ProxiesString() string {
	return strings.Join(f.Proxies, "\n")
}

//nolint:gocritic // this is used in template
func (f formData) KeywordsString() string {
	return strings.Join(f.Keywords, "\n")
}

func (s *Server) index(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)

		return
	}

	tmpl, ok := s.tmpl["static/templates/index.html"]
	if !ok {
		http.Error(w, "missing tpl", http.StatusInternalServerError)

		return
	}

	data := formData{
		Name:       "",
		MaxTime:    "5m",
		Keywords:   []string{},
		Language:   "fr",
		Zoom:       15,
		FastMode:   false,
		Radius:     10000,
		Lat:        "0",
		Lon:        "0",
		Depth:      3,
		Email:      true,
		Country:    "Morocco",
		MaxResults: 50,
	}

	_ = tmpl.Execute(w, data)
}

func (s *Server) scrape(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)

		return
	}

	err := r.ParseForm()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)

		return
	}

	newJob := Job{
		ID:     uuid.New().String(),
		Name:   r.Form.Get("name"),
		Date:   time.Now().UTC(),
		Status: StatusPending,
		Data:   JobData{},
	}

	maxTimeStr := r.Form.Get("maxtime")

	maxTime, err := time.ParseDuration(maxTimeStr)
	if err != nil {
		http.Error(w, "invalid max time", http.StatusUnprocessableEntity)

		return
	}

	if maxTime < time.Minute*3 {
		http.Error(w, "max time must be more than 3m", http.StatusUnprocessableEntity)

		return
	}

	newJob.Data.MaxTime = maxTime

	keywordsStr, ok := r.Form["keywords"]
	if !ok {
		http.Error(w, "missing keywords", http.StatusUnprocessableEntity)

		return
	}

	keywords := strings.Split(keywordsStr[0], "\n")
	for _, k := range keywords {
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}

		newJob.Data.Keywords = append(newJob.Data.Keywords, k)
	}

	newJob.Data.Lang = r.Form.Get("lang")

	newJob.Data.Zoom, err = strconv.Atoi(r.Form.Get("zoom"))
	if err != nil {
		http.Error(w, "invalid zoom", http.StatusUnprocessableEntity)

		return
	}

	if r.Form.Get("fastmode") == "on" {
		newJob.Data.FastMode = true
	}

	newJob.Data.Radius, err = strconv.Atoi(r.Form.Get("radius"))
	if err != nil {
		http.Error(w, "invalid radius", http.StatusUnprocessableEntity)

		return
	}

	newJob.Data.Lat = r.Form.Get("latitude")
	newJob.Data.Lon = r.Form.Get("longitude")

	newJob.Data.Depth, err = strconv.Atoi(r.Form.Get("depth"))
	if err != nil {
		http.Error(w, "invalid depth", http.StatusUnprocessableEntity)

		return
	}

	newJob.Data.Email = r.Form.Get("email") == "on"

	newJob.Data.Country = r.Form.Get("country")
	if newJob.Data.Country == "" {
		newJob.Data.Country = "Morocco"
	}

	maxResultsStr := r.Form.Get("maxresults")
	if maxResultsStr != "" {
		newJob.Data.MaxResults, err = strconv.Atoi(maxResultsStr)
		if err != nil {
			http.Error(w, "invalid max results", http.StatusUnprocessableEntity)
			return
		}
	}

	proxies := strings.Split(r.Form.Get("proxies"), "\n")
	if len(proxies) > 0 {
		for _, p := range proxies {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}

			newJob.Data.Proxies = append(newJob.Data.Proxies, p)
		}
	}

	err = newJob.Validate()
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)

		return
	}

	// Enqueue through the queue system if available, otherwise create directly.
	if s.svc.queueRepo != nil {
		_, err = s.svc.EnqueueJob(r.Context(), &newJob)
	} else {
		err = s.svc.Create(r.Context(), &newJob)
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)

		return
	}

	tmpl, ok := s.tmpl["static/templates/job_row.html"]
	if !ok {
		http.Error(w, "missing tpl", http.StatusInternalServerError)

		return
	}

	_ = tmpl.Execute(w, newJob)
}

func (s *Server) getJobs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)

		return
	}

	tmpl, ok := s.tmpl["static/templates/job_rows.html"]
	if !ok {
		http.Error(w, "missing tpl", http.StatusInternalServerError)
		return
	}

	jobs, err := s.svc.All(context.Background())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)

		return
	}

	_ = tmpl.Execute(w, jobs)
}

func (s *Server) download(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)

		return
	}

	ctx := r.Context()

	id, ok := getIDFromRequest(r)
	if !ok {
		http.Error(w, "Invalid ID", http.StatusUnprocessableEntity)

		return
	}

	filePath, err := s.svc.GetCSV(ctx, id.String())
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	file, err := os.Open(filePath)
	if err != nil {
		http.Error(w, "Failed to open file", http.StatusInternalServerError)
		return
	}
	defer file.Close()

	fileName := filepath.Base(filePath)
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", fileName))
	w.Header().Set("Content-Type", "text/csv")

	_, err = io.Copy(w, file)
	if err != nil {
		http.Error(w, "Failed to send file", http.StatusInternalServerError)
		return
	}
}

func (s *Server) delete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)

		return
	}

	deleteID, ok := getIDFromRequest(r)
	if !ok {
		http.Error(w, "Invalid ID", http.StatusUnprocessableEntity)

		return
	}

	err := s.svc.Delete(r.Context(), deleteID.String())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)

		return
	}

	w.WriteHeader(http.StatusOK)
}

type apiError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type apiScrapeRequest struct {
	Name string
	JobData
}

type apiScrapeResponse struct {
	ID string `json:"id"`
}

func (s *Server) redocHandler(w http.ResponseWriter, _ *http.Request) {
	tmpl, ok := s.tmpl["static/templates/redoc.html"]
	if !ok {
		http.Error(w, "missing tpl", http.StatusInternalServerError)

		return
	}

	_ = tmpl.Execute(w, nil)
}

func (s *Server) apiScrape(w http.ResponseWriter, r *http.Request) {
	var req apiScrapeRequest

	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		ans := apiError{
			Code:    http.StatusUnprocessableEntity,
			Message: err.Error(),
		}

		renderJSON(w, http.StatusUnprocessableEntity, ans)

		return
	}

	newJob := Job{
		ID:     uuid.New().String(),
		Name:   req.Name,
		Date:   time.Now().UTC(),
		Status: StatusPending,
		Data:   req.JobData,
	}

	// convert to seconds
	newJob.Data.MaxTime *= time.Second

	err = newJob.Validate()
	if err != nil {
		ans := apiError{
			Code:    http.StatusUnprocessableEntity,
			Message: err.Error(),
		}

		renderJSON(w, http.StatusUnprocessableEntity, ans)

		return
	}

	if s.svc.queueRepo != nil {
		_, err = s.svc.EnqueueJob(r.Context(), &newJob)
	} else {
		err = s.svc.Create(r.Context(), &newJob)
	}
	if err != nil {
		ans := apiError{
			Code:    http.StatusInternalServerError,
			Message: err.Error(),
		}

		renderJSON(w, http.StatusInternalServerError, ans)

		return
	}

	ans := apiScrapeResponse{
		ID: newJob.ID,
	}

	renderJSON(w, http.StatusCreated, ans)
}

func (s *Server) apiGetJobs(w http.ResponseWriter, r *http.Request) {
	jobs, err := s.svc.All(r.Context())
	if err != nil {
		apiError := apiError{
			Code:    http.StatusInternalServerError,
			Message: err.Error(),
		}

		renderJSON(w, http.StatusInternalServerError, apiError)

		return
	}

	renderJSON(w, http.StatusOK, jobs)
}

func (s *Server) apiGetJob(w http.ResponseWriter, r *http.Request) {
	id, ok := getIDFromRequest(r)
	if !ok {
		apiError := apiError{
			Code:    http.StatusUnprocessableEntity,
			Message: "Invalid ID",
		}

		renderJSON(w, http.StatusUnprocessableEntity, apiError)

		return
	}

	job, err := s.svc.Get(r.Context(), id.String())
	if err != nil {
		apiError := apiError{
			Code:    http.StatusNotFound,
			Message: http.StatusText(http.StatusNotFound),
		}

		renderJSON(w, http.StatusNotFound, apiError)

		return
	}

	renderJSON(w, http.StatusOK, job)
}

func (s *Server) apiDeleteJob(w http.ResponseWriter, r *http.Request) {
	id, ok := getIDFromRequest(r)
	if !ok {
		apiError := apiError{
			Code:    http.StatusUnprocessableEntity,
			Message: "Invalid ID",
		}

		renderJSON(w, http.StatusUnprocessableEntity, apiError)

		return
	}

	err := s.svc.Delete(r.Context(), id.String())
	if err != nil {
		apiError := apiError{
			Code:    http.StatusInternalServerError,
			Message: err.Error(),
		}

		renderJSON(w, http.StatusInternalServerError, apiError)

		return
	}

	w.WriteHeader(http.StatusOK)
}

func (s *Server) apiStopJob(w http.ResponseWriter, r *http.Request) {
	id, ok := getIDFromRequest(r)
	if !ok {
		renderJSON(w, http.StatusUnprocessableEntity, apiError{Code: http.StatusUnprocessableEntity, Message: "Invalid ID"})
		return
	}

	jobID := id.String()

	if err := s.svc.StopJob(r.Context(), jobID); err != nil {
		renderJSON(w, http.StatusInternalServerError, apiError{Code: http.StatusInternalServerError, Message: err.Error()})
		return
	}

	renderJSON(w, http.StatusOK, map[string]any{"stopped": true, "id": jobID})
}

// --- Website Scraper handlers ---

type webScraperPageData struct {
	Jobs    []Job
	Results []WebScraperResult
	Stats   struct {
		Total     int
		WithEmail int
		WithPhone int
	}
	Filter  string
	Search  string
	JobID   string
}

func (s *Server) webScraperPage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	tmpl, ok := s.tmpl["static/templates/webscraper.html"]
	if !ok {
		http.Error(w, "missing tpl", http.StatusInternalServerError)
		return
	}

	data := webScraperPageData{}

	// Get all webscraper jobs
	allJobs, err := s.svc.All(r.Context())
	if err == nil {
		for _, j := range allJobs {
			if j.Data.JobType == JobTypeWebScraper {
				data.Jobs = append(data.Jobs, j)
			}
		}
	}

	// Get filter params
	data.Filter = r.URL.Query().Get("filter")
	data.Search = r.URL.Query().Get("search")
	data.JobID = r.URL.Query().Get("job_id")

	params := WebScraperSelectParams{
		JobID:  data.JobID,
		Search: data.Search,
		Limit:  200,
	}

	if data.Filter == "has-email" {
		params.HasEmail = true
	} else if data.Filter == "has-phone" {
		params.HasPhone = true
	}

	results, err := s.svc.SelectWebScraperResults(r.Context(), params)
	if err == nil {
		data.Results = results
	}

	total, withEmail, withPhone, err := s.svc.CountWebScraperResults(r.Context(), data.JobID)
	if err == nil {
		data.Stats.Total = total
		data.Stats.WithEmail = withEmail
		data.Stats.WithPhone = withPhone
	}

	_ = tmpl.Execute(w, data)
}

func (s *Server) webScraperScrape(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	err := r.ParseForm()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	name := r.Form.Get("name")
	if name == "" {
		name = "Website Scrape"
	}

	keywordsStr := r.Form.Get("keywords")
	if keywordsStr == "" {
		http.Error(w, "missing search queries", http.StatusUnprocessableEntity)
		return
	}

	var keywords []string
	for _, k := range strings.Split(keywordsStr, "\n") {
		k = strings.TrimSpace(k)
		if k != "" {
			keywords = append(keywords, k)
		}
	}

	if len(keywords) == 0 {
		http.Error(w, "missing search queries", http.StatusUnprocessableEntity)
		return
	}

	maxTimeStr := r.Form.Get("maxtime")
	if maxTimeStr == "" {
		maxTimeStr = "5m"
	}

	maxTime, err := time.ParseDuration(maxTimeStr)
	if err != nil || maxTime < 3*time.Minute {
		maxTime = 5 * time.Minute
	}

	maxResults := 10
	if v := r.Form.Get("maxresults"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			maxResults = n
		}
	}

	lang := r.Form.Get("lang")
	if lang == "" {
		lang = "en"
	}

	newJob := Job{
		ID:     uuid.New().String(),
		Name:   name,
		Date:   time.Now().UTC(),
		Status: StatusPending,
		Data: JobData{
			JobType:    JobTypeWebScraper,
			Keywords:   keywords,
			Lang:       lang,
			MaxTime:    maxTime,
			MaxResults: maxResults,
		},
	}

	// Parse proxies
	if proxiesStr := r.Form.Get("proxies"); proxiesStr != "" {
		for _, p := range strings.Split(proxiesStr, "\n") {
			p = strings.TrimSpace(p)
			if p != "" {
				newJob.Data.Proxies = append(newJob.Data.Proxies, p)
			}
		}
	}

	err = newJob.Validate()
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}

	if s.svc.queueRepo != nil {
		_, err = s.svc.EnqueueJob(r.Context(), &newJob)
	} else {
		err = s.svc.Create(r.Context(), &newJob)
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("HX-Redirect", "/webscraper")
	w.WriteHeader(http.StatusCreated)
}

func (s *Server) webScraperResults(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	jobID := r.URL.Query().Get("job_id")
	filter := r.URL.Query().Get("filter")
	search := r.URL.Query().Get("search")

	params := WebScraperSelectParams{
		JobID:  jobID,
		Search: search,
		Limit:  200,
	}

	if filter == "has-email" {
		params.HasEmail = true
	} else if filter == "has-phone" {
		params.HasPhone = true
	}

	results, err := s.svc.SelectWebScraperResults(r.Context(), params)
	if err != nil {
		renderJSON(w, http.StatusInternalServerError, apiError{Code: 500, Message: err.Error()})
		return
	}

	total, withEmail, withPhone, _ := s.svc.CountWebScraperResults(r.Context(), jobID)

	type response struct {
		Results   []WebScraperResult `json:"results"`
		Total     int                `json:"total"`
		WithEmail int                `json:"with_email"`
		WithPhone int                `json:"with_phone"`
	}

	renderJSON(w, http.StatusOK, response{
		Results:   results,
		Total:     total,
		WithEmail: withEmail,
		WithPhone: withPhone,
	})
}

func (s *Server) webScraperJobs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	allJobs, err := s.svc.All(r.Context())
	if err != nil {
		renderJSON(w, http.StatusInternalServerError, apiError{Code: 500, Message: err.Error()})
		return
	}

	var wsJobs []Job
	for _, j := range allJobs {
		if j.Data.JobType == JobTypeWebScraper {
			wsJobs = append(wsJobs, j)
		}
	}

	renderJSON(w, http.StatusOK, wsJobs)
}

func (s *Server) webScraperDownload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	jobID := r.URL.Query().Get("job_id")

	params := WebScraperSelectParams{
		JobID: jobID,
		Limit: 10000,
	}

	results, err := s.svc.SelectWebScraperResults(r.Context(), params)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", "attachment; filename=webscraper-results.csv")

	csvW := csv.NewWriter(w)

	// Write headers
	_ = csvW.Write([]string{"url", "title", "description", "emails", "phones", "socials", "search_query"})

	for _, r := range results {
		_ = csvW.Write([]string{r.URL, r.Title, r.Description, r.Emails, r.Phones, r.Socials, r.SearchQuery})
	}

	csvW.Flush()
}

func (s *Server) whatsappPage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	tmpl, ok := s.tmpl["static/templates/whatsapp.html"]
	if !ok {
		http.Error(w, "missing tpl", http.StatusInternalServerError)
		return
	}

	_ = tmpl.Execute(w, nil)
}

func (s *Server) emailPage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	tmpl, ok := s.tmpl["static/templates/email.html"]
	if !ok {
		http.Error(w, "missing tpl", http.StatusInternalServerError)
		return
	}

	_ = tmpl.Execute(w, nil)
}

// === Count Fiche handlers ===

func (s *Server) countFichePage(w http.ResponseWriter, r *http.Request) {
	tmpl, ok := s.tmpl["static/templates/countfiche.html"]
	if !ok {
		http.Error(w, "missing tpl", http.StatusInternalServerError)
		return
	}

	_ = tmpl.Execute(w, nil)
}

func (s *Server) countFicheCreate(w http.ResponseWriter, r *http.Request) {
	err := r.ParseForm()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	keyword := strings.TrimSpace(r.Form.Get("keyword"))
	if keyword == "" {
		http.Error(w, "missing keyword", http.StatusUnprocessableEntity)
		return
	}

	lang := r.Form.Get("lang")
	if lang == "" {
		lang = "fr"
	}

	lat := r.Form.Get("lat")
	lon := r.Form.Get("lon")
	zoom, _ := strconv.Atoi(r.Form.Get("zoom"))
	if zoom == 0 {
		zoom = 15
	}

	// If keyword looks like a Google Maps URL, extract the query from it
	if strings.Contains(keyword, "google.com/maps") {
		if parsed, err := url.Parse(keyword); err == nil {
			// Try ?q= parameter first
			if q := parsed.Query().Get("q"); q != "" {
				keyword = strings.ReplaceAll(q, "+", " ")
			}
		}
	}

	live := r.Form.Get("live") == "on"

	newJob := Job{
		ID:     uuid.New().String(),
		Name:   "Count: " + keyword,
		Date:   time.Now().UTC(),
		Status: StatusPending,
		Data: JobData{
			JobType:  JobTypeCountFiche,
			Keywords: []string{keyword},
			Lang:     lang,
			Lat:      lat,
			Lon:      lon,
			Zoom:     zoom,
			Depth:    200,
			MaxTime:  10 * time.Minute,
			Live:     live,
		},
	}

	if s.svc.queueRepo != nil {
		_, err = s.svc.EnqueueJob(r.Context(), &newJob)
	} else {
		err = s.svc.Create(r.Context(), &newJob)
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	renderJSON(w, http.StatusOK, map[string]string{"id": newJob.ID})
}

func (s *Server) countFicheStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	id := r.URL.Query().Get("id")
	if id == "" {
		http.Error(w, "missing id", http.StatusUnprocessableEntity)
		return
	}

	job, err := s.svc.Get(r.Context(), id)
	if err != nil {
		renderJSON(w, http.StatusNotFound, apiError{Code: 404, Message: "not found"})
		return
	}

	// Read count from .count file if job is done
	count := 0
	if job.Status == StatusOK {
		countPath := s.svc.GetCountFilePath(id)
		data, err := os.ReadFile(countPath)
		if err == nil {
			count, _ = strconv.Atoi(strings.TrimSpace(string(data)))
		}
	}

	type statusResp struct {
		Status string `json:"status"`
		Count  int    `json:"count"`
	}

	renderJSON(w, http.StatusOK, statusResp{
		Status: job.Status,
		Count:  count,
	})
}

func (s *Server) countFicheHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	jobs, err := s.svc.All(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	type historyItem struct {
		ID      string `json:"id"`
		Keyword string `json:"keyword"`
		Date    string `json:"date"`
		Status  string `json:"status"`
		Count   int    `json:"count"`
	}

	var items []historyItem
	for _, job := range jobs {
		if job.Data.JobType != JobTypeCountFiche {
			continue
		}

		count := 0
		if job.Status == StatusOK {
			countPath := s.svc.GetCountFilePath(job.ID)
			data, err := os.ReadFile(countPath)
			if err == nil {
				count, _ = strconv.Atoi(strings.TrimSpace(string(data)))
			}
		}

		kw := ""
		if len(job.Data.Keywords) > 0 {
			kw = job.Data.Keywords[0]
		}

		items = append(items, historyItem{
			ID:      job.ID,
			Keyword: kw,
			Date:    job.Date.Format("Jan 02, 2006 15:04"),
			Status:  job.Status,
			Count:   count,
		})
	}

	if items == nil {
		items = []historyItem{}
	}

	renderJSON(w, http.StatusOK, items)
}

func (s *Server) countFicheDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	id := r.URL.Query().Get("id")
	if id == "" {
		http.Error(w, "missing id", http.StatusUnprocessableEntity)
		return
	}

	err := s.svc.Delete(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Also remove the count file
	countPath := s.svc.GetCountFilePath(id)
	_ = os.Remove(countPath)

	w.WriteHeader(http.StatusOK)
}

func (s *Server) resultsPreview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	id, ok := getIDFromRequest(r)
	if !ok {
		http.Error(w, "Invalid ID", http.StatusUnprocessableEntity)
		return
	}

	filePath, err := s.svc.GetCSV(r.Context(), id.String())
	if err != nil {
		renderJSON(w, http.StatusNotFound, apiError{Code: 404, Message: "not found"})
		return
	}

	file, err := os.Open(filePath)
	if err != nil {
		renderJSON(w, http.StatusInternalServerError, apiError{Code: 500, Message: "cannot open"})
		return
	}
	defer file.Close()

	reader := csv.NewReader(file)
	records, err := reader.ReadAll()
	if err != nil {
		renderJSON(w, http.StatusInternalServerError, apiError{Code: 500, Message: "cannot parse csv"})
		return
	}

	if len(records) < 2 {
		renderJSON(w, http.StatusOK, []map[string]string{})
		return
	}

	headers := records[0]
	var results []map[string]string

	for _, row := range records[1:] {
		item := make(map[string]string)
		for i, h := range headers {
			if i < len(row) {
				item[h] = row[i]
			}
		}
		results = append(results, item)
	}

	renderJSON(w, http.StatusOK, results)
}

func (s *Server) resultsPage(w http.ResponseWriter, r *http.Request) {
	id, ok := getIDFromRequest(r)
	if !ok {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	job, err := s.svc.Get(r.Context(), id.String())
	if err != nil {
		http.Error(w, "Job not found", http.StatusNotFound)
		return
	}

	tmpl, ok := s.tmpl["static/templates/results.html"]
	if !ok {
		http.Error(w, "missing template", http.StatusInternalServerError)
		return
	}

	data := struct {
		Job Job
	}{Job: job}

	_ = tmpl.Execute(w, data)
}

func (s *Server) jobProgress(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	id, ok := getIDFromRequest(r)
	if !ok {
		http.Error(w, "Invalid ID", http.StatusUnprocessableEntity)
		return
	}

	job, err := s.svc.Get(r.Context(), id.String())
	if err != nil {
		renderJSON(w, http.StatusNotFound, apiError{Code: 404, Message: "not found"})
		return
	}

	// Count lines in CSV if exists
	count := 0
	filePath, err := s.svc.GetCSV(r.Context(), id.String())
	if err == nil {
		file, err := os.Open(filePath)
		if err == nil {
			reader := csv.NewReader(file)
			records, err := reader.ReadAll()
			if err == nil && len(records) > 1 {
				count = len(records) - 1 // minus header
			}
			file.Close()
		}
	}

	type progressResponse struct {
		Status     string `json:"status"`
		ResultCount int   `json:"result_count"`
	}

	renderJSON(w, http.StatusOK, progressResponse{
		Status:      job.Status,
		ResultCount: count,
	})
}

type chartItem struct {
	Label string `json:"label"`
	Count int    `json:"count"`
	Pct   int    `json:"pct"`
}

type dashboardData struct {
	TotalJobs     int         `json:"total_jobs"`
	CompletedJobs int         `json:"completed_jobs"`
	FailedJobs    int         `json:"failed_jobs"`
	TotalLeads    int         `json:"total_leads"`
	HasEmail      int         `json:"has_email"`
	HasPhone      int         `json:"has_phone"`
	NoWebsite     int         `json:"no_website"`
	HasEmailPct   int         `json:"has_email_pct"`
	HasPhonePct   int         `json:"has_phone_pct"`
	NoWebsitePct  int         `json:"no_website_pct"`
	LeadsByCountry []chartItem `json:"leads_by_country"`
	TopQueries     []chartItem `json:"top_queries"`
	JobsOverTime   []chartItem `json:"jobs_over_time"`

	// WhatsApp campaign stats
	WATotalCampaigns     int `json:"wa_total_campaigns"`
	WACompletedCampaigns int `json:"wa_completed_campaigns"`
	WAPausedCampaigns    int `json:"wa_paused_campaigns"`
	WARunningCampaigns   int `json:"wa_running_campaigns"`
	WATotalSent          int `json:"wa_total_sent"`
	WATotalFailed        int `json:"wa_total_failed"`
	WATotalPending       int `json:"wa_total_pending"`
	WASentToday          int `json:"wa_sent_today"`
	WADeliveryPct        int `json:"wa_delivery_pct"`
}

func (s *Server) computeDashboard(ctx context.Context) dashboardData {
	var d dashboardData

	jobs, err := s.svc.All(ctx)
	if err != nil {
		return d
	}

	d.TotalJobs = len(jobs)

	countryMap := make(map[string]int)
	queryMap := make(map[string]int)
	jobDateMap := make(map[string]int)

	for _, job := range jobs {
		switch job.Status {
		case StatusOK:
			d.CompletedJobs++
		case StatusFailed:
			d.FailedJobs++
		}

		// Group jobs by date
		dateKey := job.Date.Format("Jan 02")
		jobDateMap[dateKey]++

		// Only count CSV results for completed jobs
		if job.Status != StatusOK {
			continue
		}

		filePath, err := s.svc.GetCSV(ctx, job.ID)
		if err != nil {
			continue
		}

		file, err := os.Open(filePath)
		if err != nil {
			continue
		}

		reader := csv.NewReader(file)
		records, err := reader.ReadAll()
		file.Close()

		if err != nil || len(records) < 2 {
			continue
		}

		headers := records[0]
		headerIdx := make(map[string]int)
		for i, h := range headers {
			headerIdx[strings.ToLower(strings.TrimSpace(h))] = i
		}

		rows := records[1:]
		d.TotalLeads += len(rows)

		for _, row := range rows {
			if idx, ok := headerIdx["email"]; ok && idx < len(row) && strings.TrimSpace(row[idx]) != "" {
				d.HasEmail++
			}
			if idx, ok := headerIdx["phone"]; ok && idx < len(row) && strings.TrimSpace(row[idx]) != "" {
				d.HasPhone++
			}
			if idx, ok := headerIdx["website"]; ok && idx < len(row) && strings.TrimSpace(row[idx]) == "" {
				d.NoWebsite++
			} else if _, ok := headerIdx["website"]; !ok {
				// no website column means no website
				d.NoWebsite++
			}
		}

		// Country from job data
		country := job.Data.Country
		if country == "" {
			country = "Unknown"
		}
		countryMap[country] += len(rows)

		// Queries
		for _, kw := range job.Data.Keywords {
			queryMap[kw] += len(rows)
		}
	}

	// Percentages
	if d.TotalLeads > 0 {
		d.HasEmailPct = d.HasEmail * 100 / d.TotalLeads
		d.HasPhonePct = d.HasPhone * 100 / d.TotalLeads
		d.NoWebsitePct = d.NoWebsite * 100 / d.TotalLeads
	}

	// Leads by country
	d.LeadsByCountry = mapToChartItems(countryMap, 10)

	// Top queries
	d.TopQueries = mapToChartItems(queryMap, 10)

	// Jobs over time (last 30 days)
	now := time.Now().UTC()
	var timeItems []chartItem
	maxJobCount := 0
	for i := 29; i >= 0; i-- {
		day := now.AddDate(0, 0, -i)
		key := day.Format("Jan 02")
		count := jobDateMap[key]
		if count > maxJobCount {
			maxJobCount = count
		}
		timeItems = append(timeItems, chartItem{Label: key, Count: count})
	}
	for i := range timeItems {
		if maxJobCount > 0 {
			timeItems[i].Pct = timeItems[i].Count * 100 / maxJobCount
		}
	}
	d.JobsOverTime = timeItems

	// Fetch WhatsApp campaign stats from wa-bridge
	d.fetchWAStats()

	return d
}

func (d *dashboardData) fetchWAStats() {
	waHost := os.Getenv("WA_BRIDGE_URL")
	if waHost == "" {
		waHost = "http://wa-bridge:3001"
	}
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(waHost + "/stats")
	if err != nil {
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return
	}

	var stats struct {
		TotalCampaigns     int `json:"total_campaigns"`
		CompletedCampaigns int `json:"completed_campaigns"`
		PausedCampaigns    int `json:"paused_campaigns"`
		RunningCampaigns   int `json:"running_campaigns"`
		TotalSent          int `json:"total_sent"`
		TotalFailed        int `json:"total_failed"`
		TotalPending       int `json:"total_pending"`
		SentToday          int `json:"sent_today"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&stats); err != nil {
		return
	}

	d.WATotalCampaigns = stats.TotalCampaigns
	d.WACompletedCampaigns = stats.CompletedCampaigns
	d.WAPausedCampaigns = stats.PausedCampaigns
	d.WARunningCampaigns = stats.RunningCampaigns
	d.WATotalSent = stats.TotalSent
	d.WATotalFailed = stats.TotalFailed
	d.WATotalPending = stats.TotalPending
	d.WASentToday = stats.SentToday

	totalProcessed := stats.TotalSent + stats.TotalFailed
	if totalProcessed > 0 {
		d.WADeliveryPct = stats.TotalSent * 100 / totalProcessed
	}
}

func mapToChartItems(m map[string]int, limit int) []chartItem {
	type kv struct {
		key   string
		value int
	}

	var sorted []kv
	for k, v := range m {
		sorted = append(sorted, kv{k, v})
	}

	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].value > sorted[j].value
	})

	if len(sorted) > limit {
		sorted = sorted[:limit]
	}

	maxVal := 0
	if len(sorted) > 0 {
		maxVal = sorted[0].value
	}

	var items []chartItem
	for _, s := range sorted {
		pct := 0
		if maxVal > 0 {
			pct = s.value * 100 / maxVal
		}
		items = append(items, chartItem{
			Label: s.key,
			Count: s.value,
			Pct:   pct,
		})
	}

	return items
}

func (s *Server) dashboardPage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	tmpl, ok := s.tmpl["static/templates/dashboard.html"]
	if !ok {
		http.Error(w, "missing tpl", http.StatusInternalServerError)
		return
	}

	data := s.computeDashboard(r.Context())
	_ = tmpl.Execute(w, data)
}

func (s *Server) apiDashboard(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		renderJSON(w, http.StatusMethodNotAllowed, apiError{
			Code:    http.StatusMethodNotAllowed,
			Message: "Method not allowed",
		})
		return
	}

	data := s.computeDashboard(r.Context())
	renderJSON(w, http.StatusOK, data)
}

// --- Proxy Monitor handlers ---

func (s *Server) proxiesPage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	tmpl, ok := s.tmpl["static/templates/proxies.html"]
	if !ok {
		http.Error(w, "missing tpl", http.StatusInternalServerError)
		return
	}

	_ = tmpl.Execute(w, nil)
}

func (s *Server) proxiesCheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	err := r.ParseForm()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	raw := r.Form.Get("proxies")
	lines := strings.Split(raw, "\n")

	var proxies []string

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			proxies = append(proxies, line)
		}
	}

	if len(proxies) == 0 {
		http.Error(w, "no proxies provided", http.StatusUnprocessableEntity)
		return
	}

	results := s.svc.ProxyMonitor.CheckAll(r.Context(), proxies)

	renderJSON(w, http.StatusOK, results)
}

func (s *Server) apiProxyStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		renderJSON(w, http.StatusMethodNotAllowed, apiError{
			Code:    http.StatusMethodNotAllowed,
			Message: "Method not allowed",
		})
		return
	}

	statuses := s.svc.ProxyMonitor.GetStatuses()
	renderJSON(w, http.StatusOK, statuses)
}

func renderJSON(w http.ResponseWriter, code int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)

	_ = json.NewEncoder(w).Encode(data)
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-XSS-Protection", "1; mode=block")
		w.Header().Set("Content-Security-Policy",
			"default-src 'self'; "+
				"script-src 'self' cdn.redoc.ly cdnjs.cloudflare.com 'unsafe-inline' 'unsafe-eval'; "+
				"worker-src 'self' blob:; "+
				"style-src 'self' 'unsafe-inline' fonts.googleapis.com; "+
				"img-src 'self' data: cdn.redoc.ly; "+
				"font-src 'self' fonts.gstatic.com; "+
				"connect-src 'self' http://localhost:3001 http://localhost:3002")

		next.ServeHTTP(w, r)
	})
}

// Webhook handlers

func (s *Server) webhooksPage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	tmpl, ok := s.tmpl["static/templates/webhooks.html"]
	if !ok {
		http.Error(w, "missing tpl", http.StatusInternalServerError)
		return
	}

	webhooks, err := s.svc.AllWebhooks(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	data := struct {
		Webhooks []Webhook
		Events   []string
	}{
		Webhooks: webhooks,
		Events:   ValidEvents,
	}

	_ = tmpl.Execute(w, data)
}

func (s *Server) createWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	err := r.ParseForm()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	events := r.Form["events"]
	if len(events) == 0 {
		http.Error(w, "at least one event is required", http.StatusUnprocessableEntity)
		return
	}

	now := time.Now().UTC()

	wh := Webhook{
		ID:        uuid.New().String(),
		Name:      r.Form.Get("name"),
		URL:       r.Form.Get("url"),
		Secret:    r.Form.Get("secret"),
		Events:    strings.Join(events, ","),
		Enabled:   true,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := wh.Validate(); err != nil {
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}

	if err := s.svc.CreateWebhook(r.Context(), &wh); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("HX-Redirect", "/webhooks")
	w.WriteHeader(http.StatusCreated)
}

func (s *Server) deleteWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	id, ok := getIDFromRequest(r)
	if !ok {
		http.Error(w, "Invalid ID", http.StatusUnprocessableEntity)
		return
	}

	if err := s.svc.DeleteWebhook(r.Context(), id.String()); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (s *Server) toggleWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	id, ok := getIDFromRequest(r)
	if !ok {
		http.Error(w, "Invalid ID", http.StatusUnprocessableEntity)
		return
	}

	wh, err := s.svc.GetWebhook(r.Context(), id.String())
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	wh.Enabled = !wh.Enabled
	wh.UpdatedAt = time.Now().UTC()

	if wh.Enabled {
		wh.FailureCount = 0
	}

	if err := s.svc.UpdateWebhook(r.Context(), &wh); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("HX-Redirect", "/webhooks")
	w.WriteHeader(http.StatusOK)
}

func (s *Server) testWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	id, ok := getIDFromRequest(r)
	if !ok {
		http.Error(w, "Invalid ID", http.StatusUnprocessableEntity)
		return
	}

	wh, err := s.svc.GetWebhook(r.Context(), id.String())
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	dispatcher := s.svc.Dispatcher()
	if dispatcher == nil {
		http.Error(w, "webhook dispatcher not configured", http.StatusInternalServerError)
		return
	}

	go dispatcher.send(wh, "test", map[string]any{
		"message": "This is a test webhook event",
		"webhook": wh.Name,
	})

	w.Header().Set("HX-Redirect", "/webhooks")
	w.WriteHeader(http.StatusOK)
}

func (s *Server) apiGetWebhooks(w http.ResponseWriter, r *http.Request) {
	webhooks, err := s.svc.AllWebhooks(r.Context())
	if err != nil {
		renderJSON(w, http.StatusInternalServerError, apiError{
			Code:    http.StatusInternalServerError,
			Message: err.Error(),
		})
		return
	}

	renderJSON(w, http.StatusOK, webhooks)
}

// --- Queue handlers ---

type queuePageData struct {
	Tasks  []QueueTask
	Stats  map[string]int
	Filter string
}

func (s *Server) queuePage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	tmpl, ok := s.tmpl["static/templates/queue.html"]
	if !ok {
		http.Error(w, "missing tpl", http.StatusInternalServerError)
		return
	}

	filter := r.URL.Query().Get("status")

	params := QueueSelectParams{
		Status: filter,
		Limit:  100,
	}

	tasks, err := s.svc.SelectTasks(r.Context(), params)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	stats, err := s.svc.CountTasks(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	data := queuePageData{
		Tasks:  tasks,
		Stats:  stats,
		Filter: filter,
	}

	_ = tmpl.Execute(w, data)
}

func (s *Server) queueDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	id, ok := getIDFromRequest(r)
	if !ok {
		http.Error(w, "Invalid ID", http.StatusUnprocessableEntity)
		return
	}

	if err := s.svc.DeleteTask(r.Context(), id.String()); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (s *Server) apiGetQueue(w http.ResponseWriter, r *http.Request) {
	params := QueueSelectParams{
		Status:   r.URL.Query().Get("status"),
		TaskType: r.URL.Query().Get("type"),
		Limit:    100,
	}

	tasks, err := s.svc.SelectTasks(r.Context(), params)
	if err != nil {
		renderJSON(w, http.StatusInternalServerError, apiError{Code: 500, Message: err.Error()})
		return
	}

	renderJSON(w, http.StatusOK, tasks)
}

func (s *Server) apiGetQueueStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		renderJSON(w, http.StatusMethodNotAllowed, apiError{Code: 405, Message: "Method not allowed"})
		return
	}

	stats, err := s.svc.CountTasks(r.Context())
	if err != nil {
		renderJSON(w, http.StatusInternalServerError, apiError{Code: 500, Message: err.Error()})
		return
	}

	renderJSON(w, http.StatusOK, stats)
}
