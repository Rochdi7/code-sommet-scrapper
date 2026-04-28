package web

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

type Service struct {
	repo              JobRepository
	scheduleRepo      ScheduleRepository
	leadRepo          LeadRepository
	webhookRepo       WebhookRepository
	webScraperRepo    WebScraperResultRepository
	queueRepo         QueueRepository
	dispatcher        *WebhookDispatcher
	dataFolder        string
	ProxyMonitor      *ProxyMonitor

	// queue worker
	queueOnce sync.Once
	queueStop chan struct{}

	// running job cancel functions (jobID -> context.CancelFunc)
	jobCancels sync.Map
}

func NewService(repo JobRepository, dataFolder string) *Service {
	return &Service{
		repo:         repo,
		dataFolder:   dataFolder,
		ProxyMonitor: NewProxyMonitor(),
	}
}

// SetScheduleRepo sets the schedule repository. Called after construction
// so that the same sqlite Repo can satisfy both interfaces.
func (s *Service) SetScheduleRepo(r ScheduleRepository) {
	s.scheduleRepo = r
}

// SetLeadRepo sets the lead repository. Called after construction
// so that the same sqlite Repo can satisfy both interfaces.
func (s *Service) SetLeadRepo(r LeadRepository) {
	s.leadRepo = r
}

// SetWebhookRepo sets the webhook repository and creates the dispatcher.
func (s *Service) SetWebhookRepo(r WebhookRepository) {
	s.webhookRepo = r
	s.dispatcher = NewWebhookDispatcher(r)
}

// SetWebScraperRepo sets the webscraper result repository.
func (s *Service) SetWebScraperRepo(r WebScraperResultRepository) {
	s.webScraperRepo = r
}

// Dispatcher returns the webhook dispatcher (may be nil).
func (s *Service) Dispatcher() *WebhookDispatcher {
	return s.dispatcher
}

// Webhook methods

func (s *Service) GetWebhook(ctx context.Context, id string) (Webhook, error) {
	return s.webhookRepo.GetWebhook(ctx, id)
}

func (s *Service) CreateWebhook(ctx context.Context, wh *Webhook) error {
	return s.webhookRepo.CreateWebhook(ctx, wh)
}

func (s *Service) DeleteWebhook(ctx context.Context, id string) error {
	return s.webhookRepo.DeleteWebhook(ctx, id)
}

func (s *Service) AllWebhooks(ctx context.Context) ([]Webhook, error) {
	return s.webhookRepo.SelectWebhooks(ctx)
}

func (s *Service) UpdateWebhook(ctx context.Context, wh *Webhook) error {
	return s.webhookRepo.UpdateWebhook(ctx, wh)
}

func (s *Service) Create(ctx context.Context, job *Job) error {
	return s.repo.Create(ctx, job)
}

func (s *Service) All(ctx context.Context) ([]Job, error) {
	return s.repo.Select(ctx, SelectParams{})
}

func (s *Service) Get(ctx context.Context, id string) (Job, error) {
	return s.repo.Get(ctx, id)
}

func (s *Service) Delete(ctx context.Context, id string) error {
	if strings.Contains(id, "/") || strings.Contains(id, "\\") || strings.Contains(id, "..") {
		return fmt.Errorf("invalid file name")
	}

	datapath := filepath.Join(s.dataFolder, id+".csv")

	if _, err := os.Stat(datapath); err == nil {
		if err := os.Remove(datapath); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}

	return s.repo.Delete(ctx, id)
}

func (s *Service) Update(ctx context.Context, job *Job) error {
	return s.repo.Update(ctx, job)
}

func (s *Service) SelectPending(ctx context.Context) ([]Job, error) {
	return s.repo.Select(ctx, SelectParams{Status: StatusPending, Limit: 1})
}

// RegisterJobCancel stores a cancel function for a running job.
func (s *Service) RegisterJobCancel(jobID string, cancel context.CancelFunc) {
	s.jobCancels.Store(jobID, cancel)
}

// UnregisterJobCancel removes the cancel function for a job.
func (s *Service) UnregisterJobCancel(jobID string) {
	s.jobCancels.Delete(jobID)
}

// StopJob cancels a running job and sets its status to "stopped".
func (s *Service) StopJob(ctx context.Context, jobID string) error {
	if cancelVal, ok := s.jobCancels.Load(jobID); ok {
		if cancel, ok := cancelVal.(context.CancelFunc); ok {
			cancel()
		}
		s.jobCancels.Delete(jobID)
	}
	return nil
}

