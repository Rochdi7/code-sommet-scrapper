package web

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// schedulePageData is the data passed to the schedules template.
type schedulePageData struct {
	Schedules []Schedule
	Form      formData
}

func (s *Server) schedulesPage(w http.ResponseWriter, r *http.Request) {
	tmpl, ok := s.tmpl["static/templates/schedules.html"]
	if !ok {
		http.Error(w, "missing tpl", http.StatusInternalServerError)
		return
	}

	schedules, err := s.svc.AllSchedules(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	data := schedulePageData{
		Schedules: schedules,
		Form: formData{
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
			Email:      false,
			Country:    "Morocco",
			MaxResults: 50,
		},
	}

	_ = tmpl.Execute(w, data)
}

func (s *Server) createSchedule(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
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

	var keywords []string

	keywordsStr, ok := r.Form["keywords"]
	if !ok {
		http.Error(w, "missing keywords", http.StatusUnprocessableEntity)
		return
	}

	for _, k := range strings.Split(keywordsStr[0], "\n") {
		k = strings.TrimSpace(k)
		if k != "" {
			keywords = append(keywords, k)
		}
	}

	zoom, err := strconv.Atoi(r.Form.Get("zoom"))
	if err != nil {
		http.Error(w, "invalid zoom", http.StatusUnprocessableEntity)
		return
	}

	radius, err := strconv.Atoi(r.Form.Get("radius"))
	if err != nil {
		http.Error(w, "invalid radius", http.StatusUnprocessableEntity)
		return
	}

	depth, err := strconv.Atoi(r.Form.Get("depth"))
	if err != nil {
		http.Error(w, "invalid depth", http.StatusUnprocessableEntity)
		return
	}

	maxResults := 50

	if v := r.Form.Get("maxresults"); v != "" {
		maxResults, err = strconv.Atoi(v)
		if err != nil {
			http.Error(w, "invalid max results", http.StatusUnprocessableEntity)
			return
		}
	}

	var proxies []string

	for _, p := range strings.Split(r.Form.Get("proxies"), "\n") {
		p = strings.TrimSpace(p)
		if p != "" {
			proxies = append(proxies, p)
		}
	}

	cronExpr := r.Form.Get("cron_expr")
	now := time.Now().UTC()

	nextRun, err := ParseScheduleFrom(cronExpr, now)
	if err != nil {
		http.Error(w, "invalid schedule: "+err.Error(), http.StatusUnprocessableEntity)
		return
	}

	country := r.Form.Get("country")
	if country == "" {
		country = "Morocco"
	}

	sched := Schedule{
		ID:       uuid.New().String(),
		Name:     r.Form.Get("name"),
		CronExpr: cronExpr,
		JobData: JobData{
			Keywords:   keywords,
			Lang:       r.Form.Get("lang"),
			Zoom:       zoom,
			Lat:        r.Form.Get("latitude"),
			Lon:        r.Form.Get("longitude"),
			FastMode:   r.Form.Get("fastmode") == "on",
			Radius:     radius,
			Depth:      depth,
			Email:      r.Form.Get("email") == "on",
			MaxTime:    maxTime,
			Proxies:    proxies,
			Country:    country,
			MaxResults: maxResults,
		},
		Enabled:   true,
		NextRun:   nextRun,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := sched.Validate(); err != nil {
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}

	if err := s.svc.CreateSchedule(r.Context(), &sched); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("HX-Redirect", "/schedules")
	w.WriteHeader(http.StatusOK)
}

func (s *Server) deleteSchedule(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	id, ok := getIDFromRequest(r)
	if !ok {
		http.Error(w, "Invalid ID", http.StatusUnprocessableEntity)
		return
	}

	if err := s.svc.DeleteSchedule(r.Context(), id.String()); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (s *Server) toggleSchedule(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	id, ok := getIDFromRequest(r)
	if !ok {
		http.Error(w, "Invalid ID", http.StatusUnprocessableEntity)
		return
	}

	sched, err := s.svc.GetSchedule(r.Context(), id.String())
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	sched.Enabled = !sched.Enabled

	if sched.Enabled {
		nextRun, parseErr := ParseScheduleFrom(sched.CronExpr, time.Now().UTC())
		if parseErr == nil {
			sched.NextRun = nextRun
		}
	}

	if err := s.svc.UpdateSchedule(r.Context(), &sched); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("HX-Redirect", "/schedules")
	w.WriteHeader(http.StatusOK)
}

func (s *Server) apiGetSchedules(w http.ResponseWriter, r *http.Request) {
	schedules, err := s.svc.AllSchedules(r.Context())
	if err != nil {
		renderJSON(w, http.StatusInternalServerError, apiError{
			Code:    http.StatusInternalServerError,
			Message: err.Error(),
		})

		return
	}

	renderJSON(w, http.StatusOK, schedules)
}
