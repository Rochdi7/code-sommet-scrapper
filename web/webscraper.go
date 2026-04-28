package web

import (
	"context"
	"time"
)

type WebScraperResult struct {
	ID          string
	JobID       string
	URL         string
	Title       string
	Description string
	Emails      string
	Phones      string
	Socials     string
	SearchQuery string
	Error       string
	CreatedAt   time.Time
}

type WebScraperSelectParams struct {
	JobID    string
	HasEmail bool
	HasPhone bool
	Search   string
	Page     int
	Limit    int
}

type WebScraperResultRepository interface {
	CreateWebScraperResult(ctx context.Context, r *WebScraperResult) error
	SelectWebScraperResults(ctx context.Context, params WebScraperSelectParams) ([]WebScraperResult, error)
	CountWebScraperResults(ctx context.Context, jobID string) (total, withEmail, withPhone int, err error)
	DeleteWebScraperResultsByJob(ctx context.Context, jobID string) error
}
