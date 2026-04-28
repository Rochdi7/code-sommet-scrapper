---
name: google-maps-scraper
description: >
  Free and open-source Google Maps scraper using Docker. Use when the user wants to find businesses,
  extract leads, emails, reviews, or ratings from Google Maps. Triggers on requests like
  "find all <business type> in <city>", "scrape Google Maps for <keyword>",
  "get leads from Google Maps". Keywords: google maps, scrape, business, leads, restaurants,
  shops, places, reviews, ratings, emails, contacts.
license: MIT
compatibility: "Requires Docker installed and running."
metadata:
  author: gosom
  email: hi@gosom.dev
  version: "1.11.0"
  repository: "https://github.com/gosom/google-maps-scraper"
allowed-tools: Bash(docker:*) Bash(touch:*) Bash(wc:*) Bash(mkdir:*) Read Write
---

# Google Maps Scraper — SEO & Web Dev Lead Generation

This scraper is used to find local businesses in Morocco and beyond
that need **SEO services** or **web development**.

The goal is simple:
- Find businesses that exist on Google Maps but have weak or no online presence
- Collect their phone, website, email, rating, and address
- Prioritize the ones most likely to pay for SEO or a new website

---

## What We Are Selling

**Service 1 — Web Development**
Build professional websites for businesses that have no website or a broken/outdated one.

Target signals:
- `website` column is empty → they have no website at all
- Website URL looks like a Facebook page → they have no real site
- Low review count despite being open a long time → poor online visibility

**Service 2 — SEO (Search Engine Optimization)**
Help businesses rank higher on Google Maps and Google Search.

Target signals:
- `review_count` < 20 → low visibility, needs SEO
- `review_rating` < 4.0 → needs reputation management
- Business exists but doesn't show up easily in searches → needs local SEO
- Has a website but no Google ranking → needs on-page SEO

---

## Best Niches to Scrape (High Budget + High Need)

| Niche | Why They Pay Well |
|-------|------------------|
| Dentists & clinics | High revenue per patient, need local SEO to get more |
| Lawyers & notaries | Pay premium for visibility, very competitive keyword space |
| Real estate agents | Need lead generation, landing pages, SEO constantly |
| Hotels & riads | Compete heavily for Google rankings, need web presence |
| Beauty salons & spas | Need booking systems, Instagram integration, local SEO |
| Gyms & fitness centers | Need membership pages, local SEO, Google Maps optimization |
| Restaurants | Need websites with menus, Google Business optimization |
| Car dealerships | High ticket, need SEO + landing pages |
| Accountants & consultants | Need professional websites and local SEO |
| Private schools & tutors | Need enrollment pages, ranking in local search |

---

## Interaction Flow

When asked to find leads, follow this exact flow:

### Phase 1: Gather Requirements

Do NOT ask for permission or confirmation before proceeding.
Use sensible defaults and start immediately.
Only ask if the location or niche is missing entirely.

Show a brief summary of defaults:

1. **What to search?** — (provided by user, e.g. "dentists in Casablanca")
2. **Language** — `en` (use `fr` for Morocco if searching in French)
3. **Extract emails?** — yes (for outreach)
4. **Extract phones?** — yes, always (primary channel — WhatsApp outreach)
5. **Depth** — `medium` (~40 results per query)
6. **Output format** — CSV
7. **Proxy?** — no (add only if scraping slows or blocks)

Then proceed directly to Phase 2.

### Phase 2: Prepare and Run

**Step 1 — Build queries file**

Split the target city into neighborhoods for better coverage.
Write one query per line to `/tmp/gmaps_queries.txt`.

Example — user says "find dentists in Casablanca":
```
dentists in Casablanca Centre
dentists in Casablanca Maarif
dentists in Casablanca Ain Diab
dentists in Casablanca Hay Hassani
dentists in Casablanca Sidi Moumen
dentists in Casablanca Anfa
```

Example — user says "web design leads in Marrakech":
```
restaurants in Marrakech Gueliz
hotels in Marrakech Medina
riads in Marrakech
beauty salons in Marrakech
gyms in Marrakech
lawyers in Marrakech
dentists in Marrakech
```

**Step 2 — Map choices to flags**

| Choice | Flag |
|--------|------|
| Language `XX` | `-lang XX` |
| Extract emails | `-email` |
| Depth: shallow (~20 results) | `-depth 1` |
| Depth: medium (~40 results) | `-depth 5` |
| Depth: deep (~80 results) | `-depth 10` |
| CSV output | `-results /results.csv` |
| Proxy URL | `-proxies "URL"` |

Always use `-email` for lead generation.
Phone is the #1 priority — `phone` column is always scraped automatically (no extra flag needed).
Never go above `-depth 10` unless explicitly asked.

**Step 3 — Run the scraper in the background**

