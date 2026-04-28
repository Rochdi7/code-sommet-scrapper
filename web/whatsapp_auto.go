//go:build windows

package web

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/atotto/clipboard"
	"github.com/micmonay/keybd_event"
)

// ---------------------------------------------------------------------------
// WhatsApp Desktop auto-sender
//
// Drives the locally-installed WhatsApp Desktop app via the `whatsapp://send`
// URI scheme, then simulates an OS-level Enter keypress to actually send.
// No QR / bridge connection required: uses the existing WhatsApp session on
// the host machine. Much harder for WhatsApp to detect as automation than
// whatsapp-web.js, because the messages flow through the real Desktop client.
//
// IMPORTANT: this must run on the same machine where WhatsApp Desktop is
// installed (not inside Docker). The randomized delays keep sending pace
// human-like to avoid rate-limit flags.
// ---------------------------------------------------------------------------

type waAutoMessage struct {
	Phone    string `json:"phone"`
	Business string `json:"business"`
	Message  string `json:"message"`
	Status   string `json:"status"` // pending | sending | sent | failed | skipped
	Error    string `json:"error,omitempty"`
	SentAt   string `json:"sentAt,omitempty"`
}

type waAutoCampaign struct {
	ID             string          `json:"id"`
	Name           string          `json:"name"`
	Status         string          `json:"status"` // running | paused | completed | stopped
	Total          int             `json:"total"`
	Sent           int             `json:"sent"`
	Failed         int             `json:"failed"`
	DelayMin       int             `json:"delayMin"`
	DelayMax       int             `json:"delayMax"`
	LaunchWait     int             `json:"launchWait"`
	AttachmentPath string          `json:"attachmentPath,omitempty"`
	Messages       []waAutoMessage `json:"messages"`
	CreatedAt      string          `json:"createdAt"`
	UpdatedAt      string          `json:"updatedAt"`
}

type waAutoState struct {
	mu          sync.Mutex
	running     bool
	paused      bool
	aborted     bool
	current     string
	nextSendAt  int64
	campaign    *waAutoCampaign
	persistPath string
}

var autoState = &waAutoState{}

func waAutoStatePath() string {
	// Writable regardless of working dir: stick next to the binary / in cwd.
	dir, err := os.Getwd()
	if err != nil {
		dir = "."
	}
	return filepath.Join(dir, "whatsapp-auto-campaigns.json")
}

func (st *waAutoState) load() []waAutoCampaign {
	if st.persistPath == "" {
		st.persistPath = waAutoStatePath()
	}
	data, err := os.ReadFile(st.persistPath)
	if err != nil {
		return nil
	}
	var list []waAutoCampaign
	if err := json.Unmarshal(data, &list); err != nil {
		return nil
	}
	return list
}

func (st *waAutoState) save(list []waAutoCampaign) {
	if st.persistPath == "" {
		st.persistPath = waAutoStatePath()
	}
	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(st.persistPath, data, 0o644)
}

func (st *waAutoState) persistCurrent() {
	if st.campaign == nil {
		return
	}
	list := st.load()
	st.campaign.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	found := false
	for i, c := range list {
		if c.ID == st.campaign.ID {
			list[i] = *st.campaign
			found = true
			break
		}
	}
	if !found {
		list = append([]waAutoCampaign{*st.campaign}, list...)
	}
	st.save(list)
}

// ---------------------------------------------------------------------------
// OS automation primitives
// ---------------------------------------------------------------------------

func launchWhatsAppURL(target string) error {
	switch runtime.GOOS {
	case "windows":
		// rundll32 with url.dll,FileProtocolHandler is the canonical Windows
		// URL-protocol invoker. More reliable than `cmd /c start` at actually
		// passing the URI payload to the registered handler (WhatsApp Desktop),
		// which prevents the app from just coming to focus on the last-open
		// chat without navigating.
		return exec.Command("rundll32.exe", "url.dll,FileProtocolHandler", target).Start()
	case "darwin":
		return exec.Command("open", target).Start()
	default:
		return exec.Command("xdg-open", target).Start()
	}
}