func (s *Service) GetCSV(_ context.Context, id string) (string, error) {
	if strings.Contains(id, "/") || strings.Contains(id, "\\") || strings.Contains(id, "..") {
		return "", fmt.Errorf("invalid file name")
	}

	datapath := filepath.Join(s.dataFolder, id+".csv")

	if _, err := os.Stat(datapath); os.IsNotExist(err) {
		return "", fmt.Errorf("csv file not found for job %s", id)
	}

	return datapath, nil
}

func (s *Service) GetCountFilePath(id string) string {
	return filepath.Join(s.dataFolder, id+".count")
}

// Schedule methods

func (s *Service) CreateSchedule(ctx context.Context, sched *Schedule) error {
	return s.scheduleRepo.CreateSchedule(ctx, sched)
}

func (s *Service) GetSchedule(ctx context.Context, id string) (Schedule, error) {
	return s.scheduleRepo.GetSchedule(ctx, id)
}

func (s *Service) DeleteSchedule(ctx context.Context, id string) error {
	return s.scheduleRepo.DeleteSchedule(ctx, id)
}

func (s *Service) AllSchedules(ctx context.Context) ([]Schedule, error) {
	return s.scheduleRepo.SelectSchedules(ctx)
}

func (s *Service) UpdateSchedule(ctx context.Context, sched *Schedule) error {
	return s.scheduleRepo.UpdateSchedule(ctx, sched)
}

// RunDueSchedules finds enabled schedules whose NextRun <= now, creates a Job
// from their JobData, and advances NextRun.
func (s *Service) RunDueSchedules(ctx context.Context) error {
	schedules, err := s.scheduleRepo.SelectSchedules(ctx)
	if err != nil {
		return err
	}

	now := time.Now().UTC()

	for i := range schedules {
		sched := &schedules[i]

		if !sched.Enabled {
			continue
		}

		if sched.NextRun.After(now) {
			continue
		}

		// Create a new job from this schedule
		newJob := Job{
			ID:     uuid.New().String(),
			Name:   fmt.Sprintf("%s (scheduled)", sched.Name),
			Date:   now,
			Status: StatusPending,
			Data:   sched.JobData,
		}

		if s.queueRepo != nil {
			if _, err := s.EnqueueJob(ctx, &newJob); err != nil {
				log.Printf("schedule %s: failed to enqueue job: %v", sched.ID, err)
				continue
			}
		} else {
			if err := s.repo.Create(ctx, &newJob); err != nil {
				log.Printf("schedule %s: failed to create job: %v", sched.ID, err)
				continue
			}
		}

		log.Printf("schedule %s: created job %s", sched.ID, newJob.ID)

		// Update LastRun and compute NextRun
		sched.LastRun = now

		nextRun, err := ParseScheduleFrom(sched.CronExpr, now)
		if err != nil {
			log.Printf("schedule %s: failed to parse next run: %v", sched.ID, err)
			continue
		}

		sched.NextRun = nextRun

		if err := s.scheduleRepo.UpdateSchedule(ctx, sched); err != nil {
			log.Printf("schedule %s: failed to update: %v", sched.ID, err)
		}
	}

	return nil
}

// Lead methods

func (s *Service) GetLead(ctx context.Context, id string) (Lead, error) {
	return s.leadRepo.GetLead(ctx, id)
}

func (s *Service) CreateLead(ctx context.Context, lead *Lead) error {
	return s.leadRepo.CreateLead(ctx, lead)
}

func (s *Service) UpdateLead(ctx context.Context, lead *Lead) error {
	return s.leadRepo.UpdateLead(ctx, lead)
}

func (s *Service) DeleteLead(ctx context.Context, id string) error {
	return s.leadRepo.DeleteLead(ctx, id)
}

func (s *Service) SelectLeads(ctx context.Context, params LeadSelectParams) ([]Lead, error) {
	return s.leadRepo.SelectLeads(ctx, params)
}

func (s *Service) CountLeadsByStatus(ctx context.Context) (map[string]int, error) {
	return s.leadRepo.CountLeadsByStatus(ctx)
}

