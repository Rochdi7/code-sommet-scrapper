# Data Handling

After scraping, `results.csv` contains raw data.
Raw data is messy — duplicates, missing values, irrelevant businesses.
This skill is about cleaning and filtering it so only useful leads remain.

---

## Understanding results.csv

Each row = one business. Key columns you care about most:

| Column | Example | Use |
|--------|---------|-----|
| `title` | EAT ME MARRAKECH | Business name |
| `category` | Thai restaurant | Filter by type |
| `address` | 10 Rue Sourya, Marrakech | Contact/location |
| `phone` | 06 63 89 69 59 | Call or WhatsApp |
| `website` | eatme.com | Outreach |
| `emails` | info@eatme.com | Email outreach |
| `review_rating` | 4.7 | Quality filter |
| `review_count` | 586 | Popularity filter |
| `price_range` | MAD 100–150 | Segment by budget |
| `open_hours` | JSON with days/times | Verify open |

---

## Opening in Excel or Google Sheets

**Excel:**
1. Open Excel → Data → From Text/CSV
2. Select `results.csv`
3. Delimiter: Comma
4. Click Load

**Google Sheets:**
1. File → Import → Upload `results.csv`
2. Separator: Comma
3. Click Import

---

## Filtering Useful Leads (in Excel/Sheets)

### Only high-rated businesses
- Filter `review_rating` >= 4.0

### Only businesses with a phone number
- Filter `phone` is not empty

### Only businesses with a website
- Filter `website` is not empty

### Only businesses with emails
- Filter `emails` is not empty
- This column only fills if you ran with `-email` flag

### Remove duplicates
- Excel: Data → Remove Duplicates → by `title` + `address`
- Sheets: Data → Data Cleanup → Remove Duplicates

---

## Useful Excel Formulas for Leads

```excel
# Count how many have a phone
=COUNTIF(E:E,"<>")

# Count how many have rating above 4
=COUNTIF(N:N,">4")

# Extract city from address (if format is consistent)
=MID(D2, FIND(",",D2)+2, 50)
```

---

## Cleaning Checklist

- [ ] Remove rows with no phone AND no website AND no email
- [ ] Remove businesses with fewer than 10 reviews (low reliability)
- [ ] Remove duplicates by business name
- [ ] Check category column — remove unrelated businesses
- [ ] Sort by `review_rating` descending to see best first

---

## Exporting a Clean List

After filtering in Excel/Sheets, export as a new CSV:
- `leads-marrakech-restaurants.csv`
- `leads-casablanca-dentists.csv`

Keep originals untouched. Always work on a copy.

---

## Practice Tasks

1. Open `results.csv` in Excel or Google Sheets
2. Filter to only show businesses rated 4.5 or above
3. Filter to only rows that have a phone number
4. Count how many leads remain
5. Save the filtered list as `leads-clean.csv`