func pressEnter() error {
	kb, err := keybd_event.NewKeyBonding()
	if err != nil {
		return err
	}
	if runtime.GOOS == "linux" {
		// uinput needs a brief warm-up on Linux.
		time.Sleep(500 * time.Millisecond)
	}
	kb.SetKeys(keybd_event.VK_ENTER)
	return kb.Launching()
}

// pressCtrlV simulates Ctrl+V (paste).
func pressCtrlV() error {
	kb, err := keybd_event.NewKeyBonding()
	if err != nil {
		return err
	}
	if runtime.GOOS == "linux" {
		time.Sleep(500 * time.Millisecond)
	}
	kb.HasCTRL(true)
	kb.SetKeys(keybd_event.VK_V)
	return kb.Launching()
}

// primeChatInputFocus nudges WhatsApp Desktop to focus the message input box.
// Opening a chat via whatsapp:// URI sometimes leaves focus on the sidebar,
// so a subsequent Ctrl+V pastes into nothing. Typing a single space and then
// backspacing it reliably lands focus on the message input (WA Desktop auto-
// focuses the input on any printable keypress) without leaving any stray text.
func primeChatInputFocus() error {
	kb, err := keybd_event.NewKeyBonding()
	if err != nil {
		return err
	}
	if runtime.GOOS == "linux" {
		time.Sleep(500 * time.Millisecond)
	}
	kb.SetKeys(keybd_event.VK_SPACE)
	if err := kb.Launching(); err != nil {
		return err
	}
	time.Sleep(200 * time.Millisecond)

	kb2, err := keybd_event.NewKeyBonding()
	if err != nil {
		return err
	}
	kb2.SetKeys(keybd_event.VK_BACKSPACE)
	return kb2.Launching()
}

// setClipboardFile puts a file reference on the Windows clipboard (CF_HDROP).
// When pasted into WhatsApp Desktop, this triggers the file-attachment preview
// dialog (same behavior as copying a file in Explorer and pasting into a chat).
// Implemented via PowerShell's Set-Clipboard -Path — slower than a syscall but
// avoids the Win32 CF_HDROP boilerplate. Expect ~500–800 ms overhead per call.
func setClipboardFile(path string) error {
	if runtime.GOOS != "windows" {
		return fmt.Errorf("file clipboard is only supported on Windows")
	}
	// Escape single quotes for PowerShell single-quoted strings: ' → ''
	escaped := strings.ReplaceAll(path, "'", "''")
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command",
		"Set-Clipboard -Path '"+escaped+"'")
	return cmd.Run()
}

// clearInputField presses Ctrl+A then Delete to wipe any existing draft text
// in the focused message input. Without this, a leftover draft from a prior
// session would be prepended to the pasted message.
func clearInputField() error {
	ka, err := keybd_event.NewKeyBonding()
	if err != nil {
		return err
	}
	if runtime.GOOS == "linux" {
		time.Sleep(500 * time.Millisecond)
	}
	ka.HasCTRL(true)
	ka.SetKeys(keybd_event.VK_A)
	if err := ka.Launching(); err != nil {
		return err
	}
	time.Sleep(150 * time.Millisecond)

	kd, err := keybd_event.NewKeyBonding()
	if err != nil {
		return err
	}
	kd.SetKeys(keybd_event.VK_DELETE)
	return kd.Launching()
}

// isFaxNumber returns true for Moroccan landline/fax numbers (they aren't on
// WhatsApp). Mobile numbers start with 06 / 07 (local) or 2126 / 2127 (intl).
// Fax/landline start with 05 / 2125.
func isFaxNumber(raw string) bool {
	s := strings.TrimSpace(raw)
	s = strings.NewReplacer(" ", "", "-", "", "(", "", ")", "", "+", "").Replace(s)
	return strings.HasPrefix(s, "2125") || (len(s) == 10 && strings.HasPrefix(s, "05"))
}

