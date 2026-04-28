# Proxies

A proxy is a middleman server between your computer and Google Maps.
Instead of Google seeing your real IP, it sees the proxy's IP.

---

## Why Proxies Matter for Scraping

When you scrape too much from the same IP address:
- Google starts showing CAPTCHAs
- Requests get blocked or return empty results
- Your IP may be temporarily banned

Proxies solve this by rotating your apparent IP address.

---

## Types of Proxies

| Type | Speed | Cost | Best For |
|------|-------|------|---------|
| **Datacenter** | Fast | Cheap | Testing, low-risk scrapes |
| **Residential** | Medium | Expensive | Large-scale, avoids detection |
| **Mobile** | Slow | Most expensive | Hardest to block |

For Google Maps scraping, **residential proxies** work best.

---

## Proxy Format

Proxies usually look like this:
```
http://username:password@host:port
# Example:
http://user123:pass456@proxy.provider.com:8080
```

---

## Using Proxies with This Scraper

The scraper supports the `--proxy` flag or the Webshare integration.

### Single Proxy
```powershell
docker run `
  -v "C:/Users/ASUS/.../queries.txt:/queries.txt" `
  -v "C:/Users/ASUS/.../results.csv:/results.csv" `
  gosom/google-maps-scraper `
  -depth 1 `
  -input /queries.txt `
  -results /results.csv `
  -exit-on-inactivity 3m `
  --proxy "http://user:pass@proxyhost:8080"
```

### Webshare Integration (Recommended)
The scraper has built-in Webshare support (a proxy provider):

```powershell
docker run `
  -v "C:/Users/ASUS/.../queries.txt:/queries.txt" `
  -v "C:/Users/ASUS/.../results.csv:/results.csv" `
  gosom/google-maps-scraper `
  -depth 1 `
  -input /queries.txt `
  -results /results.csv `
  -exit-on-inactivity 3m `
  --webshare-key "YOUR_WEBSHARE_API_KEY"
```

See: [webshare.md](../../webshare.md) in the project root.

---

## When Do You Actually Need Proxies?

| Scenario | Need Proxy? |
|---------|------------|
| Testing 1–2 queries | No |
| Scraping 1 niche, 1 city | No |
| Scraping 10+ queries per day | Maybe |
| Scraping hundreds of queries | Yes |
| Running scraper 24/7 | Yes |

Start without proxies. Add them only when you start seeing:
- Empty results
- CAPTCHA errors in logs
- Incomplete data

---

## Free Proxy Testing

Free proxies exist but are unreliable. Use them only for testing:
- They are slow
- They often go offline
- They may not work with Google

For real lead generation, pay for a proxy service.

---

## Recommended Proxy Providers

- **Webshare** — has direct integration with this scraper
- **Bright Data** — most powerful, most expensive
- **Oxylabs** — good residential proxies
- **Smartproxy** — affordable residential

---

## Practice Tasks

1. Run a scrape without a proxy — note how many results you get
2. Sign up for a free Webshare trial
3. Run the same scrape with the Webshare key
4. Compare result counts and quality
