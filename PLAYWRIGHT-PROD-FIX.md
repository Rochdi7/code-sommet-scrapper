# Production Fix — Playwright driver 404 → scraper returns 0 results

## Root cause (verified from source, not guessed)

The compiled binary uses the Playwright Go client pinned in `go.mod`:

- **`go.mod:27`** → `github.com/playwright-community/playwright-go v0.5700.1`
- That version hard-codes, in its own source (`run.go:19`):
  `const playwrightCliVersion = "1.57.0"`
- And its driver download mirrors (`run.go:24-26`) are the **retired** Azure CDN:
  `https://playwright.azureedge.net`, `...-akamai...`, `...-verizon...`

At runtime the app calls `playwright.Install()`, which fetches
`https://playwright.azureedge.net/builds/driver/playwright-1.57.0-linux.zip`.
That host is dead → **404** → driver never installs → `playwright.Run()` fails →
the browser pool can't be built → every `Fetch()` errors → the job worker retries
silently → **"Found 0 results" forever.**

Secondary problems found:
- **Two different pinned versions**: main module `v0.5700.1` (driver 1.57.0) vs
  `scrapemate-local/go.mod:14` `v0.5200.0` (driver 1.52.0). Whichever is active,
  the *runtime* download is the failing step.
- **Dockerfile:14** installs the CLI at `@latest` (not the pinned version) → version drift.
- **Dockerfile:16** `playwright install chromium` also hits the dead Azure CDN at build time.
- **Dockerfile:30** sets `PLAYWRIGHT_DRIVER_PATH=/opt` but the driver is copied to
  `/opt/ms-playwright-go` (line 67) → path mismatch (runtime looks for `package/cli.js`+`node` directly under the driver dir).

## Fix strategy (matches your Tasks 5–7)

1. **Unify on ONE version** — `v0.5700.1` / driver **1.57.0** — in both `go.mod` files, and re-enable the `replace` so the local adapter (which has `--no-sandbox`, `--disable-dev-shm-usage`) is used.
2. **Remove runtime `playwright.Install()`** (2 call sites) so production never phones a CDN.
3. **Bundle driver 1.57.0 + Chromium at Docker build time**, downloading from the **live Microsoft mirror** via `PLAYWRIGHT_DOWNLOAD_HOST` (the Go client honors this env — `run.go:365`).
4. **Fix the driver path** so build-time layout == runtime expectation.

The version `1.57.0` is **derived from the code** (`playwrightCliVersion` in `playwright-go@v0.5700.1`), not chosen.

---

## Files changed

### 1. `go.mod` — re-enable the replace (unify on local adapter)

```diff
-// replace github.com/gosom/scrapemate v1.0.0 => ./scrapemate-local
+replace github.com/gosom/scrapemate v1.0.0 => ./scrapemate-local
```

### 2. `scrapemate-local/go.mod` — pin the SAME playwright-go version

```diff
-	github.com/playwright-community/playwright-go v0.5200.0
+	github.com/playwright-community/playwright-go v0.5700.1
```

> After this, run `go mod tidy` locally (or in the builder) so `go.sum` updates for `v0.5700.1`.

### 3. `scrapemate-local/adapters/fetchers/jshttp/jshttp.go` — drop runtime Install

```diff
 func New(params JSFetcherOptions) (scrapemate.HTTPFetcher, error) {
-	opts := []*playwright.RunOptions{
-		{
-			Browsers: []string{"chromium"},
-			Verbose:  true,
-		},
-	}
-
-	if err := playwright.Install(opts...); err != nil {
-		return nil, err
-	}
-
-	pw, err := playwright.Run()
+	// Driver + browsers are bundled at Docker build time (see Dockerfile).
+	// Never install at runtime — the pinned client's CDN is retired.
+	pw, err := playwright.Run()
 	if err != nil {
 		return nil, err
 	}
```

### 4. `runner/installplaywright/installplaywright.go` — no-op the runtime installer

This runner only executes in `RunModeInstallPlaywright` (an explicit CLI subcommand, not the web path), but its `Install()` also hits the dead CDN. Make it a safe no-op so nothing downloads at runtime:

```diff
 func (i *installer) Run(context.Context) error {
-	opts := []*playwright.RunOptions{
-		{
-			Browsers: []string{"chromium"},
-		},
-	}
-
-	return playwright.Install(opts...)
+	// Driver + browsers are bundled at Docker build time; nothing to install at runtime.
+	return nil
 }
```

> Also remove the now-unused `playwright` import from that file (leave `context`, `fmt`, `runner`).

### 5. `Dockerfile` — install the PINNED driver from the LIVE mirror at build time

Replace the `playwright-deps` stage and the driver env/copy lines:

```dockerfile
# Build stage for Playwright dependencies (driver + chromium bundled at BUILD time)
FROM ubuntu:20.04 AS playwright-deps
ENV PLAYWRIGHT_BROWSERS_PATH=/opt/browsers
# Live Microsoft mirror — the old azureedge.net CDN is retired (was the 404 cause)
ENV PLAYWRIGHT_DOWNLOAD_HOST=https://playwright.download.prss.microsoft.com/dbazure/download/playwright
RUN export PATH=$PATH:/usr/local/go/bin:/root/go/bin \
    && apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates curl wget \
    && wget -q https://go.dev/dl/go1.26.2.linux-amd64.tar.gz \
    && tar -C /usr/local -xzf go1.26.2.linux-amd64.tar.gz \
    && rm go1.26.2.linux-amd64.tar.gz \
    && curl -fsSL https://deb.nodesource.com/setup_20.x | bash - \
    && apt-get install -y --no-install-recommends nodejs \
    && apt-get clean \
    && rm -rf /var/lib/apt/lists/* \
    # Pin the CLI to the SAME version the Go client expects (v0.5700.1 => driver 1.57.0)
    && go install github.com/playwright-community/playwright-go/cmd/playwright@v0.5700.1 \
    && mkdir -p /opt/browsers \
    && playwright install chromium --with-deps
```

Then fix the driver env + copy in the final stage:

```diff
 ENV PLAYWRIGHT_BROWSERS_PATH=/opt/browsers
-ENV PLAYWRIGHT_DRIVER_PATH=/opt
+# Driver dir must contain package/cli.js + node directly (run.go getDriverCliJs)
+ENV PLAYWRIGHT_DRIVER_PATH=/opt/ms-playwright-go/1.57.0
```

```diff
 COPY --from=playwright-deps /opt/browsers /opt/browsers
-COPY --from=playwright-deps /root/.cache/ms-playwright-go /opt/ms-playwright-go
+COPY --from=playwright-deps /root/.cache/ms-playwright-go /opt/ms-playwright-go
```

```diff
 RUN chmod -R 755 /opt/browsers \
-    && chmod -R 755 /opt/ms-playwright-go
+    && chmod -R 755 /opt/ms-playwright-go
```

> `PLAYWRIGHT_DRIVER_PATH` now points at the exact versioned folder (`/opt/ms-playwright-go/1.57.0`) that `go install .../cmd/playwright@v0.5700.1` produces under `~/.cache/ms-playwright-go/1.57.0`. That folder holds `node` + `package/cli.js`, which is what `playwright.Run()` needs (`run.go:308,336,338`).

---

## Revert the earlier workarounds

These were compile-forcing hacks and must be undone:

- **`mxschmitt/playwright-go`** import change → revert to `playwright-community/playwright-go` (the pinned module). If your prod `go.mod`/imports still reference `mxschmitt`, switch them back.
- The `go.mod.bak` / `go.mod.bak2` files on the VPS → delete after confirming the fix.

The 3 commented WhatsApp routes in `web/web.go` are **unrelated to scraping** — leave them commented for now if you only need scraping working. (Uncommenting requires implementing `whatsappAutoResumeCampaign`, `whatsappAutoContactedPhones`, `whatsappAutoDeleteCampaign` — a separate task.)

---

## Apply on the VPS

```bash
cd /var/www/scrape

# 1. Apply the edits above (go.mod, scrapemate-local/go.mod, jshttp.go,
#    installplaywright.go, Dockerfile). Then:

# 2. Rebuild with no cache so the new driver stage runs
docker compose build --no-cache scraper
docker compose up -d scraper

# 3. Verify the driver is bundled (should print 1.57.0 dir + cli.js)
docker exec codesommet-scraper sh -c 'ls -la /opt/ms-playwright-go/1.57.0 && ls /opt/ms-playwright-go/1.57.0/package/cli.js'
docker exec codesommet-scraper sh -c 'ls -la /opt/browsers'

# 4. Confirm no runtime download happens: watch logs while starting a scrape
docker compose logs -f scraper
```

Then create a scrape job (e.g. "travel agency marrakech"). You should now see page
fetches in the logs and results appear — instead of "Found 0 results" forever.

### If build-time `playwright install chromium` still 404s

That means the mirror path needs adjusting. Confirm the exact live path by testing in the builder:
```bash
curl -I "https://playwright.download.prss.microsoft.com/dbazure/download/playwright/builds/driver/playwright-1.57.0-linux.zip"
```
If that returns 200, the `PLAYWRIGHT_DOWNLOAD_HOST` above is correct. If it 404s, share the output and we adjust the host prefix (Microsoft occasionally changes the `dbazure/download/playwright` segment) — **without** changing the version, which stays `1.57.0` per the pinned client.
```