func cleanPhoneForURI(raw string) string {
	s := strings.TrimSpace(raw)
	// Strip common separators
	s = strings.NewReplacer(" ", "", "-", "", "(", "", ")", "", "+", "").Replace(s)
	// Local Moroccan numbers → E.164 without +
	if len(s) == 10 && (strings.HasPrefix(s, "05") || strings.HasPrefix(s, "06") || strings.HasPrefix(s, "07")) {
		s = "212" + s[1:]
	}
	return s
}

// buildWhatsAppURI builds a `whatsapp://send?phone=X` URI. We deliberately
// omit the `text=` param because recent WhatsApp Desktop builds ignore it —
// the message is placed via clipboard paste after the chat opens.
func buildWhatsAppURI(phone string) string {
	p := cleanPhoneForURI(phone)
	v := url.Values{}
	v.Set("phone", p)
	return "whatsapp://send?" + v.Encode()
}

// ---------------------------------------------------------------------------
// Main loop
// ---------------------------------------------------------------------------

func (st *waAutoState) runLoop() {
	for i := range st.campaign.Messages {
		st.mu.Lock()
		if st.aborted {
			st.campaign.Status = "stopped"
			st.mu.Unlock()
			break
		}
		st.mu.Unlock()

		// Respect pause
		for {
			st.mu.Lock()
			paused := st.paused
			aborted := st.aborted
			st.mu.Unlock()
			if aborted {
				break
			}
			if !paused {
				break
			}
			time.Sleep(500 * time.Millisecond)
		}

		st.mu.Lock()
		if st.aborted {
			st.campaign.Status = "stopped"
			st.mu.Unlock()
			break
		}
		entry := &st.campaign.Messages[i]
		if entry.Status == "sent" || entry.Status == "failed" || entry.Status == "skipped" {
			st.mu.Unlock()
			continue
		}

		// Skip Moroccan fax/landline numbers (05… / 2125…) — not on WhatsApp.
		if isFaxNumber(entry.Phone) {
			entry.Status = "skipped"
			entry.Error = "fax/landline (no WhatsApp)"
			st.campaign.Failed++
			st.persistCurrent()
			st.mu.Unlock()
			continue
		}

		entry.Status = "sending"
		st.current = entry.Phone
		launchWait := st.campaign.LaunchWait
		if launchWait < 3 {
			launchWait = 7
		}
		minD := st.campaign.DelayMin
		maxD := st.campaign.DelayMax
		st.persistCurrent()
		st.mu.Unlock()

		attachPath := st.campaign.AttachmentPath

		// 1. Pre-load clipboard.
		//    If an attachment is configured: put the FILE on the clipboard.
		//    WhatsApp Desktop will open its attach-preview dialog with a caption
		//    field; we'll paste the text into the caption afterwards.
		//    Otherwise: put the message text on the clipboard directly.
		if attachPath != "" {
			if err := setClipboardFile(attachPath); err != nil {
				st.mu.Lock()
				entry.Status = "failed"
				entry.Error = "file clipboard failed: " + err.Error()
				st.campaign.Failed++
				st.persistCurrent()
				st.mu.Unlock()
				continue
			}
		} else {
			if err := clipboard.WriteAll(entry.Message); err != nil {
				st.mu.Lock()
				entry.Status = "failed"
				entry.Error = "clipboard write failed: " + err.Error()
				st.campaign.Failed++
				st.persistCurrent()
				st.mu.Unlock()
				continue
			}
		}

		// 2. Launch whatsapp://send?phone=... — opens/focuses WhatsApp Desktop
		//    with the chat open.
		uri := buildWhatsAppURI(entry.Phone)
		if err := launchWhatsAppURL(uri); err != nil {
			st.mu.Lock()
			entry.Status = "failed"
			entry.Error = "launch failed: " + err.Error()
			st.campaign.Failed++
			st.persistCurrent()
			st.mu.Unlock()
			continue
		}

		// 3. Give WhatsApp Desktop time to open the chat and focus the input box.
		time.Sleep(time.Duration(launchWait) * time.Second)

		// 3b. Prime input focus: space + backspace.
		if err := primeChatInputFocus(); err != nil {
			st.mu.Lock()
			entry.Status = "failed"
			entry.Error = "focus prime failed: " + err.Error()
			st.campaign.Failed++
			st.persistCurrent()
			st.mu.Unlock()
			continue
		}
		time.Sleep(400 * time.Millisecond)

		// 3c. Wipe any leftover draft text in the input (Ctrl+A → Delete).
		if err := clearInputField(); err != nil {
			st.mu.Lock()
			entry.Status = "failed"
			entry.Error = "clear input failed: " + err.Error()
			st.campaign.Failed++
			st.persistCurrent()
			st.mu.Unlock()
			continue
		}
		time.Sleep(300 * time.Millisecond)

		// 4. Paste (Ctrl+V). With a file on the clipboard, WhatsApp Desktop
		//    opens the attach-preview dialog; with text, it fills the input.
		if err := pressCtrlV(); err != nil {
			st.mu.Lock()
			entry.Status = "failed"
			entry.Error = "paste failed: " + err.Error()
			st.campaign.Failed++
			st.persistCurrent()
			st.mu.Unlock()
			continue
		}

		// 5. If attaching: wait for preview dialog, then swap clipboard to
		//    text and paste it into the caption field, then Enter to send.
		//    Else: brief settle, then Enter.
		if attachPath != "" {
			// Wait for attach-preview dialog to appear and receive focus on
			// its caption text field. WhatsApp animates this; 2s is safe.
			time.Sleep(2 * time.Second)

			// Swap clipboard to caption text.
			if err := clipboard.WriteAll(entry.Message); err != nil {
				st.mu.Lock()
				entry.Status = "failed"
				entry.Error = "caption clipboard failed: " + err.Error()
				st.campaign.Failed++
				st.persistCurrent()
				st.mu.Unlock()
				continue
			}
			time.Sleep(300 * time.Millisecond)

			// Paste caption into the now-focused caption field.
			if err := pressCtrlV(); err != nil {
				st.mu.Lock()
				entry.Status = "failed"
				entry.Error = "caption paste failed: " + err.Error()
				st.campaign.Failed++
				st.persistCurrent()
				st.mu.Unlock()
				continue
			}
			time.Sleep(800 * time.Millisecond)
		} else {
			time.Sleep(800 * time.Millisecond)
		}

		// 6. Press Enter — sends the message (with or without attachment).
		if err := pressEnter(); err != nil {
			st.mu.Lock()
			entry.Status = "failed"
			entry.Error = "enter failed: " + err.Error()
			st.campaign.Failed++
			st.persistCurrent()
			st.mu.Unlock()
			continue
		}

		st.mu.Lock()
		entry.Status = "sent"
		entry.SentAt = time.Now().UTC().Format(time.RFC3339)
		st.campaign.Sent++
		st.persistCurrent()

		// 4. Randomized human-like delay before next send (skip on last).
		pending := 0
		for _, m := range st.campaign.Messages {
			if m.Status == "pending" {
				pending++
			}
		}
		if pending > 0 && !st.aborted {
			delta := maxD - minD
			if delta < 1 {
				delta = 1
			}
			wait := minD + rand.Intn(delta+1)
			st.nextSendAt = time.Now().Add(time.Duration(wait) * time.Second).Unix()
			st.mu.Unlock()

			// Sleep in small ticks so pause/abort react quickly.
			deadline := time.Now().Add(time.Duration(wait) * time.Second)
			for time.Now().Before(deadline) {
				st.mu.Lock()
				if st.aborted {
					st.mu.Unlock()
					break
				}
				paused := st.paused
				st.mu.Unlock()
				if paused {
					// Extend deadline while paused.
					deadline = deadline.Add(500 * time.Millisecond)
				}
				time.Sleep(500 * time.Millisecond)
			}
			st.mu.Lock()
			st.nextSendAt = 0
			st.mu.Unlock()
		} else {
			st.mu.Unlock()
		}
	}

	// Finalize
	st.mu.Lock()
	st.running = false
	st.current = ""
	st.nextSendAt = 0
	if st.campaign != nil {
		pending := 0
		for _, m := range st.campaign.Messages {
			if m.Status == "pending" {
				pending++
			}
		}
		switch {
		case st.aborted:
			st.campaign.Status = "stopped"
		case pending > 0:
			st.campaign.Status = "paused"
		default:
			st.campaign.Status = "completed"
		}
		st.persistCurrent()
	}
	st.aborted = false
	st.paused = false
	st.mu.Unlock()
}

