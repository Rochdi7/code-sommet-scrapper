package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite" // sqlite driver

	"github.com/gosom/google-maps-scraper/web"
)

type repo struct {
	db *sql.DB
}

// Repo wraps both JobRepository and ScheduleRepository.
type Repo struct {
	repo
}

func New(path string) (*Repo, error) {
	db, err := initDatabase(path)
	if err != nil {
		return nil, err
	}

	return &Repo{repo{db: db}}, nil
}

func (repo *repo) Get(ctx context.Context, id string) (web.Job, error) {
	const q = `SELECT * from jobs WHERE id = ?`

	row := repo.db.QueryRowContext(ctx, q, id)

	return rowToJob(row)
}

func (repo *repo) Create(ctx context.Context, job *web.Job) error {
	item, err := jobToRow(job)
	if err != nil {
		return err
	}

	const q = `INSERT INTO jobs (id, name, status, data, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`

	_, err = repo.db.ExecContext(ctx, q, item.ID, item.Name, item.Status, item.Data, item.CreatedAt, item.UpdatedAt)
	if err != nil {
		return err
	}

	return nil
}

func (repo *repo) Delete(ctx context.Context, id string) error {
	const q = `DELETE FROM jobs WHERE id = ?`

	_, err := repo.db.ExecContext(ctx, q, id)

	return err
}

func (repo *repo) Select(ctx context.Context, params web.SelectParams) ([]web.Job, error) {
	q := `SELECT * from jobs`

	var args []any

	if params.Status != "" {
		q += ` WHERE status = ?`

		args = append(args, params.Status)
	}

	q += " ORDER BY created_at DESC"

	if params.Limit > 0 {
		q += " LIMIT ?"

		args = append(args, params.Limit)
	}

	rows, err := repo.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}

	defer rows.Close()

	var ans []web.Job

	for rows.Next() {
		job, err := rowToJob(rows)
		if err != nil {
			return nil, err
		}

		ans = append(ans, job)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return ans, nil
}

func (repo *repo) Update(ctx context.Context, job *web.Job) error {
	item, err := jobToRow(job)
	if err != nil {
		return err
	}

	const q = `UPDATE jobs SET name = ?, status = ?, data = ?, updated_at = ? WHERE id = ?`

	_, err = repo.db.ExecContext(ctx, q, item.Name, item.Status, item.Data, item.UpdatedAt, item.ID)

	return err
}

type scannable interface {
	Scan(dest ...any) error
}

func rowToJob(row scannable) (web.Job, error) {
	var j job

	err := row.Scan(&j.ID, &j.Name, &j.Status, &j.Data, &j.CreatedAt, &j.UpdatedAt)
	if err != nil {
		return web.Job{}, err
	}

	ans := web.Job{
		ID:     j.ID,
		Name:   j.Name,
		Status: j.Status,
		Date:   time.Unix(j.CreatedAt, 0).UTC(),
	}

	err = json.Unmarshal([]byte(j.Data), &ans.Data)
	if err != nil {
		return web.Job{}, err
	}

	return ans, nil
}

func jobToRow(item *web.Job) (job, error) {
	data, err := json.Marshal(item.Data)
	if err != nil {
		return job{}, err
	}

	return job{
		ID:        item.ID,
		Name:      item.Name,
		Status:    item.Status,
		Data:      string(data),
		CreatedAt: item.Date.Unix(),
		UpdatedAt: time.Now().UTC().Unix(),
	}, nil
}

type job struct {
	ID        string
	Name      string
	Status    string
	Data      string
	CreatedAt int64
	UpdatedAt int64
}

func initDatabase(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}

	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(30 * time.Minute)

	_, err = db.Exec("PRAGMA busy_timeout = 5000")
	if err != nil {
		return nil, err
	}

	_, err = db.Exec("PRAGMA journal_mode=WAL")
	if err != nil {
		return nil, err
	}

	_, err = db.Exec("PRAGMA synchronous=NORMAL")
	if err != nil {
		return nil, err
	}

	_, err = db.Exec("PRAGMA cache_size=1000")
	if err != nil {
		return nil, err
	}

	err = db.Ping()
	if err != nil {
		return nil, err
	}

	return db, createSchema(db)
}

