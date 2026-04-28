# Automation

Automation means making the scraper run on its own — no manual clicking.
You set it up once, and it runs every day, every hour, or on demand.

---

## Option 1: PowerShell Script (Simplest)

Create a file called `run-scrape.ps1` in the project folder:

```powershell
# run-scrape.ps1

# Set paths
$projectPath = "C:\Users\ASUS\Desktop\Projects\google-maps-scraper"
$timestamp = Get-Date -Format "yyyy-MM-dd_HH-mm"
$outputFile = "$projectPath\results-$timestamp.csv"

# Create empty output file
New-Item $outputFile -ItemType File -Force

# Run the scraper
docker run `
  -v "$projectPath\queries.txt:/queries.txt" `
  -v "${outputFile}:/results.csv" `
  gosom/google-maps-scraper `
  -depth 1 `
  -input /queries.txt `
  -results /results.csv `
  -exit-on-inactivity 3m

Write-Host "Done! Results saved to: $outputFile"
```

Run it from PowerShell:
```powershell
.\run-scrape.ps1
```

Each run creates a new timestamped file like `results-2026-04-11_14-30.csv`.

---

## Option 2: Windows Task Scheduler (Run Daily)

1. Open **Task Scheduler** (search in Start menu)
2. Click **Create Basic Task**
3. Name it: `Google Maps Scraper`
4. Trigger: **Daily** at a time you choose (e.g. 08:00)
5. Action: **Start a program**
   - Program: `powershell.exe`
   - Arguments: `-File "C:\Users\ASUS\Desktop\Projects\google-maps-scraper\run-scrape.ps1"`
6. Click Finish

Now it runs every morning automatically.

---

## Option 3: Multiple Queries Automation

Create a `queries.txt` with all your targets:

```
restaurants in Marrakech
dentists in Casablanca
gyms in Rabat
hotels in Agadir
lawyers in Fes
```

One run scrapes all of them. Results are combined in one CSV.

---

## Option 4: Rotate Queries Daily

Create separate query files per day or niche:

```
queries-monday.txt    → restaurants in Marrakech
queries-tuesday.txt   → dentists in Casablanca
queries-wednesday.txt → gyms in Rabat
```

In your script, pick the file based on the day:

```powershell
$day = (Get-Date).DayOfWeek
$queryFile = "$projectPath\queries-$day.txt"
```

---

## Folder Structure for Organized Results

```
google-maps-scraper/
├── queries.txt
├── results/
│   ├── 2026-04-11_restaurants-marrakech.csv
│   ├── 2026-04-12_dentists-casablanca.csv
│   └── ...
└── run-scrape.ps1
```

---

## Practice Tasks

1. Create `run-scrape.ps1` with the script above
2. Run it manually: `.\run-scrape.ps1`
3. Confirm a timestamped CSV was created
4. Set up Task Scheduler to run it at 9am daily