// ---------------------------------------------------------------------------
// HTTP handlers
// ---------------------------------------------------------------------------

type waAutoStartRequest struct {
	Name           string          `json:"name"`
	DelayMin       int             `json:"delayMin"`
	DelayMax       int             `json:"delayMax"`
	LaunchWait     int             `json:"launchWait"`
	AttachmentPath string          `json:"attachmentPath"`
	Messages       []waAutoMessage `json:"messages"`
}

func (s *Server) whatsappAutoStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req waAutoStartRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		renderJSON(w, http.StatusBadRequest, apiError{Code: 400, Message: err.Error()})
		return
	}
	if len(req.Messages) == 0 {
		renderJSON(w, http.StatusBadRequest, apiError{Code: 400, Message: "messages array is required"})
		return
	}

	autoState.mu.Lock()
	if autoState.running {
		autoState.mu.Unlock()
		renderJSON(w, http.StatusConflict, apiError{Code: 409, Message: "A campaign is already running"})
		return
	}

	minD := req.DelayMin
	if minD < 30 {
		minD = 45
	}
	maxD := req.DelayMax
	if maxD <= minD {
		maxD = minD + 30
	}
	launchWait := req.LaunchWait
	if launchWait < 3 {
		launchWait = 5
	}

	// Pre-clean phones; drop rows with empty phone.
	var msgs []waAutoMessage
	for _, m := range req.Messages {
		if strings.TrimSpace(m.Phone) == "" {
			continue
		}
		status := "pending"
		msgs = append(msgs, waAutoMessage{
			Phone:    m.Phone,
			Business: m.Business,
			Message:  m.Message,
			Status:   status,
		})
	}
	if len(msgs) == 0 {
		autoState.mu.Unlock()
		renderJSON(w, http.StatusBadRequest, apiError{Code: 400, Message: "no valid phone numbers"})
		return
	}

	now := time.Now().UTC().Format(time.RFC3339)
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = "Desktop campaign " + time.Now().Format("2006-01-02 15:04")
	}

	// Validate attachment path if provided — fail fast so the user knows
	// before the campaign starts rather than after the first message fails.
	attachPath := strings.TrimSpace(req.AttachmentPath)
	if attachPath != "" {
		if _, err := os.Stat(attachPath); err != nil {
			autoState.mu.Unlock()
			renderJSON(w, http.StatusBadRequest, apiError{
				Code:    400,
				Message: "attachment file not found: " + attachPath,
			})
			return
		}
	}

	camp := &waAutoCampaign{
		ID:             fmt.Sprintf("auto_%d", time.Now().UnixNano()),
		Name:           name,
		Status:         "running",
		Total:          len(msgs),
		DelayMin:       minD,
		DelayMax:       maxD,
		LaunchWait:     launchWait,
		AttachmentPath: attachPath,
		Messages:       msgs,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	autoState.campaign = camp
	autoState.running = true
	autoState.paused = false
	autoState.aborted = false
	autoState.persistCurrent()
	autoState.mu.Unlock()

	go autoState.runLoop()

	renderJSON(w, http.StatusOK, map[string]any{
		"started":    true,
		"campaignId": camp.ID,
		"total":      camp.Total,
	})
}