func createSchema(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS jobs (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			status TEXT NOT NULL,
			data TEXT NOT NULL,
			created_at INT NOT NULL,
			updated_at INT NOT NULL
		)
	`)
	if err != nil {
		return err
	}

	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS leads (
			id TEXT PRIMARY KEY,
			place_id TEXT,
			title TEXT NOT NULL,
			category TEXT,
			phone TEXT,
			website TEXT,
			email TEXT,
			rating REAL,
			reviews INTEGER,
			address TEXT,
			country TEXT,
			job_id TEXT,
			tags TEXT DEFAULT '',
			notes TEXT DEFAULT '',
			status TEXT DEFAULT 'new',
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL,
			UNIQUE(phone, title)
		)
	`)
	if err != nil {
		return err
	}

	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS schedules (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			cron_expr TEXT NOT NULL,
			job_data TEXT NOT NULL,
			enabled INTEGER NOT NULL DEFAULT 1,
			last_run INTEGER DEFAULT 0,
			next_run INTEGER DEFAULT 0,
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL
		)
	`)
	if err != nil {
		return err
	}

	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS webscraper_results (
			id TEXT PRIMARY KEY,
			job_id TEXT NOT NULL,
			url TEXT NOT NULL,
			title TEXT,
			description TEXT,
			emails TEXT,
			phones TEXT,
			socials TEXT,
			search_query TEXT,
			error TEXT,
			created_at INTEGER NOT NULL
		)
	`)
	if err != nil {
		return err
	}

	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS webhooks (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			url TEXT NOT NULL,
			secret TEXT DEFAULT '',
			events TEXT NOT NULL,
			enabled INTEGER NOT NULL DEFAULT 1,
			last_triggered INTEGER DEFAULT 0,
			last_status INTEGER DEFAULT 0,
			failure_count INTEGER DEFAULT 0,
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL
		)
	`)
	if err != nil {
		return err
	}

	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS queue_tasks (
			id TEXT PRIMARY KEY,
			task_type TEXT NOT NULL,
			ref_id TEXT DEFAULT '',
			payload TEXT DEFAULT '{}',
			status TEXT NOT NULL DEFAULT 'pending',
			priority INTEGER NOT NULL DEFAULT 10,
			attempts INTEGER NOT NULL DEFAULT 0,
			max_attempts INTEGER NOT NULL DEFAULT 3,
			error TEXT DEFAULT '',
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL,
			started_at INTEGER DEFAULT 0,
			completed_at INTEGER DEFAULT 0
		)
	`)
	if err != nil {
		return err
	}

	_, err = db.Exec(`CREATE INDEX IF NOT EXISTS idx_queue_status_priority ON queue_tasks (status, priority, created_at)`)

	return err
}

// Schedule CRUD

type schedule struct {
	ID        string
	Name      string
	CronExpr  string
	JobData   string
	Enabled   int
	LastRun   int64
	NextRun   int64
	CreatedAt int64
	UpdatedAt int64
}

func (r *Repo) GetSchedule(ctx context.Context, id string) (web.Schedule, error) {
	const q = `SELECT id, name, cron_expr, job_data, enabled, last_run, next_run, created_at, updated_at FROM schedules WHERE id = ?`

	row := r.db.QueryRowContext(ctx, q, id)

	return rowToSchedule(row)
}

func (r *Repo) CreateSchedule(ctx context.Context, s *web.Schedule) error {
	item, err := scheduleToRow(s)
	if err != nil {
		return err
	}

	const q = `INSERT INTO schedules (id, name, cron_expr, job_data, enabled, last_run, next_run, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`

	_, err = r.db.ExecContext(ctx, q, item.ID, item.Name, item.CronExpr, item.JobData, item.Enabled, item.LastRun, item.NextRun, item.CreatedAt, item.UpdatedAt)

	return err
}

