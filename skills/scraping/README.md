# Web Scraping

Web scraping means automatically reading data from websites using code.
Instead of copying 500 restaurant names by hand, a scraper does it in minutes.

---

## How This Scraper Works

1. You write a query in `queries.txt` (e.g. `restaurants in Marrakech`)
2. The scraper opens Google Maps in a headless browser (invisible Chrome)
3. It searches for your query and collects all listed businesses
4. For each business, it visits the detail page and extracts all data
5. It writes each business as a row in `results.csv`

---

## Key Settings That Affect Results

### `-depth`
Controls how many pages of results to collect.

| Value | Meaning |
|-------|---------|
| `1` | First page only (~20 results) |
| `2` | Scroll and load more (~40 results) |
| `3+` | Even more results, takes longer |

Start with `-depth 1` to test. Use `-depth 3` for real lead generation.

### `-exit-on-inactivity`
Stops the scraper after X minutes with no new results.
- `3m` = stop after 3 minutes of inactivity
- Use longer values (like `5m`) for large scrapes

### `-email`
Tries to find contact emails from the business website.
Slower, but very useful for lead generation.

---

## What Can Be Scraped

Any search query that works on Google Maps works here:

```
restaurants in Marrakech
dentists in Casablanca
gyms in Rabat
hotels near Marrakech airport
plumbers in Dubai
real estate agents in Paris
```

---

## Rate Limits and Blocking

Google Maps limits how fast you can scrape.

| Risk Level | Behavior |
|-----------|---------|
| Low | Small scrape, 1 query, `-depth 1` |
| Medium | Multiple queries, `-depth 3` |
| High | Hundreds of queries without delays |

**How to reduce blocking risk:**
- Use `-exit-on-inactivity 3m` (built-in delay)
- Do not run multiple containers at the same time
- Use proxies for large-scale scraping (see proxies README)
- Scrape at different times of day

---

## Output Quality Tips

- Be specific in your query: `Italian restaurants in Gueliz Marrakech` gives better results than just `restaurants`
- Use the local language sometimes: `مطاعم في مراكش` can return different results
- One query per line in `queries.txt`

---

## Practice Tasks

1. Add 3 different queries to `queries.txt` (different cities or niches)
2. Run the scraper with `-depth 1` and count the results
3. Run again with `-depth 2` and compare
4. Try adding `-email` flag and see if emails appear in the CSV
