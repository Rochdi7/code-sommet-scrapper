package web

import (
	"fmt"
	"html/template"
	"net/http"
	"strconv"
	"strings"
)

// --- Leads CRM handlers ---

type leadsPageData struct {
	Leads    []Lead
	Stats    map[string]int
	Jobs     []Job
	Statuses []string
	Params   LeadSelectParams
	Page     int
	HasPrev  bool
	HasNext  bool
}

func (s *Server) leadsPage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	tmpl, ok := s.tmpl["static/templates/leads.html"]
	if !ok {
		http.Error(w, "missing tpl", http.StatusInternalServerError)
		return
	}

	q := r.URL.Query()

	page, _ := strconv.Atoi(q.Get("page"))
	if page < 1 {
		page = 1
	}

	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit <= 0 {
		limit = 50
	}

	params := LeadSelectParams{
		Status:    q.Get("status"),
		Tag:       q.Get("tag"),
		Search:    q.Get("search"),
		HasEmail:  q.Get("has_email") == "1",
		HasPhone:  q.Get("has_phone") == "1",
		NoWebsite: q.Get("no_website") == "1",
		Page:      page,
		Limit:     limit,
	}

	leads, err := s.svc.SelectLeads(r.Context(), params)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	stats, err := s.svc.CountLeadsByStatus(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	jobs, err := s.svc.All(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var completedJobs []Job
	for _, j := range jobs {
		if j.Status == StatusOK {
			completedJobs = append(completedJobs, j)
		}
	}

	data := leadsPageData{
		Leads:    leads,
		Stats:    stats,
		Jobs:     completedJobs,
		Statuses: LeadStatuses,
		Params:   params,
		Page:     page,
		HasPrev:  page > 1,
		HasNext:  len(leads) >= limit,
	}

	_ = tmpl.Execute(w, data)
}

func (s *Server) leadsImport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	jobID := r.URL.Query().Get("job_id")
	if jobID == "" {
		_ = r.ParseForm()
		jobID = r.Form.Get("job_id")
	}

	if jobID == "" {
		http.Error(w, "missing job_id", http.StatusUnprocessableEntity)
		return
	}

	if s.svc.queueRepo != nil {
		_, err := s.svc.EnqueueLeadImport(r.Context(), jobID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("HX-Redirect", "/leads")
		_, _ = fmt.Fprint(w, "Lead import queued")
	} else {
		count, err := s.svc.ImportLeadsFromJob(r.Context(), jobID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("HX-Redirect", "/leads")
		_, _ = fmt.Fprintf(w, "Imported %d leads", count)
	}
}

func (s *Server) leadsUpdateTags(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "missing id", http.StatusUnprocessableEntity)
		return
	}

	_ = r.ParseForm()
	tags := r.Form.Get("tags")

	lead, err := s.svc.GetLead(r.Context(), id)
	if err != nil {
		http.Error(w, "lead not found", http.StatusNotFound)
		return
	}

	lead.Tags = tags
	if err := s.svc.UpdateLead(r.Context(), &lead); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if tags == "" {
		_, _ = fmt.Fprint(w, `<span class="lead-tag-empty">No tags</span>`)
	} else {
		for _, tag := range strings.Split(tags, ",") {
			tag = strings.TrimSpace(tag)
			if tag != "" {
				_, _ = fmt.Fprintf(w, `<span class="lead-tag">%s</span> `, template.HTMLEscapeString(tag))
			}
		}
	}
}

func (s *Server) leadsUpdateNotes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "missing id", http.StatusUnprocessableEntity)
		return
	}

	_ = r.ParseForm()
	notes := r.Form.Get("notes")

	lead, err := s.svc.GetLead(r.Context(), id)
	if err != nil {
		http.Error(w, "lead not found", http.StatusNotFound)
		return
	}

	lead.Notes = notes
	if err := s.svc.UpdateLead(r.Context(), &lead); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if notes == "" {
		_, _ = fmt.Fprint(w, `<span class="lead-notes-empty">No notes</span>`)
	} else {
		_, _ = fmt.Fprintf(w, `<span class="lead-notes-text">%s</span>`, template.HTMLEscapeString(notes))
	}
}

func (s *Server) leadsUpdateStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "missing id", http.StatusUnprocessableEntity)
		return
	}

	_ = r.ParseForm()
	status := r.Form.Get("status")

	valid := false
	for _, st := range LeadStatuses {
		if status == st {
			valid = true
			break
		}
	}

	if !valid {
		http.Error(w, "invalid status", http.StatusUnprocessableEntity)
		return
	}

	lead, err := s.svc.GetLead(r.Context(), id)
	if err != nil {
		http.Error(w, "lead not found", http.StatusNotFound)
		return
	}

	lead.Status = status
	if err := s.svc.UpdateLead(r.Context(), &lead); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	_, _ = fmt.Fprintf(w, `<span class="lead-status-badge status-%s">%s</span>`, template.HTMLEscapeString(status), template.HTMLEscapeString(status))
}

func (s *Server) leadsDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	id, ok := getIDFromRequest(r)
	if !ok {
		http.Error(w, "Invalid ID", http.StatusUnprocessableEntity)
		return
	}

	if err := s.svc.DeleteLead(r.Context(), id.String()); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (s *Server) apiGetLeads(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		renderJSON(w, http.StatusMethodNotAllowed, apiError{Code: 405, Message: "Method not allowed"})
		return
	}

	q := r.URL.Query()

	page, _ := strconv.Atoi(q.Get("page"))
	if page < 1 {
		page = 1
	}

	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit <= 0 {
		limit = 50
	}

	params := LeadSelectParams{
		Status:    q.Get("status"),
		Tag:       q.Get("tag"),
		Search:    q.Get("search"),
		HasEmail:  q.Get("has_email") == "1",
		HasPhone:  q.Get("has_phone") == "1",
		NoWebsite: q.Get("no_website") == "1",
		Page:      page,
		Limit:     limit,
	}

	leads, err := s.svc.SelectLeads(r.Context(), params)
	if err != nil {
		renderJSON(w, http.StatusInternalServerError, apiError{Code: 500, Message: err.Error()})
		return
	}

	renderJSON(w, http.StatusOK, leads)
}

func (s *Server) apiGetLeadStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		renderJSON(w, http.StatusMethodNotAllowed, apiError{Code: 405, Message: "Method not allowed"})
		return
	}

	stats, err := s.svc.CountLeadsByStatus(r.Context())
	if err != nil {
		renderJSON(w, http.StatusInternalServerError, apiError{Code: 500, Message: err.Error()})
		return
	}

	renderJSON(w, http.StatusOK, stats)
}
