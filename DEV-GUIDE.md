# CodeSommet Lead Scraper - Developer & Setup Guide

This guide explains how to run, modify, and rebuild the project.
Written for both humans and AI agents.

---

## Overview

A full lead-generation platform with:
- **Google Maps Scraper** - Extract business data (phone, email, website, reviews, etc.)
- **WhatsApp Sender** - Bulk WhatsApp outreach with templates, per-row customization, delay settings, duplicate detection, and message history
- **Email Sender** - Email outreach with SMTP integration
- **Leads CRM** - View, filter, and manage scraped leads
- **Dashboard** - Analytics overview
- **Schedules** - Recurring scrape jobs
- **Webhooks** - External integrations
- **Proxy Monitor** - Manage proxy rotation

---

## Quick Start (docker-compose)

**Requirement:** Docker Desktop installed and running.

```bash
cd C:\Users\ASUS\Desktop\Projects\google-maps-scraper
docker compose up -d
```

This starts 3 services:
| Service | Port | Description |
|---------|------|-------------|
| `scraper` | 8080 | Main Go app (Web UI + API + scraping engine) |
| `wa-bridge` | 3001 | WhatsApp bridge (Node.js + whatsapp-web.js + Chromium) |
| `email-bridge` | 3002 | Email bridge (Node.js + SMTP) |

Open http://localhost:8080

### Rebuild after code changes

```bash
docker compose up -d --build
```

### Stop everything

```bash
docker compose down
```

---

## Project Architecture

### Services

```
docker-compose.yml
├── scraper        (Go binary, Playwright+Chromium, port 8080)
│   └── Serves Web UI, REST API, runs scrape jobs, stores in SQLite
├── wa-bridge      (Node.js, whatsapp-web.js+Chromium, port 3001)
│   └── REST API for WhatsApp: QR auth, send, bulk-send, status
└── email-bridge   (Node.js, port 3002)
    └── REST API for email: SMTP config, send, bulk-send
```

### How Scraping Works

1. User opens http://localhost:8080 or calls API
2. Go binary serves HTML UI and accepts scrape jobs via form/API
3. Jobs stored in SQLite (`gmapsdata/jobs.db`)
4. Background worker launches Playwright/Chromium, scrapes Google Maps
5. Results saved as CSV in `gmapsdata/{job-id}.csv`
6. Results auto-imported into Leads CRM
7. User can export CSV, send WhatsApp/Email campaigns from results

### Run Mode Detection (`runner/runner.go`)

```
No input + No DSN  -> Web mode    (HTTP server + job queue)
-input provided    -> File mode   (CLI, reads queries.txt, writes CSV)
-dsn provided      -> Database    (PostgreSQL as job queue)
-aws-lambda        -> Lambda mode (AWS Lambda execution)
```

---

## File Structure