func (s *Server) whatsappAutoStatus(w http.ResponseWriter, r *http.Request) {
	autoState.mu.Lock()
	defer autoState.mu.Unlock()

	resp := map[string]any{
		"running": autoState.running,
		"paused":  autoState.paused,
		"current": autoState.current,
	}
	if autoState.nextSendAt > 0 {
		remaining := autoState.nextSendAt - time.Now().Unix()
		if remaining < 0 {
			remaining = 0
		}
		resp["next_send_in"] = remaining
	}
	if autoState.campaign != nil {
		resp["campaignId"] = autoState.campaign.ID
		resp["name"] = autoState.campaign.Name
		resp["status"] = autoState.campaign.Status
		resp["total"] = autoState.campaign.Total
		resp["sent"] = autoState.campaign.Sent
		resp["failed"] = autoState.campaign.Failed
		resp["messages"] = autoState.campaign.Messages
	}
	renderJSON(w, http.StatusOK, resp)
}

func (s *Server) whatsappAutoPause(w http.ResponseWriter, r *http.Request) {
	autoState.mu.Lock()
	defer autoState.mu.Unlock()
	if !autoState.running {
		renderJSON(w, http.StatusBadRequest, apiError{Code: 400, Message: "no campaign running"})
		return
	}
	autoState.paused = true
	if autoState.campaign != nil {
		autoState.campaign.Status = "paused"
		autoState.persistCurrent()
	}
	renderJSON(w, http.StatusOK, map[string]bool{"paused": true})
}

