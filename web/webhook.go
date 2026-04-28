package web

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"time"
)

const (
	EventJobCompleted = "job.completed"
	EventJobFailed    = "job.failed"
	EventLeadsImported = "leads.imported"
)

var ValidEvents = []string{EventJobCompleted, EventJobFailed, EventLeadsImported}

type Webhook struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	URL           string    `json:"url"`
	Secret        string    `json:"secret,omitempty"`
	Events        string    `json:"events"`
	Enabled       bool      `json:"enabled"`
	LastTriggered time.Time `json:"last_triggered"`
	LastStatus    int       `json:"last_status"`
	FailureCount  int       `json:"failure_count"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

func (w *Webhook) Validate() error {
	if w.ID == "" {
		return errors.New("missing id")
	}

	if w.Name == "" {
		return errors.New("missing name")
	}

	if w.URL == "" {
		return errors.New("missing url")
	}

	parsed, err := url.Parse(w.URL)
	if err != nil {
		return errors.New("invalid url")
	}

	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return errors.New("url must use http or https scheme")
	}

	if parsed.Host == "" {
		return errors.New("url must have a host")
	}

	if w.Events == "" {
		return errors.New("missing events")
	}

	events := strings.Split(w.Events, ",")
	for _, e := range events {
		e = strings.TrimSpace(e)
		if !isValidEvent(e) {
			return errors.New("invalid event: " + e)
		}
	}

	return nil
}

func (w *Webhook) MatchesEvent(event string) bool {
	events := strings.Split(w.Events, ",")
	for _, e := range events {
		if strings.TrimSpace(e) == event {
			return true
		}
	}

	return false
}

func isValidEvent(event string) bool {
	for _, e := range ValidEvents {
		if e == event {
			return true
		}
	}

	return false
}

type WebhookRepository interface {
	GetWebhook(ctx context.Context, id string) (Webhook, error)
	CreateWebhook(ctx context.Context, webhook *Webhook) error
	DeleteWebhook(ctx context.Context, id string) error
	SelectWebhooks(ctx context.Context) ([]Webhook, error)
	UpdateWebhook(ctx context.Context, webhook *Webhook) error
}
