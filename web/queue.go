package web

import (
	"context"
	"errors"
	"time"
)

// Task types that can be enqueued.
const (
	TaskTypeScrape      = "scrape"
	TaskTypeWebScraper  = "webscraper"
	TaskTypeCountFiche  = "countfiche"
	TaskTypeWebhook     = "webhook"
	TaskTypeLeadImport  = "lead_import"
	TaskTypeScheduleRun = "schedule_run"
)

// Task statuses.
const (
	QueueStatusPending    = "pending"
	QueueStatusProcessing = "processing"
	QueueStatusCompleted  = "completed"
	QueueStatusFailed     = "failed"
)

// QueueTask represents a unit of work in the queue.
type QueueTask struct {
	ID          string
	TaskType    string
	RefID       string // reference ID (job ID, webhook ID, schedule ID, etc.)
	Payload     string // JSON-encoded task-specific data
	Status      string
	Priority    int // lower = higher priority (0 is highest)
	Attempts    int
	MaxAttempts int
	Error       string
	CreatedAt   time.Time
	UpdatedAt   time.Time
	StartedAt   time.Time
	CompletedAt time.Time
}

func (t *QueueTask) Validate() error {
	if t.ID == "" {
		return errors.New("missing id")
	}
	if t.TaskType == "" {
		return errors.New("missing task type")
	}
	if t.Status == "" {
		return errors.New("missing status")
	}
	return nil
}

type QueueSelectParams struct {
	Status   string
	TaskType string
	Limit    int
}

type QueueRepository interface {
	EnqueueTask(ctx context.Context, task *QueueTask) error
	DequeueTask(ctx context.Context, taskTypes ...string) (*QueueTask, error)
	UpdateTask(ctx context.Context, task *QueueTask) error
	GetTask(ctx context.Context, id string) (QueueTask, error)
	SelectTasks(ctx context.Context, params QueueSelectParams) ([]QueueTask, error)
	DeleteTask(ctx context.Context, id string) error
	CountTasks(ctx context.Context) (map[string]int, error)
	// RequeueStale marks processing tasks older than the given duration back to pending.
	RequeueStale(ctx context.Context, olderThan time.Duration) (int, error)
}