```
google-maps-scraper/
│
├── main.go                          # Entry point
├── docker-compose.yml               # All 3 services (scraper, wa-bridge, email-bridge)
├── Dockerfile                       # Main scraper Dockerfile (Go + Playwright + Chromium)
│
├── runner/                          # Job runner engine
│   ├── runner.go                    # Config parsing, CLI flags, run mode detection
│   ├── jobs.go                      # Creates seed scraping jobs from keywords
│   └── webrunner/webrunner.go       # Web mode: HTTP server + background worker
│
├── web/                             # === WEB UI & API ===
│   ├── web.go                       # HTTP routes & handlers
│   │   Routes:
│   │     GET  /                     # Scraper page (form + job table)
│   │     GET  /dashboard            # Analytics dashboard
│   │     GET  /leads                # Leads CRM
│   │     GET  /whatsapp             # WhatsApp Sender
│   │     GET  /email                # Email Sender
│   │     GET  /schedules            # Scheduled jobs
│   │     GET  /proxies              # Proxy monitor
│   │     GET  /webhooks             # Webhook config
│   │     POST /scrape               # Create new scrape job from form
│   │     GET  /jobs                 # Job rows HTML (HTMX polling)
│   │     GET  /results-preview?id=  # CSV data as JSON (for import)
│   │     GET  /api/v1/jobs          # JSON API: list all jobs
│   │     POST /api/v1/jobs          # JSON API: create job
│   │     GET  /api/v1/jobs/{id}     # JSON API: get single job
│   │     DELETE /api/v1/jobs/{id}   # JSON API: delete job
│   │     GET  /api/docs             # ReDoc API documentation
│   │
│   ├── job.go                       # Job & JobData structs
│   │   JobData fields: Keywords, Lang, Zoom, Lat, Lon, FastMode,
│   │                   Radius, Depth, Email (default: true), MaxTime,
│   │                   Proxies, Country, MaxResults
│   │
│   ├── service.go                   # Business logic: Create, Update, Delete, GetCSV
│   ├── lead.go                      # Lead struct (Title, Phone, Email, Website, etc.)
│   ├── leads_handlers.go            # Leads CRM page handlers
│   ├── schedule.go / schedulehandlers.go  # Scheduling logic
│   ├── cron.go                      # Cron job runner
│   ├── proxy.go                     # Proxy management
│   ├── webhook.go / webhook_dispatcher.go # Webhook handlers
│   │
│   ├── sqlite/sqlite.go            # SQLite repository (gmapsdata/jobs.db)
│   │
│   └── static/                      # === EMBEDDED STATIC FILES (//go:embed) ===
│       ├── css/main.css             # All styles (layout, components, responsive)
│       ├── templates/
│       │   ├── index.html           # Scraper page: form + job table + filters
│       │   ├── dashboard.html       # Dashboard: analytics & stats
│       │   ├── leads.html           # Leads CRM: filter, search, manage leads
│       │   ├── whatsapp.html        # WhatsApp Sender (see below for details)
│       │   ├── email.html           # Email Sender
│       │   ├── schedules.html       # Scheduled scrape jobs
│       │   ├── proxies.html         # Proxy status monitor
│       │   ├── webhooks.html        # Webhook configuration
│       │   ├── job_row.html         # Single job row template
│       │   ├── job_rows.html        # All job rows template (HTMX)
│       │   └── redoc.html           # API docs page
│       └── spec/spec.yaml           # OpenAPI 3.0 specification
│
├── gmaps/                           # Google Maps scraping core
│   ├── entry.go                     # Entry struct: 35+ fields per business
│   │   CsvHeaders(): input_id, link, title, category, address,
│   │                  open_hours, popular_times, website, phone,
│   │                  ..., emails, message
│   ├── searchjob.go                 # Search job: navigate + scroll Google Maps
│   ├── place.go                     # Place extraction: parse business data
│   ├── emailjob.go                  # Email extraction: crawl business website
│   └── reviews.go                   # Review extraction
│
├── wa-bridge/                       # === WHATSAPP BRIDGE (Node.js) ===
│   ├── Dockerfile                   # node:20-alpine + Chromium
│   ├── package.json                 # whatsapp-web.js, express, qrcode, cors
│   ├── index.js                     # REST API:
│   │   GET  /status                 # Connection status {connected, phone}
│   │   GET  /qr                     # QR code as data URI
│   │   POST /disconnect             # Logout from WhatsApp
│   │   POST /send                   # Send single message {phone, message}
│   │   POST /bulk-send              # Bulk send {messages[], delayMin, delayMax}
│   │   GET  /bulk-status            # Bulk send progress
│   │   POST /bulk-stop              # Abort bulk send
│   │
│   └── Uses: whatsapp-web.js (Puppeteer-based, real Chromium browser)
│            NOT baileys (had 405 protocol errors)
│
├── email-bridge/                    # === EMAIL BRIDGE (Node.js) ===
│   ├── Dockerfile
│   ├── package.json
│   └── index.js                     # REST API for SMTP email sending
│
├── gmapsdata/                       # Runtime data (created at runtime)
│   ├── jobs.db                      # SQLite database
│   └── {job-id}.csv                 # CSV result files per job
│
├── skills/                          # AI agent skill definitions
│   ├── google-maps-scraper/SKILL.md
│   ├── api/, automation/, cli/, data/, docker/, golang/
│   ├── lead-gen/, proxies/, scraping/, caveman/
│
├── queries.txt                      # Input queries for CLI mode
├── results.csv                      # Output for CLI mode
├── DEV-GUIDE.md                     # This file
└── how to run.txt                   # Quick-start instructions
```