Always use `-exit-on-inactivity 3m` so the container stops when done.
Use a descriptive filename like `/tmp/gmaps_dentists_casablanca.csv`.
Mount a named Docker volume (`gmaps-playwright-cache`) to cache browsers.

```bash
touch /tmp/gmaps_<niche>_<city>.csv

docker pull gosom/google-maps-scraper   # only on first run of the session

docker rm gmaps-scraper 2>/dev/null

docker run \
  --name gmaps-scraper \
  -v gmaps-playwright-cache:/opt \
  -v /tmp/gmaps_queries.txt:/queries.txt \
  -v /tmp/gmaps_<niche>_<city>.csv:/results.csv \
  gosom/google-maps-scraper \
  -input /queries.txt \
  -results /results.csv \
  -exit-on-inactivity 3m \
  -depth 5 \
  -email
```

On Windows always run via PowerShell with full paths:
```powershell
docker run `
  --name gmaps-scraper `
  -v gmaps-playwright-cache:/opt `
  -v "C:/Users/ASUS/Desktop/Projects/google-maps-scraper/queries.txt:/queries.txt" `
  -v "C:/Users/ASUS/Desktop/Projects/google-maps-scraper/results.csv:/results.csv" `
  gosom/google-maps-scraper `
  -input /queries.txt `
  -results /results.csv `
  -exit-on-inactivity 3m `
  -depth 5 `
  -email
```

Tell the user:
- Scrape has started
- First run is slower (downloads ~270MB browser), next runs are instant
- Roughly 1–2 minutes per query with email extraction
- You will notify when done

**Step 4 — Monitor and notify**

Once complete, move to Phase 3.

### Phase 3: Present Results

When done:

1. Count total rows in the CSV
2. Show a summary table — most useful columns for SEO/web dev leads:
   - `title`, `category`, `rating`, `review_count`, `phone`, `website`, `emails`, `address`
3. Limit preview to 20 rows, show total count
4. **Automatically flag the best leads** using these rules:

| Signal | Lead Quality | Label |
|--------|-------------|-------|
| Phone exists + no website | Best for web dev — call/WhatsApp now | HOT |
| Phone exists + has website + rating < 4.0 | Needs SEO — call/WhatsApp | HOT |
| Phone exists + has website + reviews < 30 | Needs SEO — call/WhatsApp | WARM |
| No phone + email exists | Email only outreach | WARM |
| No phone AND no email | Skip | COLD |

**Phone = WhatsApp in Morocco.** Any business with a Moroccan number (+212 / 06 / 07) can be contacted directly on WhatsApp. This is faster and gets more replies than email.

5. Announce options:

> Scrape complete! Found **N** businesses.
>
> **With phone (WhatsApp-ready):** X
> **HOT leads (phone + no website):** X
> **WARM leads (phone + needs SEO):** X
>
> Here's a preview: [table]
>
> What next?
> 1. **Export WhatsApp list** — phone numbers only, ready to paste into WhatsApp
> 2. **Filter HOT leads** — phone + no website (web dev pitch)
> 3. **Filter WARM leads** — phone + has site but needs SEO
> 4. **Export clean lead list** — all columns, ready for outreach
> 5. **Analyze** — ask anything about the data
> 6. **More results** — run deeper scrape

### Phase 4: Post-Processing

**Save**: Ask where to save, copy the file there.

**Export WhatsApp list**:
Extract only the `phone` column where phone is not empty.
Format each number for WhatsApp — remove spaces, add `+212` prefix if Moroccan local format (`06...` → `+21206...`).
Save as `whatsapp-numbers.txt`, one number per line.
User can paste directly into WhatsApp bulk messenger or use with a WhatsApp outreach tool.

**Filter HOT (web dev leads)**:
- `website` is empty AND `phone` is not empty
- Sort by `review_count` descending (busiest first = most likely to pay)
- These are businesses earning money with zero online presence

**Filter WARM (SEO leads)**:
- `website` is not empty AND (`review_count` < 50 OR `review_rating` < 4.2)
- These have a website but are invisible online — perfect SEO pitch

**Export clean lead list**:
Produce a simplified CSV with only outreach-relevant columns:
`title`, `category`, `phone`, `website`, `emails`, `address`, `review_rating`, `review_count`, `lead_type`

Add a `lead_type` column: HOT / WARM / COLD based on the rules above.

**Analyze**: Answer questions like:
- "Which category has the most leads without a website?"
- "Show me all with rating below 4"
- "Which ones have emails I can contact directly?"
- "How many riads have no website?"

---

## Outreach Templates (Ready to Use)

### WhatsApp Message — Web Development (no website)
> Bonjour [Nom] 👋
> J'ai vu votre établissement sur Google Maps.
> Vous n'avez pas encore de site web — je peux vous en créer un rapidement.
> Je vous envoie un exemple gratuit si vous voulez voir?