// ImportLeadsFromJob reads a completed job's CSV and inserts each row as a Lead.
// Uses INSERT OR IGNORE for deduplication on (phone, title).
// Returns the number of leads imported.
func (s *Service) ImportLeadsFromJob(ctx context.Context, jobID string) (int, error) {
	job, err := s.repo.Get(ctx, jobID)
	if err != nil {
		return 0, fmt.Errorf("job not found: %w", err)
	}

	filePath, err := s.GetCSV(ctx, jobID)
	if err != nil {
		return 0, fmt.Errorf("csv not found: %w", err)
	}

	file, err := os.Open(filePath)
	if err != nil {
		return 0, fmt.Errorf("cannot open csv: %w", err)
	}
	defer file.Close()

	reader := csv.NewReader(file)
	records, err := reader.ReadAll()
	if err != nil {
		return 0, fmt.Errorf("cannot parse csv: %w", err)
	}

	if len(records) < 2 {
		return 0, nil
	}

	headers := records[0]
	headerIdx := make(map[string]int)
	for i, h := range headers {
		headerIdx[strings.TrimSpace(strings.ToLower(h))] = i
	}

	getVal := func(row []string, key string) string {
		idx, ok := headerIdx[key]
		if !ok || idx >= len(row) {
			return ""
		}
		return strings.TrimSpace(row[idx])
	}

	count := 0

	for _, row := range records[1:] {
		title := getVal(row, "title")
		if title == "" {
			continue
		}

		phone := getVal(row, "phone")

		ratingStr := getVal(row, "review_rating")
		rating, _ := strconv.ParseFloat(ratingStr, 64)

		reviewsStr := getVal(row, "review_count")
		reviews, _ := strconv.Atoi(reviewsStr)

		// Try multiple possible CSV header names for email
		email := getVal(row, "emails")
		if email == "" {
			email = getVal(row, "email")
		}

		lead := Lead{
			ID:       uuid.New().String(),
			PlaceID:  getVal(row, "place_id"),
			Title:    title,
			Category: getVal(row, "category"),
			Phone:    phone,
			Website:  getVal(row, "website"),
			Email:    email,
			Rating:   rating,
			Reviews:  reviews,
			Address:  getVal(row, "address"),
			Country:  job.Data.Country,
			JobID:    jobID,
			Tags:     "",
			Notes:    "",
			Status:   LeadStatusNew,
		}

		if err := s.leadRepo.CreateLead(ctx, &lead); err != nil {
			log.Printf("failed to import lead %q: %v", title, err)
			continue
		}

		count++
	}

	return count, nil
}

// WebScraper methods

func (s *Service) CreateWebScraperResult(ctx context.Context, r *WebScraperResult) error {
	return s.webScraperRepo.CreateWebScraperResult(ctx, r)
}

func (s *Service) SelectWebScraperResults(ctx context.Context, params WebScraperSelectParams) ([]WebScraperResult, error) {
	return s.webScraperRepo.SelectWebScraperResults(ctx, params)
}

func (s *Service) CountWebScraperResults(ctx context.Context, jobID string) (total, withEmail, withPhone int, err error) {
	return s.webScraperRepo.CountWebScraperResults(ctx, jobID)
}

func (s *Service) DeleteWebScraperResultsByJob(ctx context.Context, jobID string) error {
	return s.webScraperRepo.DeleteWebScraperResultsByJob(ctx, jobID)
}

// Queue methods

// SetQueueRepo sets the queue repository.
func (s *Service) SetQueueRepo(r QueueRepository) {
	s.queueRepo = r
}

// Enqueue creates a new queue task and stores it.
func (s *Service) Enqueue(ctx context.Context, taskType, refID string, payload any, priority int) (*QueueTask, error) {
	if s.queueRepo == nil {
		return nil, fmt.Errorf("queue repository not configured")
	}

	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal payload: %w", err)
	}

	task := &QueueTask{
		ID:          uuid.New().String(),
		TaskType:    taskType,
		RefID:       refID,
		Payload:     string(payloadJSON),
		Status:      QueueStatusPending,
		Priority:    priority,
		MaxAttempts: 3,
		CreatedAt:   time.Now().UTC(),
	}

	if err := s.queueRepo.EnqueueTask(ctx, task); err != nil {
		return nil, err
	}

	log.Printf("queue: enqueued task %s type=%s ref=%s", task.ID, taskType, refID)

	return task, nil
}

// GetTask returns a single queue task by ID.
func (s *Service) GetTask(ctx context.Context, id string) (QueueTask, error) {
	return s.queueRepo.GetTask(ctx, id)
}

