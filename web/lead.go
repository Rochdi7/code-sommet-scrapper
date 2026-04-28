package web

import (
	"context"
	"time"
)

const (
	LeadStatusNew           = "new"
	LeadStatusContacted     = "contacted"
	LeadStatusInterested    = "interested"
	LeadStatusNotInterested = "not_interested"
	LeadStatusConverted     = "converted"
)

var LeadStatuses = []string{
	LeadStatusNew,
	LeadStatusContacted,
	LeadStatusInterested,
	LeadStatusNotInterested,
	LeadStatusConverted,
}

type Lead struct {
	ID        string
	PlaceID   string
	Title     string
	Category  string
	Phone     string
	Website   string
	Email     string
	Rating    float64
	Reviews   int
	Address   string
	Country   string
	JobID     string
	Tags      string
	Notes     string
	Status    string
	CreatedAt time.Time
	UpdatedAt time.Time
}

type LeadSelectParams struct {
	Status    string
	Tag       string
	Search    string
	HasEmail  bool
	HasPhone  bool
	NoWebsite bool
	Page      int
	Limit     int
}

type LeadRepository interface {
	GetLead(ctx context.Context, id string) (Lead, error)
	CreateLead(ctx context.Context, lead *Lead) error
	UpdateLead(ctx context.Context, lead *Lead) error
	DeleteLead(ctx context.Context, id string) error
	SelectLeads(ctx context.Context, params LeadSelectParams) ([]Lead, error)
	CountLeadsByStatus(ctx context.Context) (map[string]int, error)
}
