# Lead Generation

Lead generation means finding potential customers or clients for a business.
This scraper is one of the most powerful tools for local lead generation.
You search Google Maps for businesses in a niche and location — and get
their name, phone, website, email, rating, and address in minutes.

---

## What is a Lead?

A lead is a business or person who might want what you are selling.

**Example:**
- You offer website design services
- You scrape `restaurants in Marrakech` with no website
- Every restaurant without a website = a lead you can contact

---

## Best Niches to Scrape

High-value businesses that often need services:

| Niche | Why They Are Good Leads |
|-------|------------------------|
| Dentists | High revenue, often need more patients |
| Lawyers | Need visibility, pay well for services |
| Real estate agents | Always need leads and marketing |
| Restaurants | Need online presence, delivery, social media |
| Hotels & riads | Need booking optimization, SEO |
| Gyms & fitness | Need members, social media, websites |
| Beauty salons | Need online booking, Instagram presence |
| Doctors & clinics | Need local SEO, appointment systems |
| Plumbers & electricians | Need Google Business optimization |
| Car dealers | Need digital marketing |

---

## Step-by-Step Lead Generation Workflow

### Step 1 — Define your niche and location
```
dentists in Casablanca
dentists in Rabat
dentists in Marrakech
```

### Step 2 — Run the scraper
```powershell
docker run `
  -v "C:/Users/ASUS/Desktop/Projects/google-maps-scraper/queries.txt:/queries.txt" `
  -v "C:/Users/ASUS/Desktop/Projects/google-maps-scraper/results.csv:/results.csv" `
  gosom/google-maps-scraper `
  -depth 3 `
  -input /queries.txt `
  -results /results.csv `
  -email `
  -exit-on-inactivity 5m
```

Use `-depth 3` and `-email` for lead gen.

### Step 3 — Filter the results

In Excel or Google Sheets, keep only rows where:
- `phone` is not empty
- `review_count` >= 5 (business is active)
- `review_rating` >= 3.5 (not a terrible business)
- `website` is empty (they need your web design service) — **OR**
- `website` is not empty (they have a site to audit and improve)

### Step 4 — Qualify leads

Sort by `review_count` descending.
High review count = busy business = more likely to have budget.

Add a column: **Priority**
- 5 stars + 100+ reviews + no website = HOT lead
- 4 stars + 20-100 reviews + has website = WARM lead
- Under 3.5 stars = skip

### Step 5 — Outreach

Use phone or email to contact them:

**Phone script example:**
> "Hello, I found your business on Google Maps. I help [niche] businesses in [city] get more customers through their website and Google profile. Do you have 5 minutes?"

**Email template:**
> Subject: Your Google Maps listing — quick question
>
> Hi [Name],
>
> I came across [Business Name] on Google Maps and noticed [specific observation — e.g. "you have 120 great reviews but no website"].
>
> I help businesses like yours in [City] get more customers online.
>
> Would you be open to a quick call this week?
>
> [Your Name]

---

## Organizing Your Leads

Recommended folder structure:

```
leads/
├── dentists-casablanca-raw.csv       ← straight from scraper
├── dentists-casablanca-cleaned.csv   ← filtered in Excel
├── dentists-casablanca-contacted.csv ← who you reached out to
└── dentists-casablanca-clients.csv   ← who said yes
```

---

## How Many Leads Per Scrape?

| Depth | Approx Results Per Query |
|-------|--------------------------|
| 1 | ~16–20 |
| 2 | ~30–40 |
| 3 | ~60–80 |

With 10 queries at depth 3 = ~600–800 raw leads.
After filtering = ~100–200 qualified leads.
Typical conversion = 5–15 clients from 200 outreach attempts.

---

## Practice Tasks

1. Pick one niche (e.g. `gyms in Casablanca`)
2. Scrape with `-depth 2 -email`
3. Open results in Excel, filter: rating >= 4, phone not empty
4. Write a personalized message for the top 5 leads
5. Track who you contacted in a separate CSV column
