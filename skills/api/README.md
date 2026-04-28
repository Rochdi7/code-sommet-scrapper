# API

An API (Application Programming Interface) is a way for programs to talk to each other.
Instead of scraping results to a CSV file, you can send them directly to a database,
a CRM, a Google Sheet, or your own web app via an API.

---

## This Scraper's Built-in API (Web Server Mode)

The scraper can run as a web server with a full REST API:

```powershell
# Start the web server
docker run -p 8080:8080 `
  -v "C:/Users/ASUS/Desktop/Projects/google-maps-scraper/data:/gmapsdata" `
  gosom/google-maps-scraper `
  -data-folder /gmapsdata

# Then open in browser:
# http://localhost:8080
```

### API Endpoints

| Endpoint | Method | What it does |
|----------|--------|-------------|
| `/api/v1/jobs` | POST | Start a new scrape job |
| `/api/v1/jobs` | GET | List all jobs |
| `/api/v1/jobs/{id}` | GET | Get job status and details |
| `/api/v1/jobs/{id}/download` | GET | Download results as CSV |
| `/api/docs` | GET | Full API documentation |

### Start a Scrape via API

```bash
curl -X POST http://localhost:8080/api/v1/jobs \
  -H "Content-Type: application/json" \
  -d '{"queries": ["restaurants in Marrakech"], "depth": 1}'
```

---

## Sending Results to Google Sheets (via API)

Google Sheets has an API. After scraping, you can push rows automatically.

**Simple approach — use a tool like n8n or Zapier:**
1. Scraper saves `results.csv`
2. n8n watches the folder
3. When new CSV appears, it reads rows and appends to Google Sheet

**Manual approach using PowerShell + Google Sheets API:**
```powershell
# Read CSV
$data = Import-Csv "results.csv"

# For each row, send to Google Sheets via API
foreach ($row in $data) {
    $body = @{
        values = @(@($row.title, $row.phone, $row.address, $row.review_rating))
    } | ConvertTo-Json -Depth 5

    Invoke-RestMethod -Uri "YOUR_SHEETS_API_ENDPOINT" `
      -Method POST `
      -Body $body `
      -ContentType "application/json"
}
```

---

## Sending Results to a CRM

Most CRMs (HubSpot, Pipedrive, Airtable) have APIs.

**Example — Airtable:**
```powershell
$headers = @{
    Authorization = "Bearer YOUR_AIRTABLE_API_KEY"
    "Content-Type" = "application/json"
}

$data = Import-Csv "results.csv"

foreach ($row in $data) {
    $body = @{
        fields = @{
            Name    = $row.title
            Phone   = $row.phone
            Address = $row.address
            Rating  = $row.review_rating
            Website = $row.website
        }
    } | ConvertTo-Json

    Invoke-RestMethod `
      -Uri "https://api.airtable.com/v0/YOUR_BASE_ID/Leads" `
      -Method POST `
      -Headers $headers `
      -Body $body
}
```

---

## Practice Tasks

1. Start the scraper in web server mode and open `http://localhost:8080`
2. Use the web UI to submit a query and watch the job run
3. Download results via the `/api/v1/jobs/{id}/download` endpoint
4. Try creating a free Airtable base and push 5 rows via API
