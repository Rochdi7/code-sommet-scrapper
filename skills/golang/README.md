# Golang (Basic Reading)

Go is the programming language this scraper is written in.
You do not need to write Go code — but reading it helps you understand
what the scraper is doing, what data it collects, and how to modify small things.

---

## What Go Looks Like

```go
// A function that takes a URL and returns business data
func scrapePlace(url string) (Place, error) {
    // ... logic here
    return place, nil
}

// A struct = a data shape (like a table row)
type Place struct {
    Title       string
    Address     string
    Phone       string
    Rating      float64
    ReviewCount int
    Website     string
    Latitude    float64
    Longitude   float64
}
```

---

## Key Files in This Scraper

| File/Folder | What it does |
|-------------|-------------|
| `main.go` | Entry point — starts the program, reads flags |
| `gmaps/` | Core logic — opens Google Maps, extracts data |
| `scraper/` | Orchestrates jobs, workers, and output |
| `cli/` | Defines the command-line flags (`-depth`, `-input`, etc.) |
| `go.mod` | Lists all dependencies (like package.json in Node) |

---

## Reading the Scraper Output Fields

These come from the `Place` struct in the Go code and map directly to CSV columns:

| CSV Column | What it means |
|-----------|--------------|
| `title` | Business name |
| `category` | Type (Restaurant, Dentist, etc.) |
| `address` | Full street address |
| `phone` | Phone number |
| `website` | Website URL |
| `review_rating` | Average star rating (e.g. 4.7) |
| `review_count` | Total number of reviews |
| `latitude` | GPS coordinate |
| `longitude` | GPS coordinate |
| `open_hours` | Opening hours per day (JSON) |
| `price_range` | e.g. MAD 100–150 |
| `emails` | Email if `-email` flag is used |

---

## How to Read Go Without Knowing Go

- `func` = a function (a named block of code)
- `struct` = a data shape (like a row definition)
- `string` = text
- `float64` = decimal number
- `int` = whole number
- `[]string` = a list of text items
- `//` = a comment (ignored by the program, explains the code)
- `fmt.Println(...)` = print something to the screen

---

## You Do NOT Need To

- Install Go (Docker handles everything)
- Compile or build anything
- Understand advanced Go concepts

---

## Practice Tasks

1. Open `main.go` — find where `-depth` flag is defined
2. Open `gmaps/` folder — find a file that mentions "phone" or "rating"
3. Look at `go.mod` — what external libraries does this project use?