func (r *Repo) DeleteSchedule(ctx context.Context, id string) error {
	const q = `DELETE FROM schedules WHERE id = ?`

	_, err := r.db.ExecContext(ctx, q, id)

	return err
}

func (r *Repo) SelectSchedules(ctx context.Context) ([]web.Schedule, error) {
	const q = `SELECT id, name, cron_expr, job_data, enabled, last_run, next_run, created_at, updated_at FROM schedules ORDER BY created_at DESC`

	rows, err := r.db.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ans []web.Schedule

	for rows.Next() {
		s, err := rowToSchedule(rows)
		if err != nil {
			return nil, err
		}

		ans = append(ans, s)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return ans, nil
}

func (r *Repo) UpdateSchedule(ctx context.Context, s *web.Schedule) error {
	item, err := scheduleToRow(s)
	if err != nil {
		return err
	}

	const q = `UPDATE schedules SET name = ?, cron_expr = ?, job_data = ?, enabled = ?, last_run = ?, next_run = ?, updated_at = ? WHERE id = ?`

	_, err = r.db.ExecContext(ctx, q, item.Name, item.CronExpr, item.JobData, item.Enabled, item.LastRun, item.NextRun, item.UpdatedAt, item.ID)

	return err
}

func rowToSchedule(row scannable) (web.Schedule, error) {
	var s schedule

	err := row.Scan(&s.ID, &s.Name, &s.CronExpr, &s.JobData, &s.Enabled, &s.LastRun, &s.NextRun, &s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		return web.Schedule{}, err
	}

	ans := web.Schedule{
		ID:        s.ID,
		Name:      s.Name,
		CronExpr:  s.CronExpr,
		Enabled:   s.Enabled == 1,
		LastRun:   time.Unix(s.LastRun, 0).UTC(),
		NextRun:   time.Unix(s.NextRun, 0).UTC(),
		CreatedAt: time.Unix(s.CreatedAt, 0).UTC(),
		UpdatedAt: time.Unix(s.UpdatedAt, 0).UTC(),
	}

	err = json.Unmarshal([]byte(s.JobData), &ans.JobData)
	if err != nil {
		return web.Schedule{}, err
	}

	return ans, nil
}

func scheduleToRow(s *web.Schedule) (schedule, error) {
	data, err := json.Marshal(s.JobData)
	if err != nil {
		return schedule{}, err
	}

	enabled := 0
	if s.Enabled {
		enabled = 1
	}

	return schedule{
		ID:        s.ID,
		Name:      s.Name,
		CronExpr:  s.CronExpr,
		JobData:   string(data),
		Enabled:   enabled,
		LastRun:   s.LastRun.Unix(),
		NextRun:   s.NextRun.Unix(),
		CreatedAt: s.CreatedAt.Unix(),
		UpdatedAt: time.Now().UTC().Unix(),
	}, nil
}

// Lead CRUD

func (r *Repo) GetLead(ctx context.Context, id string) (web.Lead, error) {
	const q = `SELECT id, place_id, title, category, phone, website, email, rating, reviews, address, country, job_id, tags, notes, status, created_at, updated_at FROM leads WHERE id = ?`

	row := r.db.QueryRowContext(ctx, q, id)

	return rowToLead(row)
}

func (r *Repo) CreateLead(ctx context.Context, lead *web.Lead) error {
	now := time.Now().UTC().Unix()

	const q = `INSERT OR IGNORE INTO leads (id, place_id, title, category, phone, website, email, rating, reviews, address, country, job_id, tags, notes, status, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	_, err := r.db.ExecContext(ctx, q,
		lead.ID, lead.PlaceID, lead.Title, lead.Category,
		lead.Phone, lead.Website, lead.Email,
		lead.Rating, lead.Reviews,
		lead.Address, lead.Country, lead.JobID,
		lead.Tags, lead.Notes, lead.Status,
		now, now,
	)

	return err
}

func (r *Repo) UpdateLead(ctx context.Context, lead *web.Lead) error {
	now := time.Now().UTC().Unix()

	const q = `UPDATE leads SET place_id = ?, title = ?, category = ?, phone = ?, website = ?, email = ?, rating = ?, reviews = ?, address = ?, country = ?, job_id = ?, tags = ?, notes = ?, status = ?, updated_at = ? WHERE id = ?`

	_, err := r.db.ExecContext(ctx, q,
		lead.PlaceID, lead.Title, lead.Category,
		lead.Phone, lead.Website, lead.Email,
		lead.Rating, lead.Reviews,
		lead.Address, lead.Country, lead.JobID,
		lead.Tags, lead.Notes, lead.Status,
		now, lead.ID,
	)

	return err
}

func (r *Repo) DeleteLead(ctx context.Context, id string) error {
	const q = `DELETE FROM leads WHERE id = ?`

	_, err := r.db.ExecContext(ctx, q, id)

	return err
}

func (r *Repo) SelectLeads(ctx context.Context, params web.LeadSelectParams) ([]web.Lead, error) {
	q := `SELECT id, place_id, title, category, phone, website, email, rating, reviews, address, country, job_id, tags, notes, status, created_at, updated_at FROM leads`

	var conditions []string
	var args []any

	if params.Status != "" {
		conditions = append(conditions, "status = ?")
		args = append(args, params.Status)
	}

	if params.Tag != "" {
		conditions = append(conditions, "(',' || tags || ',') LIKE ?")
		args = append(args, "%,"+params.Tag+",%")
	}

	if params.HasEmail {
		conditions = append(conditions, "email != '' AND email IS NOT NULL")
	}

	if params.HasPhone {
		conditions = append(conditions, "phone != '' AND phone IS NOT NULL")
	}

	if params.NoWebsite {
		conditions = append(conditions, "(website = '' OR website IS NULL)")
	}

	if params.Search != "" {
		conditions = append(conditions, "(title LIKE ? OR category LIKE ? OR address LIKE ?)")
		s := "%" + params.Search + "%"
		args = append(args, s, s, s)
	}

	if len(conditions) > 0 {
		q += " WHERE " + strings.Join(conditions, " AND ")
	}

	q += " ORDER BY created_at DESC"

	limit := params.Limit
	if limit <= 0 {
		limit = 50
	}

	page := params.Page
	if page < 1 {
		page = 1
	}

	offset := (page - 1) * limit

	q += fmt.Sprintf(" LIMIT %d OFFSET %d", limit, offset)

	rows, err := r.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ans []web.Lead

	for rows.Next() {
		lead, err := rowToLead(rows)
		if err != nil {
			return nil, err
		}

		ans = append(ans, lead)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return ans, nil
}

func (r *Repo) CountLeadsByStatus(ctx context.Context) (map[string]int, error) {
	const q = `SELECT status, COUNT(*) FROM leads GROUP BY status`

	rows, err := r.db.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	ans := make(map[string]int)

	for rows.Next() {
		var status string
		var count int

		if err := rows.Scan(&status, &count); err != nil {
			return nil, err
		}

		ans[status] = count
	}

	return ans, rows.Err()
}

func rowToLead(row scannable) (web.Lead, error) {
	var (
		id        string
		placeID   sql.NullString
		title     string
		category  sql.NullString
		phone     sql.NullString
		website   sql.NullString
		email     sql.NullString
		rating    sql.NullFloat64
		reviews   sql.NullInt64
		address   sql.NullString
		country   sql.NullString
		jobID     sql.NullString
		tags      sql.NullString
		notes     sql.NullString
		status    string
		createdAt int64
		updatedAt int64
	)

	err := row.Scan(&id, &placeID, &title, &category, &phone, &website, &email, &rating, &reviews, &address, &country, &jobID, &tags, &notes, &status, &createdAt, &updatedAt)
	if err != nil {
		return web.Lead{}, err
	}

	return web.Lead{
		ID:        id,
		PlaceID:   placeID.String,
		Title:     title,
		Category:  category.String,
		Phone:     phone.String,
		Website:   website.String,
		Email:     email.String,
		Rating:    rating.Float64,
		Reviews:   int(reviews.Int64),
		Address:   address.String,
		Country:   country.String,
		JobID:     jobID.String,
		Tags:      tags.String,
		Notes:     notes.String,
		Status:    status,
		CreatedAt: time.Unix(createdAt, 0).UTC(),
		UpdatedAt: time.Unix(updatedAt, 0).UTC(),
	}, nil
}

// Webhook CRUD

type webhookRow struct {
	ID            string
	Name          string
	URL           string
	Secret        string
	Events        string
	Enabled       int
	LastTriggered int64
	LastStatus    int
	FailureCount  int
	CreatedAt     int64
	UpdatedAt     int64
}

func (r *Repo) GetWebhook(ctx context.Context, id string) (web.Webhook, error) {
	const q = `SELECT id, name, url, secret, events, enabled, last_triggered, last_status, failure_count, created_at, updated_at FROM webhooks WHERE id = ?`

	row := r.db.QueryRowContext(ctx, q, id)

	return rowToWebhook(row)
}

func (r *Repo) CreateWebhook(ctx context.Context, wh *web.Webhook) error {
	item := webhookToRow(wh)

	const q = `INSERT INTO webhooks (id, name, url, secret, events, enabled, last_triggered, last_status, failure_count, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	_, err := r.db.ExecContext(ctx, q, item.ID, item.Name, item.URL, item.Secret, item.Events, item.Enabled, item.LastTriggered, item.LastStatus, item.FailureCount, item.CreatedAt, item.UpdatedAt)

	return err
}

func (r *Repo) DeleteWebhook(ctx context.Context, id string) error {
	const q = `DELETE FROM webhooks WHERE id = ?`

	_, err := r.db.ExecContext(ctx, q, id)

	return err
}

func (r *Repo) SelectWebhooks(ctx context.Context) ([]web.Webhook, error) {
	const q = `SELECT id, name, url, secret, events, enabled, last_triggered, last_status, failure_count, created_at, updated_at FROM webhooks ORDER BY created_at DESC`

	rows, err := r.db.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ans []web.Webhook

	for rows.Next() {
		wh, err := rowToWebhook(rows)
		if err != nil {
			return nil, err
		}

		ans = append(ans, wh)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return ans, nil
}

func (r *Repo) UpdateWebhook(ctx context.Context, wh *web.Webhook) error {
	item := webhookToRow(wh)

	const q = `UPDATE webhooks SET name = ?, url = ?, secret = ?, events = ?, enabled = ?, last_triggered = ?, last_status = ?, failure_count = ?, updated_at = ? WHERE id = ?`

	_, err := r.db.ExecContext(ctx, q, item.Name, item.URL, item.Secret, item.Events, item.Enabled, item.LastTriggered, item.LastStatus, item.FailureCount, item.UpdatedAt, item.ID)

	return err
}

func rowToWebhook(row scannable) (web.Webhook, error) {
	var w webhookRow

	err := row.Scan(&w.ID, &w.Name, &w.URL, &w.Secret, &w.Events, &w.Enabled, &w.LastTriggered, &w.LastStatus, &w.FailureCount, &w.CreatedAt, &w.UpdatedAt)
	if err != nil {
		return web.Webhook{}, err
	}

	return web.Webhook{
		ID:            w.ID,
		Name:          w.Name,
		URL:           w.URL,
		Secret:        w.Secret,
		Events:        w.Events,
		Enabled:       w.Enabled == 1,
		LastTriggered: time.Unix(w.LastTriggered, 0).UTC(),
		LastStatus:    w.LastStatus,
		FailureCount:  w.FailureCount,
		CreatedAt:     time.Unix(w.CreatedAt, 0).UTC(),
		UpdatedAt:     time.Unix(w.UpdatedAt, 0).UTC(),
	}, nil
}

func webhookToRow(wh *web.Webhook) webhookRow {
	enabled := 0
	if wh.Enabled {
		enabled = 1
	}

	return webhookRow{
		ID:            wh.ID,
		Name:          wh.Name,
		URL:           wh.URL,
		Secret:        wh.Secret,
		Events:        wh.Events,
		Enabled:       enabled,
		LastTriggered: wh.LastTriggered.Unix(),
		LastStatus:    wh.LastStatus,
		FailureCount:  wh.FailureCount,
		CreatedAt:     wh.CreatedAt.Unix(),
		UpdatedAt:     time.Now().UTC().Unix(),
	}
}

// WebScraper Result CRUD

func (r *Repo) CreateWebScraperResult(ctx context.Context, result *web.WebScraperResult) error {
	now := time.Now().UTC().Unix()

	const q = `INSERT INTO webscraper_results (id, job_id, url, title, description, emails, phones, socials, search_query, error, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	_, err := r.db.ExecContext(ctx, q,
		result.ID, result.JobID, result.URL, result.Title, result.Description,
		result.Emails, result.Phones, result.Socials, result.SearchQuery,
		result.Error, now,
	)

	return err
}

func (r *Repo) SelectWebScraperResults(ctx context.Context, params web.WebScraperSelectParams) ([]web.WebScraperResult, error) {
	q := `SELECT id, job_id, url, title, description, emails, phones, socials, search_query, error, created_at FROM webscraper_results`

	var conditions []string
	var args []any

	if params.JobID != "" {
		conditions = append(conditions, "job_id = ?")
		args = append(args, params.JobID)
	}

	if params.HasEmail {
		conditions = append(conditions, "emails != '' AND emails IS NOT NULL")
	}

	if params.HasPhone {
		conditions = append(conditions, "phones != '' AND phones IS NOT NULL")
	}

	if params.Search != "" {
		conditions = append(conditions, "(title LIKE ? OR url LIKE ? OR emails LIKE ?)")
		s := "%" + params.Search + "%"
		args = append(args, s, s, s)
	}

	if len(conditions) > 0 {
		q += " WHERE " + strings.Join(conditions, " AND ")
	}

	q += " ORDER BY created_at DESC"

	limit := params.Limit
	if limit <= 0 {
		limit = 100
	}

	page := params.Page
	if page < 1 {
		page = 1
	}

	offset := (page - 1) * limit

	q += fmt.Sprintf(" LIMIT %d OFFSET %d", limit, offset)

	rows, err := r.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ans []web.WebScraperResult

	for rows.Next() {
		var (
			id          string
			jobID       string
			siteURL     string
			title       sql.NullString
			description sql.NullString
			emails      sql.NullString
			phones      sql.NullString
			socials     sql.NullString
			searchQuery sql.NullString
			errStr      sql.NullString
			createdAt   int64
		)

		err := rows.Scan(&id, &jobID, &siteURL, &title, &description, &emails, &phones, &socials, &searchQuery, &errStr, &createdAt)
		if err != nil {
			return nil, err
		}

		ans = append(ans, web.WebScraperResult{
			ID:          id,
			JobID:       jobID,
			URL:         siteURL,
			Title:       title.String,
			Description: description.String,
			Emails:      emails.String,
			Phones:      phones.String,
			Socials:     socials.String,
			SearchQuery: searchQuery.String,
			Error:       errStr.String,
			CreatedAt:   time.Unix(createdAt, 0).UTC(),
		})
	}

	return ans, rows.Err()
}

func (r *Repo) CountWebScraperResults(ctx context.Context, jobID string) (total, withEmail, withPhone int, err error) {
	q := `SELECT COUNT(*) FROM webscraper_results`
	args := []any{}

	if jobID != "" {
		q += " WHERE job_id = ?"
		args = append(args, jobID)
	}

	err = r.db.QueryRowContext(ctx, q, args...).Scan(&total)
	if err != nil {
		return
	}

	qEmail := `SELECT COUNT(*) FROM webscraper_results WHERE emails != '' AND emails IS NOT NULL`
	if jobID != "" {
		qEmail += " AND job_id = ?"
	}

	err = r.db.QueryRowContext(ctx, qEmail, args...).Scan(&withEmail)
	if err != nil {
		return
	}

	qPhone := `SELECT COUNT(*) FROM webscraper_results WHERE phones != '' AND phones IS NOT NULL`
	if jobID != "" {
		qPhone += " AND job_id = ?"
	}

	err = r.db.QueryRowContext(ctx, qPhone, args...).Scan(&withPhone)

	return
}

func (r *Repo) DeleteWebScraperResultsByJob(ctx context.Context, jobID string) error {
	const q = `DELETE FROM webscraper_results WHERE job_id = ?`

	_, err := r.db.ExecContext(ctx, q, jobID)

	return err
}

// Queue CRUD

func (r *Repo) EnqueueTask(ctx context.Context, task *web.QueueTask) error {
	now := time.Now().UTC().Unix()

	const q = `INSERT INTO queue_tasks (id, task_type, ref_id, payload, status, priority, attempts, max_attempts, error, created_at, updated_at, started_at, completed_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	_, err := r.db.ExecContext(ctx, q,
		task.ID, task.TaskType, task.RefID, task.Payload,
		task.Status, task.Priority, task.Attempts, task.MaxAttempts,
		task.Error, now, now, 0, 0,
	)

	return err
}

// DequeueTask atomically picks the highest-priority pending task and marks it processing.
func (r *Repo) DequeueTask(ctx context.Context, taskTypes ...string) (*web.QueueTask, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback() //nolint:errcheck

	q := `SELECT id, task_type, ref_id, payload, status, priority, attempts, max_attempts, error, created_at, updated_at, started_at, completed_at FROM queue_tasks WHERE status = 'pending'`

	var args []any

	if len(taskTypes) > 0 {
		placeholders := make([]string, len(taskTypes))
		for i, t := range taskTypes {
			placeholders[i] = "?"
			args = append(args, t)
		}
		q += " AND task_type IN (" + strings.Join(placeholders, ",") + ")"
	}

	q += " ORDER BY priority ASC, created_at ASC LIMIT 1"

	row := tx.QueryRowContext(ctx, q, args...)

	task, err := rowToQueueTask(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}

	now := time.Now().UTC().Unix()

	const updateQ = `UPDATE queue_tasks SET status = 'processing', attempts = attempts + 1, started_at = ?, updated_at = ? WHERE id = ?`

	_, err = tx.ExecContext(ctx, updateQ, now, now, task.ID)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	task.Status = web.QueueStatusProcessing
	task.Attempts++
	task.StartedAt = time.Unix(now, 0).UTC()

	return &task, nil
}

func (r *Repo) UpdateTask(ctx context.Context, task *web.QueueTask) error {
	now := time.Now().UTC().Unix()

	var completedAt int64
	if !task.CompletedAt.IsZero() {
		completedAt = task.CompletedAt.Unix()
	}

	var startedAt int64
	if !task.StartedAt.IsZero() {
		startedAt = task.StartedAt.Unix()
	}

	const q = `UPDATE queue_tasks SET status = ?, priority = ?, attempts = ?, max_attempts = ?, error = ?, updated_at = ?, started_at = ?, completed_at = ? WHERE id = ?`

	_, err := r.db.ExecContext(ctx, q,
		task.Status, task.Priority, task.Attempts, task.MaxAttempts,
		task.Error, now, startedAt, completedAt, task.ID,
	)

	return err
}

func (r *Repo) GetTask(ctx context.Context, id string) (web.QueueTask, error) {
	const q = `SELECT id, task_type, ref_id, payload, status, priority, attempts, max_attempts, error, created_at, updated_at, started_at, completed_at FROM queue_tasks WHERE id = ?`

	row := r.db.QueryRowContext(ctx, q, id)

	return rowToQueueTask(row)
}

func (r *Repo) SelectTasks(ctx context.Context, params web.QueueSelectParams) ([]web.QueueTask, error) {
	q := `SELECT id, task_type, ref_id, payload, status, priority, attempts, max_attempts, error, created_at, updated_at, started_at, completed_at FROM queue_tasks`

	var conditions []string
	var args []any

	if params.Status != "" {
		conditions = append(conditions, "status = ?")
		args = append(args, params.Status)
	}

	if params.TaskType != "" {
		conditions = append(conditions, "task_type = ?")
		args = append(args, params.TaskType)
	}

	if len(conditions) > 0 {
		q += " WHERE " + strings.Join(conditions, " AND ")
	}

	q += " ORDER BY priority ASC, created_at DESC"

	if params.Limit > 0 {
		q += fmt.Sprintf(" LIMIT %d", params.Limit)
	}

	rows, err := r.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ans []web.QueueTask

	for rows.Next() {
		task, err := rowToQueueTask(rows)
		if err != nil {
			return nil, err
		}
		ans = append(ans, task)
	}

	return ans, rows.Err()
}

func (r *Repo) DeleteTask(ctx context.Context, id string) error {
	const q = `DELETE FROM queue_tasks WHERE id = ?`

	_, err := r.db.ExecContext(ctx, q, id)

	return err
}

func (r *Repo) CountTasks(ctx context.Context) (map[string]int, error) {
	const q = `SELECT status, COUNT(*) FROM queue_tasks GROUP BY status`

	rows, err := r.db.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	ans := make(map[string]int)

	for rows.Next() {
		var status string
		var count int

		if err := rows.Scan(&status, &count); err != nil {
			return nil, err
		}
		ans[status] = count
	}

	return ans, rows.Err()
}

func (r *Repo) RequeueStale(ctx context.Context, olderThan time.Duration) (int, error) {
	cutoff := time.Now().UTC().Add(-olderThan).Unix()

	const q = `UPDATE queue_tasks SET status = 'pending', updated_at = ? WHERE status = 'processing' AND started_at < ? AND started_at > 0`

	now := time.Now().UTC().Unix()

	result, err := r.db.ExecContext(ctx, q, now, cutoff)
	if err != nil {
		return 0, err
	}

	n, err := result.RowsAffected()

	return int(n), err
}

func rowToQueueTask(row scannable) (web.QueueTask, error) {
	var (
		id          string
		taskType    string
		refID       string
		payload     string
		status      string
		priority    int
		attempts    int
		maxAttempts int
		errStr      string
		createdAt   int64
		updatedAt   int64
		startedAt   int64
		completedAt int64
	)

	err := row.Scan(&id, &taskType, &refID, &payload, &status, &priority, &attempts, &maxAttempts, &errStr, &createdAt, &updatedAt, &startedAt, &completedAt)
	if err != nil {
		return web.QueueTask{}, err
	}

	return web.QueueTask{
		ID:          id,
		TaskType:    taskType,
		RefID:       refID,
		Payload:     payload,
		Status:      status,
		Priority:    priority,
		Attempts:    attempts,
		MaxAttempts: maxAttempts,
		Error:       errStr,
		CreatedAt:   time.Unix(createdAt, 0).UTC(),
		UpdatedAt:   time.Unix(updatedAt, 0).UTC(),
		StartedAt:   time.Unix(startedAt, 0).UTC(),
		CompletedAt: time.Unix(completedAt, 0).UTC(),
	}, nil
}