// SelectTasks lists queue tasks with filters.
func (s *Service) SelectTasks(ctx context.Context, params QueueSelectParams) ([]QueueTask, error) {
	return s.queueRepo.SelectTasks(ctx, params)
}

// DeleteTask removes a queue task.
func (s *Service) DeleteTask(ctx context.Context, id string) error {
	return s.queueRepo.DeleteTask(ctx, id)
}

// CountTasks returns queue task counts grouped by status.
func (s *Service) CountTasks(ctx context.Context) (map[string]int, error) {
	return s.queueRepo.CountTasks(ctx)
}

// StartQueueWorker starts a background goroutine that processes queue tasks.
// It is safe to call multiple times; only one worker will be started.
func (s *Service) StartQueueWorker(ctx context.Context) {
	if s.queueRepo == nil {
		return
	}

	s.queueOnce.Do(func() {
		s.queueStop = make(chan struct{})
		go s.runQueueWorker(ctx)
		log.Println("queue: worker started")
	})
}

// StopQueueWorker signals the queue worker to stop.
func (s *Service) StopQueueWorker() {
	if s.queueStop != nil {
		close(s.queueStop)
	}
}

func (s *Service) runQueueWorker(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	// Requeue stale tasks every 60 seconds
	staleTicker := time.NewTicker(60 * time.Second)
	defer staleTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-s.queueStop:
			return
		case <-staleTicker.C:
			n, err := s.queueRepo.RequeueStale(ctx, 10*time.Minute)
			if err != nil {
				log.Printf("queue: requeue stale error: %v", err)
			} else if n > 0 {
				log.Printf("queue: requeued %d stale tasks", n)
			}
		case <-ticker.C:
			s.processNextTask(ctx)
		}
	}
}

func (s *Service) processNextTask(ctx context.Context) {
	task, err := s.queueRepo.DequeueTask(ctx)
	if err != nil {
		log.Printf("queue: dequeue error: %v", err)
		return
	}
	if task == nil {
		return // nothing to process
	}

	log.Printf("queue: processing task %s type=%s ref=%s (attempt %d/%d)",
		task.ID, task.TaskType, task.RefID, task.Attempts, task.MaxAttempts)

	var taskErr error

	switch task.TaskType {
	case TaskTypeScrape, TaskTypeWebScraper, TaskTypeCountFiche:
		taskErr = s.processJobTask(ctx, task)
	case TaskTypeWebhook:
		taskErr = s.processWebhookTask(ctx, task)
	case TaskTypeLeadImport:
		taskErr = s.processLeadImportTask(ctx, task)
	case TaskTypeScheduleRun:
		taskErr = s.processScheduleTask(ctx, task)
	default:
		taskErr = fmt.Errorf("unknown task type: %s", task.TaskType)
	}

	if taskErr != nil {
		task.Error = taskErr.Error()
		if task.Attempts >= task.MaxAttempts {
			task.Status = QueueStatusFailed
			task.CompletedAt = time.Now().UTC()
			log.Printf("queue: task %s FAILED permanently: %v", task.ID, taskErr)
		} else {
			// Re-queue for retry
			task.Status = QueueStatusPending
			log.Printf("queue: task %s failed (attempt %d/%d), will retry: %v",
				task.ID, task.Attempts, task.MaxAttempts, taskErr)
		}
	} else {
		task.Status = QueueStatusCompleted
		task.CompletedAt = time.Now().UTC()
		log.Printf("queue: task %s completed successfully", task.ID)
	}

	if err := s.queueRepo.UpdateTask(ctx, task); err != nil {
		log.Printf("queue: failed to update task %s: %v", task.ID, err)
	}
}

// processJobTask handles scrape/webscraper/countfiche queue tasks.
// It creates the actual Job in pending state so the existing scraper worker picks it up.
func (s *Service) processJobTask(ctx context.Context, task *QueueTask) error {
	var jobData JobData
	if err := json.Unmarshal([]byte(task.Payload), &jobData); err != nil {
		return fmt.Errorf("invalid job payload: %w", err)
	}

	// If refID is set, the job was already created — just ensure it's pending.
	if task.RefID != "" {
		existing, err := s.repo.Get(ctx, task.RefID)
		if err == nil {
			if existing.Status == StatusPending || existing.Status == StatusWorking {
				return nil // job already exists and is in progress
			}
		}
		// Job doesn't exist or is in a terminal state — recreate
	}

	newJob := Job{
		ID:     task.RefID,
		Name:   fmt.Sprintf("queued-%s", task.TaskType),
		Date:   time.Now().UTC(),
		Status: StatusPending,
		Data:   jobData,
	}

	if newJob.ID == "" {
		newJob.ID = uuid.New().String()
	}

	if err := s.repo.Create(ctx, &newJob); err != nil {
		return fmt.Errorf("failed to create job: %w", err)
	}

	// Update the task's refID to the job ID
	task.RefID = newJob.ID

	return nil
}

