package web

import (
	"context"
	"errors"
	"time"
)

type ScheduleRepository interface {
	GetSchedule(ctx context.Context, id string) (Schedule, error)
	CreateSchedule(ctx context.Context, s *Schedule) error
	DeleteSchedule(ctx context.Context, id string) error
	SelectSchedules(ctx context.Context) ([]Schedule, error)
	UpdateSchedule(ctx context.Context, s *Schedule) error
}

type Schedule struct {
	ID        string
	Name      string
	CronExpr  string
	JobData   JobData
	Enabled   bool
	LastRun   time.Time
	NextRun   time.Time
	CreatedAt time.Time
	UpdatedAt time.Time
}

func (s *Schedule) Validate() error {
	if s.ID == "" {
		return errors.New("missing id")
	}

	if s.Name == "" {
		return errors.New("missing name")
	}

	if s.CronExpr == "" {
		return errors.New("missing schedule expression")
	}

	if _, err := ParseSchedule(s.CronExpr); err != nil {
		return err
	}

	if err := s.JobData.Validate(); err != nil {
		return err
	}

	return nil
}
