# Docker

Docker lets you run software inside an isolated box called a **container**.
You do not need to install Go, Node, Python, or any dependencies.
The container has everything pre-installed. You just run it.

---

## Key Concepts

| Term | Meaning |
|------|---------|
| **Image** | A pre-built package of a program (like a ZIP file) |
| **Container** | A running instance of an image (like an opened ZIP) |
| **Volume** | A way to connect your local folder/file to the container |
| **Pull** | Download an image from the internet |
| **Run** | Start a container from an image |

---

## Core Commands

```bash
# Check Docker is running
docker info

# Download an image
docker pull gosom/google-maps-scraper

# See downloaded images
docker images

# See running containers
docker ps

# See all containers (including stopped)
docker ps -a

# Stop a running container
docker stop CONTAINER_ID

# Delete a stopped container
docker rm CONTAINER_ID

# Delete an image
docker rmi IMAGE_NAME
```

---

## Volume Mounting (Most Important for Scraping)

Volumes let the container read your input file and write results to your computer.
Without volumes, files are trapped inside the container and lost when it stops.

```powershell
# Syntax
-v "C:/local/path/file.txt:/container/path/file.txt"

# Real example
docker run `
  -v "C:/Users/ASUS/Desktop/Projects/google-maps-scraper/queries.txt:/queries.txt" `
  -v "C:/Users/ASUS/Desktop/Projects/google-maps-scraper/results.csv:/results.csv" `
  gosom/google-maps-scraper `
  -depth 1 `
  -input /queries.txt `
  -results /results.csv `
  -exit-on-inactivity 3m
```

> Always use full Windows paths (`C:/Users/...`) in PowerShell.
> The backtick `` ` `` is the line-continuation character in PowerShell.

---

## The gosom/google-maps-scraper Image

| Flag | What it does |
|------|-------------|
| `-depth 1` | How deep to scrape (1 = first page of results) |
| `-input /queries.txt` | File with your search queries |
| `-results /results.csv` | Where to save the output |
| `-exit-on-inactivity 3m` | Stop automatically after 3 minutes of no new results |
| `-email` | Also try to extract emails |
| `--extra-reviews` | Collect up to ~300 reviews per place |

---

## Common Mistakes

- Running Docker commands in Git Bash — paths break, use PowerShell
- Forgetting to create `results.csv` before running — Docker can't mount a file that doesn't exist
- Docker Desktop not started — run it first, wait ~60 seconds before using docker commands

---

## Practice Tasks

1. Run `docker images` — confirm the scraper image is downloaded
2. Edit `queries.txt` to say `dentists in Casablanca`
3. Delete `results.csv`, recreate it empty, run the scraper
4. Open `results.csv` and check the output