// WebhookTaskPayload is the payload for webhook queue tasks.
type WebhookTaskPayload struct {
	Event string `json:"event"`
	Data  any    `json:"data"`
}

// processWebhookTask dispatches a webhook via the queue.
func (s *Service) processWebhookTask(ctx context.Context, task *QueueTask) error {
	if s.dispatcher == nil {
		return fmt.Errorf("webhook dispatcher not configured")
	}

	var payload WebhookTaskPayload
	if err := json.Unmarshal([]byte(task.Payload), &payload); err != nil {
		return fmt.Errorf("invalid webhook payload: %w", err)
	}

	// Fire the webhook for the specific webhook ID
	if task.RefID != "" {
		wh, err := s.webhookRepo.GetWebhook(ctx, task.RefID)
		if err != nil {
			return fmt.Errorf("webhook %s not found: %w", task.RefID, err)
		}

		if !wh.Enabled {
			return nil // webhook disabled, skip
		}

		s.dispatcher.send(wh, payload.Event, payload.Data)

		return nil
	}

	// No specific ref — fire to all matching webhooks
	s.dispatcher.Fire(ctx, payload.Event, payload.Data)

	return nil
}

// processLeadImportTask imports leads from a job CSV via the queue.
func (s *Service) processLeadImportTask(ctx context.Context, task *QueueTask) error {
	jobID := task.RefID
	if jobID == "" {
		return fmt.Errorf("missing job ID for lead import")
	}

	count, err := s.ImportLeadsFromJob(ctx, jobID)
	if err != nil {
		return err
	}

	log.Printf("queue: imported %d leads from job %s", count, jobID)

	// Fire webhook for leads imported
	if s.dispatcher != nil {
		s.EnqueueWebhook(ctx, EventLeadsImported, map[string]any{
			"job_id": jobID,
			"count":  count,
		})
	}

	return nil
}

// processScheduleTask runs due schedules via the queue.
func (s *Service) processScheduleTask(ctx context.Context, task *QueueTask) error {
	return s.RunDueSchedules(ctx)
}

// EnqueueWebhook is a helper that enqueues webhook dispatch tasks for all matching webhooks.
func (s *Service) EnqueueWebhook(ctx context.Context, event string, data any) {
	if s.queueRepo == nil || s.webhookRepo == nil {
		// Fall back to direct dispatch
		if s.dispatcher != nil {
			s.dispatcher.Fire(ctx, event, data)
		}
		return
	}

	webhooks, err := s.webhookRepo.SelectWebhooks(ctx)
	if err != nil {
		log.Printf("queue: failed to select webhooks: %v", err)
		return
	}

	payload := WebhookTaskPayload{Event: event, Data: data}

	for _, wh := range webhooks {
		if !wh.Enabled || !wh.MatchesEvent(event) {
			continue
		}

		_, err := s.Enqueue(ctx, TaskTypeWebhook, wh.ID, payload, 5)
		if err != nil {
			log.Printf("queue: failed to enqueue webhook %s: %v", wh.ID, err)
		}
	}
}

// EnqueueLeadImport enqueues a lead import task for a completed job.
func (s *Service) EnqueueLeadImport(ctx context.Context, jobID string) (*QueueTask, error) {
	return s.Enqueue(ctx, TaskTypeLeadImport, jobID, map[string]string{"job_id": jobID}, 5)
}

// EnqueueJob creates a job and enqueues it for processing.
func (s *Service) EnqueueJob(ctx context.Context, job *Job) (*QueueTask, error) {
	// Create the job first
	if err := s.repo.Create(ctx, job); err != nil {
		return nil, err
	}

	// Determine task type
	taskType := TaskTypeScrape
	switch job.Data.JobType {
	case JobTypeWebScraper:
		taskType = TaskTypeWebScraper
	case JobTypeCountFiche:
		taskType = TaskTypeCountFiche
	}

	return s.Enqueue(ctx, taskType, job.ID, job.Data, 10)
}
