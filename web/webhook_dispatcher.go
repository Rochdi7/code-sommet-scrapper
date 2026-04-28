package web

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"
)

const (
	webhookTimeout       = 10 * time.Second
	maxConsecutiveFails  = 10
)

type WebhookPayload struct {
	Event     string `json:"event"`
	Timestamp string `json:"timestamp"`
	Data      any    `json:"data"`
}

type WebhookDispatcher struct {
	repo WebhookRepository
}

func NewWebhookDispatcher(repo WebhookRepository) *WebhookDispatcher {
	return &WebhookDispatcher{repo: repo}
}

func (d *WebhookDispatcher) Fire(ctx context.Context, event string, payload any) {
	if d == nil || d.repo == nil {
		return
	}

	webhooks, err := d.repo.SelectWebhooks(ctx)
	if err != nil {
		log.Printf("webhook dispatcher: failed to select webhooks: %v", err)
		return
	}

	for i := range webhooks {
		wh := webhooks[i]
		if !wh.Enabled || !wh.MatchesEvent(event) {
			continue
		}

		go d.send(wh, event, payload)
	}
}

// Send is exported so the queue worker can call it directly for a specific webhook.
func (d *WebhookDispatcher) send(wh Webhook, event string, data any) {
	body := WebhookPayload{
		Event:     event,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Data:      data,
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		log.Printf("webhook dispatcher: failed to marshal payload for webhook %s: %v", wh.ID, err)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), webhookTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, wh.URL, bytes.NewReader(jsonBody))
	if err != nil {
		log.Printf("webhook dispatcher: failed to create request for webhook %s: %v", wh.ID, err)
		d.recordFailure(wh)
		return
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Webhook-Event", event)

	if wh.Secret != "" {
		sig := computeHMAC(jsonBody, wh.Secret)
		req.Header.Set("X-Webhook-Signature", sig)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("webhook dispatcher: request failed for webhook %s: %v", wh.ID, err)
		d.recordFailure(wh)
		return
	}
	defer resp.Body.Close()

	d.recordResult(wh, resp.StatusCode)
}

func (d *WebhookDispatcher) recordResult(wh Webhook, statusCode int) {
	wh.LastTriggered = time.Now().UTC()
	wh.LastStatus = statusCode
	wh.UpdatedAt = time.Now().UTC()

	if statusCode >= 200 && statusCode < 300 {
		wh.FailureCount = 0
	} else {
		wh.FailureCount++
		if wh.FailureCount >= maxConsecutiveFails {
			wh.Enabled = false
			log.Printf("webhook dispatcher: auto-disabled webhook %s after %d consecutive failures", wh.ID, wh.FailureCount)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := d.repo.UpdateWebhook(ctx, &wh); err != nil {
		log.Printf("webhook dispatcher: failed to update webhook %s: %v", wh.ID, err)
	}
}

func (d *WebhookDispatcher) recordFailure(wh Webhook) {
	wh.LastTriggered = time.Now().UTC()
	wh.LastStatus = 0
	wh.FailureCount++
	wh.UpdatedAt = time.Now().UTC()

	if wh.FailureCount >= maxConsecutiveFails {
		wh.Enabled = false
		log.Printf("webhook dispatcher: auto-disabled webhook %s after %d consecutive failures", wh.ID, wh.FailureCount)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := d.repo.UpdateWebhook(ctx, &wh); err != nil {
		log.Printf("webhook dispatcher: failed to update webhook %s: %v", wh.ID, err)
	}
}

func computeHMAC(message []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(message)

	return fmt.Sprintf("sha256=%s", hex.EncodeToString(mac.Sum(nil)))
}