---

## WhatsApp Sender Features (`web/static/templates/whatsapp.html`)

### Connection
- Uses `whatsapp-web.js` via `wa-bridge` (port 3001)
- QR code displayed in browser, scan with phone
- Connection status polled every 2 seconds
- QR refreshes every 60 seconds

### Templates (15 templates in 3 languages)
All templates support variables: `{business}`, `{phone}`, `{email}`, `{category}`, `{address}`

| Category | FR | Darija | EN |
|----------|-----|--------|-----|
| No Website (Build) | nosite-fr | nosite-darija | nosite-en |
| Has Website (Redesign+SEO) | redesign-fr | redesign-darija | redesign-en |
| E-commerce | ecommerce-fr | ecommerce-darija | ecommerce-en |
| Social Media | social-fr | social-darija | social-en |
| Follow-up (Already Contacted) | followup-fr | followup-darija | followup-en |

### Recipients (3 import modes)
- **Manual List** - paste phone numbers
- **Import from Scraper** - dropdown loads completed jobs from `/api/v1/jobs`
- **Import CSV** - upload CSV with `phone` column (optional: `title`, `emails`, `website`, `category`, `address`, `message`)

### Import Table
- Shows: Business, Phone, Email, Website, Template dropdown
- **Auto-detection**: no website -> "nosite-fr", has website -> "redesign-fr"
- **Per-row template override** via dropdown
- **Duplicate detection**: already-contacted phones get yellow "SENT" badge, auto-switched to follow-up template
- **Expand button** opens full-screen modal with all columns

### Send Campaign
- Configurable **delay between messages** (min/max in seconds, default 30-90)
- Sending **animation** with live counter
- **Completion banner** (green success or yellow with error count)
- Each recipient gets personalized message from their selected template

### Message History (separate tab)
- Stored in **localStorage** (persists across sessions)
- Filters: Status (All/Sent/Failed), Search (phone or business)
- Stats bar: Total, Sent, Failed counts
- Clear All button
- Badge on tab shows total count

### Two Page Tabs
- **Send Campaign** - compose + recipients + send controls + delivery status
- **Message History** - full-view filterable history table

---

## How to Make Changes

### Step-by-step:
1. Edit files in `web/static/` (HTML, CSS, JS) or `.go` files
2. Rebuild: `docker compose up -d --build`
3. Test at http://localhost:8080

**IMPORTANT:** Static files are embedded in Go binary via `//go:embed`.
Editing them has NO effect until you rebuild the Docker image.

### Common changes:

| I want to... | Edit this file |
|---|---|
| Change scraper form fields | `web/static/templates/index.html` |
| Change styles/layout | `web/static/css/main.css` |
| Change WhatsApp templates | `web/static/templates/whatsapp.html` (TEMPLATES object) |
| Change WhatsApp sender logic | `web/static/templates/whatsapp.html` (JS section) |
| Change WhatsApp bridge API | `wa-bridge/index.js` |
| Change email sender | `web/static/templates/email.html` + `email-bridge/index.js` |
| Change dashboard | `web/static/templates/dashboard.html` |
| Change leads CRM | `web/static/templates/leads.html` + `web/leads_handlers.go` |
| Add HTTP endpoint | `web/web.go` |
| Change form defaults | `web/web.go` (index handler, formData struct) |
| Change job fields | `web/job.go` (JobData struct) |
| Change CSV columns | `gmaps/entry.go` (CsvHeaders + CsvRow) |
| Change scraping behavior | `runner/webrunner/webrunner.go` |
| Change extracted data | `gmaps/entry.go`, `gmaps/place.go` |