func (s *Server) whatsappAutoResume(w http.ResponseWriter, r *http.Request) {
	autoState.mu.Lock()
	defer autoState.mu.Unlock()
	if !autoState.running {
		renderJSON(w, http.StatusBadRequest, apiError{Code: 400, Message: "no campaign running"})
		return
	}
	autoState.paused = false
	if autoState.campaign != nil {
		autoState.campaign.Status = "running"
		autoState.persistCurrent()
	}
	renderJSON(w, http.StatusOK, map[string]bool{"resumed": true})
}

func (s *Server) whatsappAutoStop(w http.ResponseWriter, r *http.Request) {
	autoState.mu.Lock()
	defer autoState.mu.Unlock()
	if !autoState.running {
		renderJSON(w, http.StatusOK, map[string]bool{"stopped": false})
		return
	}
	autoState.aborted = true
	autoState.paused = false
	renderJSON(w, http.StatusOK, map[string]bool{"stopped": true})
}

func (s *Server) whatsappAutoCampaigns(w http.ResponseWriter, r *http.Request) {
	list := autoState.load()
	// Strip the heavy messages slice from the summary.
	type summary struct {
		ID             string `json:"id"`
		Name           string `json:"name"`
		Status         string `json:"status"`
		Source         string `json:"source"`
		Total          int    `json:"total"`
		Sent           int    `json:"sent"`
		Failed         int    `json:"failed"`
		Pending        int    `json:"pending"`
		AttachmentPath string `json:"attachmentPath,omitempty"`
		CreatedAt      string `json:"createdAt"`
		UpdatedAt      string `json:"updatedAt"`
	}
	out := make([]summary, 0, len(list))
	for _, c := range list {
		pending := 0
		for _, m := range c.Messages {
			if m.Status == "pending" {
				pending++
			}
		}
		out = append(out, summary{
			ID:             c.ID,
			Name:           c.Name,
			Status:         c.Status,
			Source:         "desktop",
			Total:          c.Total,
			Sent:           c.Sent,
			Failed:         c.Failed,
			Pending:        pending,
			AttachmentPath: c.AttachmentPath,
			CreatedAt:      c.CreatedAt,
			UpdatedAt:      c.UpdatedAt,
		})
	}
	renderJSON(w, http.StatusOK, out)
}

