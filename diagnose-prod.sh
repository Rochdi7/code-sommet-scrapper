#!/usr/bin/env bash
# Production scraper diagnostics — run on the VPS from /var/www/scrape
# Usage:  bash diagnose-prod.sh 2>&1 | tee /tmp/scrape-diag.txt
set +e
line(){ printf '\n========== %s ==========\n' "$1"; }

line "docker compose ps"
docker compose ps

line "scraper logs (last 200)"
docker compose logs --tail=200 scraper

line "wa-bridge logs (last 30)"
docker compose logs --tail=30 wa-bridge

line "container: uname / whoami"
docker exec codesommet-scraper sh -c 'uname -a; id' 2>&1

line "container: /dev/shm size (df -h /dev/shm)"
docker exec codesommet-scraper sh -c 'df -h /dev/shm' 2>&1

line "container: is Chromium present? (Playwright browsers path)"
docker exec codesommet-scraper sh -c 'echo PLAYWRIGHT_BROWSERS_PATH=$PLAYWRIGHT_BROWSERS_PATH; ls -la /opt/browsers 2>/dev/null; find / -name "chrome" -type f 2>/dev/null | head; find / -iname "headless_shell" 2>/dev/null | head' 2>&1

line "container: can Chromium even launch? (--version)"
docker exec codesommet-scraper sh -c 'CHR=$(find /opt/browsers -name chrome -type f 2>/dev/null | head -1); echo "binary=$CHR"; [ -n "$CHR" ] && "$CHR" --headless=new --no-sandbox --disable-gpu --disable-dev-shm-usage --version 2>&1 | head' 2>&1

line "container: outbound network + DNS to Google"
docker exec codesommet-scraper sh -c 'which curl wget 2>/dev/null; (curl -sS -I -m 15 https://www.google.com/maps 2>&1 || wget -S -q -O /dev/null --timeout=15 https://www.google.com/maps 2>&1) | head -20' 2>&1

line "container: gmapsdata perms"
docker exec codesommet-scraper sh -c 'ls -la /gmapsdata 2>/dev/null; ls -la /data 2>/dev/null' 2>&1

line "host: memory / disk"
free -m
df -h /

line "host: shm_size in compose"
grep -n "shm_size" docker-compose.yml

line "repo state: is scrapemate replace active?"
grep -n "replace github.com/gosom/scrapemate" go.mod
echo "--- git status ---"; git log --oneline -1 2>/dev/null; git status -s 2>/dev/null

line "DONE — paste everything above"