### WhatsApp Message — SEO (has website, low visibility)
> Bonjour [Nom] 👋
> J'ai regardé votre présence en ligne — votre site existe mais n'apparaît pas bien sur Google.
> Je peux vous faire un audit gratuit en 24h.
> Intéressé?

> **Tips for WhatsApp outreach:**
> - Send between 9am–12pm or 3pm–6pm
> - Keep first message under 3 lines
> - No links in first message (gets ignored)
> - Follow up once after 2 days if no reply

### Phone Script — Web Development
> "Bonjour, j'ai vu votre établissement sur Google Maps.
> Je crée des sites web professionnels pour les [restaurants/cliniques/...] à [Ville].
> Est-ce que vous avez actuellement un site web?
> [If no] — Je peux vous montrer un exemple gratuit pour votre activité.
> Vous avez 5 minutes cette semaine?"

### Email — Web Development
> **Subject:** Votre présence en ligne — [Business Name]
>
> Bonjour,
>
> J'ai trouvé [Business Name] sur Google Maps et j'ai remarqué que vous n'avez pas encore de site web.
>
> Je crée des sites professionnels pour les [niche] à [Ville] — rapides, modernes, et optimisés pour Google.
>
> Je peux vous envoyer un exemple gratuit adapté à votre activité.
>
> Cordialement,
> [Your Name]

### Email — SEO Audit
> **Subject:** Votre site web peut attirer plus de clients — [Business Name]
>
> Bonjour,
>
> J'ai analysé la présence en ligne de [Business Name] et j'ai identifié quelques points
> qui limitent votre visibilité sur Google.
>
> Je peux vous envoyer un audit gratuit avec des recommandations concrètes.
>
> Interested? Je réponds dans l'heure.
>
> [Your Name]

---

## Grid Search (Large City Coverage)

For full city coverage, divide the city into a grid:

```bash
echo "dentists" > /tmp/gmaps_queries.txt

docker rm gmaps-scraper 2>/dev/null

docker run \
  --name gmaps-scraper \
  -v gmaps-playwright-cache:/opt \
  -v /tmp/gmaps_queries.txt:/queries.txt \
  -v /tmp/gmaps_dentists_casablanca.csv:/results.csv \
  gosom/google-maps-scraper \
  -input /queries.txt \
  -results /results.csv \
  -exit-on-inactivity 3m \
  -depth 5 \
  -email \
  -grid-bbox "33.49,7.53,33.65,7.72" \
  -grid-cell 1.0
```

Grid flags:

| Flag | Description |
|------|-------------|
| `-grid-bbox "minLat,minLon,maxLat,maxLon"` | City bounding box |
| `-grid-cell N` | Cell size in km — smaller = more thorough |
| `-depth N` | Use 5–10 for grid searches |

Moroccan city bounding boxes (approximate):

| City | Bounding Box |
|------|-------------|
| Casablanca | `33.49,7.53,33.65,7.72` |
| Marrakech | `31.59,8.06,31.68,7.95` |
| Rabat | `33.95,6.80,34.05,6.90` |
| Fes | `33.97,4.93,34.08,5.05` |
| Agadir | `30.38,9.59,30.45,9.68` |
| Tangier | `35.73,5.82,35.80,5.92` |

Warn the user: grid search over a full city can take 30–60 minutes.

---

## CSV Columns Reference

Most important for SEO/web dev outreach:

| Column | Use for |
|--------|---------|
| `title` | Business name |
| `category` | Filter by niche |
| `phone` | **#1 priority** — WhatsApp + call outreach (always scraped, no flag needed) |
| `website` | Check if they have a site |
| `emails` | Email outreach (needs `-email` flag) |
| `review_rating` | SEO quality signal |
| `review_count` | Popularity and SEO visibility |
| `address` | Confirm location |
| `price_range` | Estimate budget |

Full column list:
`input_id`, `link`, `title`, `category`, `address`, `open_hours`, `popular_times`, `website`, `phone`, `plus_code`, `review_count`, `review_rating`, `reviews_per_rating`, `latitude`, `longitude`, `cid`, `status`, `description`, `reviews_link`, `thumbnail`, `timezone`, `price_range`, `data_id`, `images`, `reservations`, `order_online`, `menu`, `owner`, `complete_address`, `about`, `user_reviews`, `emails`

---

## Error Handling

- **Docker not running**: Start Docker Desktop, wait 60 seconds, retry
- **Empty results**: Broaden query, try French keywords (`dentistes à Casablanca`), or reduce depth
- **Path errors in Git Bash**: Always use PowerShell for Docker commands on Windows
- **Slow scraping**: Remove `-email` flag for faster results, add it back for final lead export
- **results.csv not created**: Create it first with `touch` or `New-Item` before running Docker