// whatsappAutoResumeCampaign restarts the send loop for a previously paused /
// stopped campaign. Reads the stored campaign, flips any "sending" back to
// "pending" (in case we were killed mid-send), and resumes runLoop.
func (s *Server) whatsappAutoResumeCampaign(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		CampaignID string `json:"campaignId"`
		DelayMin   int    `json:"delayMin"`
		DelayMax   int    `json:"delayMax"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		renderJSON(w, http.StatusBadRequest, apiError{Code: 400, Message: err.Error()})
		return
	}
	if req.CampaignID == "" {
		renderJSON(w, http.StatusBadRequest, apiError{Code: 400, Message: "campaignId is required"})
		return
	}

	autoState.mu.Lock()
	if autoState.running {
		autoState.mu.Unlock()
		renderJSON(w, http.StatusConflict, apiError{Code: 409, Message: "A campaign is already running"})
		return
	}

	list := autoState.load()
	var target *waAutoCampaign
	for i := range list {
		if list[i].ID == req.CampaignID {
			target = &list[i]
			break
		}
	}
	if target == nil {
		autoState.mu.Unlock()
		renderJSON(w, http.StatusNotFound, apiError{Code: 404, Message: "campaign not found"})
		return
	}

	// Reset any stale "sending" status back to "pending" and count what's left.
	pending := 0
	sent := 0
	failed := 0
	for i := range target.Messages {
		if target.Messages[i].Status == "sending" {
			target.Messages[i].Status = "pending"
		}
		switch target.Messages[i].Status {
		case "pending":
			pending++
		case "sent":
			sent++
		case "failed", "skipped":
			failed++
		}
	}
	if pending == 0 {
		autoState.mu.Unlock()
		renderJSON(w, http.StatusBadRequest, apiError{Code: 400, Message: "no pending messages to resume"})
		return
	}

	// Optional delay overrides.
	if req.DelayMin >= 30 {
		target.DelayMin = req.DelayMin
	}
	if req.DelayMax > target.DelayMin {
		target.DelayMax = req.DelayMax
	}

	target.Status = "running"
	target.Sent = sent
	target.Failed = failed

	// Hand the campaign pointer to the live state and kick off the loop.
	resumed := *target // copy to avoid aliasing the slice element
	autoState.campaign = &resumed
	autoState.running = true
	autoState.paused = false
	autoState.aborted = false
	autoState.persistCurrent()
	autoState.mu.Unlock()

	go autoState.runLoop()

	renderJSON(w, http.StatusOK, map[string]any{
		"resumed":    true,
		"campaignId": resumed.ID,
		"remaining":  pending,
	})
}

// whatsappAutoContactedPhones returns the set of phone numbers that have been
// successfully sent to across ALL past campaigns (normalized international
// format, no '+'). The frontend uses this to flag leads as "already
// contacted" during import — the user can still include them, but they'll
// know.
func (s *Server) whatsappAutoContactedPhones(w http.ResponseWriter, r *http.Request) {
	list := autoState.load()
	seen := make(map[string]bool)
	for _, c := range list {
		for _, m := range c.Messages {
			if m.Status != "sent" {
				continue
			}
			norm := cleanPhoneForURI(m.Phone)
			if norm != "" {
				seen[norm] = true
			}
		}
	}
	phones := make([]string, 0, len(seen))
	for p := range seen {
		phones = append(phones, p)
	}
	renderJSON(w, http.StatusOK, map[string]any{
		"phones": phones,
		"count":  len(phones),
	})
}

// whatsappAutoDeleteCampaign removes a persisted campaign from disk. Does not
// allow deleting the currently-running campaign.
func (s *Server) whatsappAutoDeleteCampaign(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete && r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id := r.URL.Query().Get("id")
	if id == "" {
		renderJSON(w, http.StatusBadRequest, apiError{Code: 400, Message: "id is required"})
		return
	}
	autoState.mu.Lock()
	defer autoState.mu.Unlock()
	if autoState.running && autoState.campaign != nil && autoState.campaign.ID == id {
		renderJSON(w, http.StatusConflict, apiError{Code: 409, Message: "cannot delete running campaign"})
		return
	}
	list := autoState.load()
	out := list[:0]
	found := false
	for _, c := range list {
		if c.ID == id {
			found = true
			continue
		}
		out = append(out, c)
	}
	if !found {
		renderJSON(w, http.StatusNotFound, apiError{Code: 404, Message: "campaign not found"})
		return
	}
	autoState.save(out)
	renderJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}