### Template variables in index.html:
```
{{.Name}}, {{.MaxTime}}, {{.KeywordsString}}, {{.Language}},
{{.Zoom}}, {{.FastMode}}, {{.Radius}}, {{.Lat}}, {{.Lon}},
{{.Depth}}, {{.Email}}, {{.ProxiesString}}, {{.Country}}, {{.MaxResults}}
```

---

## API Endpoints

### Scraper API (`localhost:8080`)

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/api/v1/jobs` | GET | List all jobs (JSON) |
| `/api/v1/jobs` | POST | Create job (JSON) |
| `/api/v1/jobs/{id}` | GET | Get single job |
| `/api/v1/jobs/{id}` | DELETE | Delete job |
| `/api/v1/jobs/{id}/download` | GET | Download CSV |
| `/results-preview?id={id}` | GET | CSV data as JSON array |
| `/api/docs` | GET | OpenAPI documentation |

### WhatsApp Bridge API (`localhost:3001`)

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/status` | GET | `{connected, phone}` |
| `/qr` | GET | QR code data URI |
| `/send` | POST | `{phone, message}` |
| `/bulk-send` | POST | `{messages[], delayMin, delayMax}` |
| `/bulk-status` | GET | Progress: `{total, sent, failed, messages[]}` |
| `/bulk-stop` | POST | Abort bulk send |
| `/disconnect` | POST | Logout |

### Email Bridge API (`localhost:3002`)

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/status` | GET | Connection status |
| `/send` | POST | Send single email |
| `/bulk-send` | POST | Bulk email send |

---

## Key Technologies

| Component | Technology |
|-----------|------------|
| Language | Go 1.26+ |
| Browser engine | Playwright (Chromium) |
| Web framework | `net/http` + `html/template` |
| Frontend | HTMX 1.9.6 (no React/Vue/npm) |
| Database | SQLite (embedded) |
| WhatsApp | whatsapp-web.js + Puppeteer + Chromium |
| Email | Nodemailer + SMTP |
| Container | Docker + docker-compose |
| Static files | Go `//go:embed` |

---

## CLI Flags Reference

```
-input string         Path to queries file (one query per line)
-results string       Output file path (default: stdout)
-depth int            Scroll depth (default: 10)
-lang string          Language code (default: "en")
-email                Extract emails from business websites (default in web: true)
-c int                Concurrency (default: half CPU cores)
-json                 Output JSON instead of CSV
-exit-on-inactivity   Auto-exit after duration (e.g., "3m")
-data-folder string   Data folder for web mode (default: "webdata")
-addr string          Listen address (default: ":8080")
-zoom int             Zoom level 0-21 (default: 15)
-radius float         Search radius in meters (default: 10000)
-fast-mode            Quick mode
-proxies string       Comma-separated proxy list
-debug                Show browser window
-extra-reviews        Collect extended reviews
```

---

## Troubleshooting

| Problem | Solution |
|---------|----------|
| Port already in use | `docker compose down` then `docker compose up -d` |
| Git Bash path errors | Use PowerShell or prefix with `MSYS_NO_PATHCONV=1` |
| UI changes not showing | Must rebuild: `docker compose up -d --build` |
| WhatsApp QR not showing | Check `docker logs codesommet-wa-bridge` |
| WhatsApp "can't link device" | Remove old linked devices on phone (max 4) |
| Empty job dropdown | Need completed scrape jobs first |
| Docker DNS errors | Retry build or use `--network host` flag |
| Slow first build | Normal: downloads Go + Playwright + Chromium (~270MB) |
