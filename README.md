# CodeSommet Lead Scraper

**A full lead-generation platform** - scrape business data from Google Maps, then reach out via WhatsApp and Email, all from one interface.

![Example GIF](img/example.gif)

---

## Platform Features

| | |
|---|---|
| **Google Maps Scraper** | Extract 35+ data points: phone, email, website, reviews, coordinates |
| **WhatsApp Sender** | Bulk WhatsApp outreach with 15 templates (FR/Darija/EN), per-contact customization, delay settings, duplicate detection, message history |
| **Email Sender** | Email campaigns with SMTP integration |
| **Leads CRM** | View, filter, search, and manage scraped leads |
| **Dashboard** | Analytics and overview stats |
| **Import/Export** | Import from scraper results or CSV, export with pre-filled message column |
| **Anti-Spam** | Configurable delay (30-90s default), duplicate detection, follow-up templates for already-contacted clients |
| **Proxy Support** | Built-in SOCKS5/HTTP/HTTPS proxy rotation |

---

## Table of Contents

- [Quick Start](#quick-start)
- [Pages & URLs](#pages--urls)
- [WhatsApp Sender](#whatsapp-sender)
- [Installation](#installation)
- [Features](#features)
- [Extracted Data Points](#extracted-data-points)
- [Configuration](#configuration)
- [REST API](#rest-api)
- [Developer Guide](#developer-guide)
- [Performance](#performance)
- [License](#license)

---

## Quick Start

### docker-compose (Recommended)

Starts all 3 services (scraper + WhatsApp + Email):

```bash
cd C:\Users\ASUS\Desktop\Projects\google-maps-scraper
docker compose up -d
```

Then open http://localhost:8080

### Rebuild after code changes

```bash
docker compose up -d --build
```

### Single container (scraper only)

```bash
mkdir -p gmapsdata && docker run -v $PWD/gmapsdata:/gmapsdata -p 8080:8080 gosom/google-maps-scraper -data-folder /gmapsdata
```

### Command Line (no UI)

```bash
touch results.csv && docker run \
  -v $PWD/example-queries.txt:/example-queries \
  -v $PWD/results.csv:/results.csv \
  gosom/google-maps-scraper \
  -depth 1 -email \
  -input /example-queries \
  -results /results.csv \
  -exit-on-inactivity 3m
```

---

## Pages & URLs

| URL | Page | Description |
|-----|------|-------------|
| http://localhost:8080 | Scraper | Create scrape jobs, view results |
| http://localhost:8080/dashboard | Dashboard | Analytics overview |
| http://localhost:8080/leads | Leads CRM | Filter, search, manage leads |
| http://localhost:8080/whatsapp | WhatsApp Sender | Bulk messaging with templates |
| http://localhost:8080/email | Email Sender | Email campaigns |
| http://localhost:8080/schedules | Schedules | Recurring scrape jobs |
| http://localhost:8080/proxies | Proxy Monitor | Manage proxy rotation |
| http://localhost:8080/webhooks | Webhooks | External integrations |
| http://localhost:8080/api/docs | API Docs | OpenAPI documentation |

---

## WhatsApp Sender

### Templates (15 templates, 3 languages each)
- **No Website** (FR/Darija/EN) - "We build websites that get you clients"
- **Has Website - Redesign+SEO** (FR/Darija/EN) - "Your site is not optimized"
- **E-commerce** (FR/Darija/EN) - "+40% sales in 3 months"
- **Social Media** (FR/Darija/EN) - "Instagram/Facebook presence"
- **Follow-up** (FR/Darija/EN) - Polite relance for already-contacted clients

### Smart Features
- **Auto-detect template** based on whether business has a website
- **Per-row template override** in the import table
- **Duplicate detection** - flags already-contacted numbers, auto-switches to follow-up template
- **Configurable delay** between messages (default 30-90 seconds)
- **Message history** with filters (status, search) - persists in browser

### Import Recipients
- From completed scrape jobs (dropdown)
- From CSV file (must have `phone` column)
- Manual phone list

---

## REST API

### Scraper API (port 8080)

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/api/v1/jobs` | POST | Create a new scraping job |
| `/api/v1/jobs` | GET | List all jobs |
| `/api/v1/jobs/{id}` | GET | Get job details |
| `/api/v1/jobs/{id}` | DELETE | Delete a job |
| `/api/v1/jobs/{id}/download` | GET | Download results as CSV |
| `/results-preview?id={id}` | GET | CSV data as JSON array |

### WhatsApp Bridge API (port 3001)

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/qr` | GET | QR code for authentication |
| `/status` | GET | Connection status |
| `/send` | POST | Send single message |
| `/bulk-send` | POST | Bulk send with configurable delay |
| `/bulk-status` | GET | Bulk send progress |
| `/bulk-stop` | POST | Abort bulk send |

Full OpenAPI 3.0.3 documentation available at http://localhost:8080/api/docs

---

## Developer Guide

See [DEV-GUIDE.md](DEV-GUIDE.md) for:
- Full project file structure with descriptions
- How to make UI changes (which file to edit for what)
- How to add new form fields
- All CLI flags
- Troubleshooting guide

---

## Installation

### Using Docker (Recommended)

Two Docker image variants are available:

| Image | Tag | Browser Engine | Best For |
|-------|-----|----------------|----------|
| Playwright (default) | `latest`, `vX.X.X` | Playwright | Most users, better stability |
| Rod | `latest-rod`, `vX.X.X-rod` | Rod/Chromium | Lightweight, faster startup |

```bash
# Playwright version (default)
docker pull gosom/google-maps-scraper

# Rod version (alternative)
docker pull gosom/google-maps-scraper:latest-rod
```

### Build from Source

Requirements: Go 1.25.6+

```bash
git clone https://github.com/gosom/google-maps-scraper.git
cd google-maps-scraper
go mod download

# Playwright version (default)
go build
./google-maps-scraper -input example-queries.txt -results results.csv -exit-on-inactivity 3m

# Rod version (alternative)
go build -tags rod
./google-maps-scraper -input example-queries.txt -results results.csv -exit-on-inactivity 3m
```

> First run downloads required browser libraries (Playwright or Chromium depending on version).

---

## Features

| Feature | Description |
|---------|-------------|
| **33+ Data Points** | Business name, address, phone, website, reviews, coordinates, and more |
| **Email Extraction** | Optional crawling of business websites for email addresses |
| **Multiple Output Formats** | CSV, JSON, PostgreSQL, S3, or custom plugins |
| **Proxy Support** | SOCKS5, HTTP, HTTPS with authentication |
| **Scalable Architecture** | Single machine to Kubernetes cluster |
| **REST API** | Programmatic control for automation |
| **Web UI** | User-friendly browser interface |
| **Fast Mode (Beta)** | Quick extraction of up to 21 results per query |
| **AWS Lambda** | Serverless execution support (experimental) |

---

## Extracted Data Points

<details>
<summary><strong>Click to expand all 33 data points</strong></summary>

| # | Field | Description |
|---|-------|-------------|
| 1 | `input_id` | Internal identifier for the input query |
| 2 | `link` | Direct URL to the listing |
| 3 | `title` | Business name |
| 4 | `category` | Business type (e.g., Restaurant, Hotel) |
| 5 | `address` | Street address |
| 6 | `open_hours` | Operating hours |
| 7 | `popular_times` | Visitor traffic patterns |
| 8 | `website` | Official business website |
| 9 | `phone` | Contact phone number |
| 10 | `plus_code` | Location shortcode |
| 11 | `review_count` | Total number of reviews |
| 12 | `review_rating` | Average star rating |
| 13 | `reviews_per_rating` | Breakdown by star rating |
| 14 | `latitude` | GPS latitude |
| 15 | `longitude` | GPS longitude |
| 16 | `cid` | Unique Customer ID |
| 17 | `status` | Business status (open/closed/temporary) |
| 18 | `descriptions` | Business description |
| 19 | `reviews_link` | Direct link to reviews |
| 20 | `thumbnail` | Thumbnail image URL |
| 21 | `timezone` | Business timezone |
| 22 | `price_range` | Price level ($, $$, $$$) |
| 23 | `data_id` | Internal identifier |
| 24 | `images` | Associated image URLs |
| 25 | `reservations` | Reservation booking link |
| 26 | `order_online` | Online ordering link |
| 27 | `menu` | Menu link |
| 28 | `owner` | Owner-claimed status |
| 29 | `complete_address` | Full formatted address |
| 30 | `about` | Additional business info |
| 31 | `user_reviews` | Customer reviews (text, rating, timestamp) |
| 32 | `emails` | Extracted email addresses (requires `-email` flag) |
| 33 | `user_reviews_extended` | Extended reviews up to ~300 (requires `-extra-reviews`) |
| 34 | `place_id` | Unique place id |

</details>

**Custom Input IDs:** Define your own IDs in the input file:
```
Matsuhisa Athens #!#MyCustomID
```

---

## Configuration

### Command Line Options

```
Usage: codesommet-scrapper [options]

Core Options:
  -input string       Path to input file with queries (one per line)
  -results string     Output file path (default: stdout)
  -json              Output JSON instead of CSV
  -depth int         Max scroll depth in results (default: 10)
  -c int             Concurrency level (default: half of CPU cores)

Email & Reviews:
  -email             Extract emails from business websites
  -extra-reviews     Collect extended reviews (up to ~300)

Location Settings:
  -lang string       Language code, e.g., 'de' for German (default: "en")
  -geo string        Coordinates for search, e.g., '37.7749,-122.4194'
  -zoom int          Zoom level 0-21 (default: 15)
  -radius float      Search radius in meters (default: 10000)
  -grid-bbox string  Bounding box for grid scraping, format: "minLat,minLon,maxLat,maxLon"
  -grid-cell float   Grid cell size in km (default: 1.0, used with -grid-bbox)

Web Server:
  -web               Run web server mode
  -addr string       Server address (default: ":8080")
  -data-folder       Data folder for web runner (default: "webdata")

Database:
  -dsn string        PostgreSQL connection string
  -produce           Produce seed jobs only (requires -dsn)

Proxy:
  -proxies string    Comma-separated proxy list
                     Format: protocol://user:pass@host:port

Advanced:
  -exit-on-inactivity duration    Exit after inactivity (e.g., '5m')
  -fast-mode                      Quick mode with reduced data
  -debug                          Show browser window
  -writer string                  Custom writer plugin (format: 'dir:pluginName')

Notes:
  -grid-bbox requires a valid zoom level (1-21)
  -fast-mode cannot be used together with -grid-bbox
```

Run `./codesommet-scrapper -h` for the complete list.

### Using Proxies

For larger scraping jobs, proxies help avoid rate limiting. Here's how to configure them:

```bash
./codesommet-scrapper \
  -input queries.txt \
  -results results.csv \
  -proxies 'socks5://user:pass@host:port,http://host2:port2' \
  -depth 1 -c 2
```

**Supported protocols:** `socks5`, `socks5h`, `http`, `https`

### Email Extraction

Email extraction is **disabled by default**. When enabled, the scrapper visits each business website to find email addresses.

```bash
./codesommet-scrapper -input queries.txt -results results.csv -email
```

> **Note:** Email extraction increases processing time significantly.

### Fast Mode

Fast mode returns up to 21 results per query, ordered by distance. Useful for quick data collection with basic fields.

```bash
./codesommet-scrapper \
  -input queries.txt \
  -results results.csv \
  -fast-mode \
  -zoom 15 \
  -radius 5000 \
  -geo '37.7749,-122.4194'
```

> **Warning:** Fast mode is in Beta. You may experience blocking.

### Grid Scraping (BBox)

Grid mode splits a bounding box into cells and runs one search per cell. This is useful when a single search does not return enough places.

`queries.txt` example:

```text
cafes in Peristeri, Greece
```

Command example:

```bash
./codesommet-scrapper \
  -input queries.txt \
  -results peristeri-cafes.csv \
  -grid-bbox "38.0077,23.6719,38.0257,23.6947" \
  -grid-cell 0.5 \
  -zoom 16 \
  -depth 1 \
  -c 4
```

Notes:
- `-grid-bbox` guides where searches are launched from, but results are not strictly clipped to the box.
- For strict distance filtering, use `-fast-mode` with `-geo` + `-radius` (or post-filter by latitude/longitude).

---

## Advanced Usage

### PostgreSQL Database Provider

For distributed scraping across multiple machines:

**1. Start PostgreSQL:**
```bash
docker-compose -f docker-compose.dev.yaml up -d
```

**2. Seed the jobs:**
```bash
./codesommet-scrapper \
  -dsn "postgres://postgres:postgres@localhost:5432/postgres" \
  -produce \
  -input example-queries.txt \
  -lang en
```

**3. Run scrapers (on multiple machines):**
```bash
./codesommet-scrapper \
  -c 2 \
  -depth 1 \
  -dsn "postgres://postgres:postgres@localhost:5432/postgres"
```

### Kubernetes Deployment

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: codesommet-scrapper
spec:
  replicas: 3  # Adjust based on needs
  selector:
    matchLabels:
      app: codesommet-scrapper
  template:
    metadata:
      labels:
        app: codesommet-scrapper
    spec:
      containers:
      - name: codesommet-scrapper
        image: gosom/google-maps-scraper:latest
        args: ["-c", "1", "-depth", "10", "-dsn", "postgres://user:pass@host:5432/db"]
        resources:
          requests:
            memory: "512Mi"
            cpu: "500m"
```

> **Note:** The headless browser requires significant CPU/memory resources.

### Custom Writer Plugins

Create custom output handlers using Go plugins:

**1. Write the plugin** (see `examples/plugins/example_writer.go`)

**2. Build:**
```bash
go build -buildmode=plugin -tags=plugin -o myplugin.so myplugin.go
```

**3. Run:**
```bash
./codesommet-scrapper -writer ~/plugins:MyWriter -input queries.txt
```

---

## Performance

**Expected throughput:** ~120 places/minute (with `-c 8 -depth 1`)

| Keywords | Results/Keyword | Total Jobs | Estimated Time |
|----------|-----------------|------------|----------------|
| 100 | 16 | 1,600 | ~13 minutes |
| 1,000 | 16 | 16,000 | ~2.5 hours |
| 10,000 | 16 | 160,000 | ~22 hours |

For large-scale scraping, use the PostgreSQL provider with Kubernetes.

### Telemetry

Anonymous usage statistics are collected for improvement purposes. Opt out:
```bash
export DISABLE_TELEMETRY=1
```

---

## Contributing

Contributions are welcome! Please:

1. Open an issue to discuss your idea
2. Fork the repository
3. Create a pull request

---

## License

This project is licensed under the [MIT License](LICENSE).

---

## Legal Notice

Please use CodeSommet Scrapper responsibly and in accordance with applicable laws and regulations. Unauthorized scraping may violate terms of service.
#   c o d e - s o m m e t - s c r a p p e r  
 