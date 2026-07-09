# 🚀 Deploy CodeSommet Lead Scraper on a Hostinger VPS

This guide walks you through hosting the app on a **Hostinger VPS** (Ubuntu) and serving it on your **Hostinger temporary domain** (e.g. `srv123456.hstgr.cloud`) over HTTPS.

The stack has 3 Docker services:

| Service | Container | Internal Port | Purpose |
|---------|-----------|:-------------:|---------|
| `scraper` | `codesommet-scraper` | 8080 | Web UI + Maps scraper |
| `wa-bridge` | `codesommet-wa-bridge` | 3001 | WhatsApp bridge API |
| `email-bridge` | `codesommet-email-bridge` | 3002 | Email bridge API |

We'll run all three with `docker compose`, then put **Nginx** in front so the public temp domain (`https://...hstgr.cloud`) reverse-proxies to the app on port 8080.

---

## 0. What you need first

- A **Hostinger VPS** (KVM plan). During setup pick the **Ubuntu 22.04 / 24.04** template (plain OS, not a panel image — or the "Docker" template if offered).
- Your VPS **IP address** and **root password** (from hPanel → VPS → Overview).
- Your **temporary domain**. In hPanel → VPS → find the auto-assigned hostname like `srv123456.hstgr.cloud`. This already points to your VPS IP — no DNS setup needed.

> Throughout this guide, replace `YOUR_DOMAIN` with your real temp domain (e.g. `srv123456.hstgr.cloud`) and `YOUR_VPS_IP` with your server IP.

---

## 1. Connect to the VPS

From your Windows machine (PowerShell):

```powershell
ssh root@YOUR_VPS_IP
```

Type `yes` to accept the fingerprint, then enter the root password.

---

## 2. Install Docker + Docker Compose

If the VPS didn't come with Docker preinstalled, run:

```bash
# Update system
apt update && apt upgrade -y

# Install Docker (official convenience script)
curl -fsSL https://get.docker.com | sh

# Verify
docker --version
docker compose version
```

> `docker compose` (v2, space) is what we use below. If you only have the old `docker-compose` (hyphen), the commands still work — just swap them.

---

## 3. Get the project onto the VPS

> This deploy uses a **dedicated folder `/var/www/scrape`** so it never touches any other site on the VPS (e.g. an existing `gls` site).

```bash
apt install -y git
mkdir -p /var/www/scrape
cd /var/www/scrape
git clone https://github.com/Rochdi7/google-maps-scraper.git .
```

> Replace the URL with your actual repo. If the repo is private, use a Personal Access Token or SSH deploy key.

> ⚠️ Don't upload the `gmapsdata/` database, `*.exe`, or WhatsApp auth files from your dev machine if you want a clean start — the containers create their own data.

---

## 4. Build and start the stack

From the project folder on the VPS (`/var/www/scrape`):

```bash
cd /var/www/scrape
docker compose up -d --build
```

First build takes several minutes (it downloads Go, Node, and a Chromium/Playwright browser). Check status:

```bash
docker compose ps
docker compose logs -f scraper     # Ctrl+C to stop following
```

You should see `queue: worker started` and `visit http://localhost:8080`.

Quick local test on the VPS:

```bash
curl -I http://localhost:8080
```

Expect `HTTP/1.1 200 OK`.

---

## 5. Put Nginx in front (reverse proxy)

Right now the app listens on port 8080 only inside the VPS. We use Nginx to serve it on the standard web ports (80/443) via your domain.

```bash
apt install -y nginx
```

Create the site config:

```bash
nano /etc/nginx/sites-available/codesommet
```

Paste this (replace `YOUR_DOMAIN`):

```nginx
server {
    listen 80;
    server_name YOUR_DOMAIN;

    # Increase timeouts + body size (scraping/exports can be slow/large)
    client_max_body_size 50m;
    proxy_read_timeout 300s;
    proxy_send_timeout 300s;

    # Main app (Web UI + scraper API)
    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
    }
}
```

> The WhatsApp bridge (3001) and email bridge (3002) are called by the browser from the same page. If your frontend calls them directly on those ports, they'll be reachable at `http://YOUR_DOMAIN:3001` / `:3002` (open those ports in the firewall — step 7). If your frontend calls them through the scraper backend, you don't need to expose them publicly. Keep them **closed** unless you know the UI needs them.

Enable the site and reload:

```bash
ln -s /etc/nginx/sites-available/codesommet /etc/nginx/sites-enabled/
rm -f /etc/nginx/sites-enabled/default
nginx -t          # test config
systemctl reload nginx
```

Now visit `http://YOUR_DOMAIN` in your browser — the app should load.

---

## 6. Add HTTPS (free, automatic)

Hostinger temp domains (`*.hstgr.cloud`) are real public hostnames, so Let's Encrypt works:

```bash
apt install -y certbot python3-certbot-nginx
certbot --nginx -d YOUR_DOMAIN
```

Answer the prompts (enter your email, agree to terms, choose **redirect HTTP → HTTPS**). Certbot edits your Nginx config and installs the certificate.

Auto-renewal is installed automatically. Test it:

```bash
certbot renew --dry-run
```

Now open **https://YOUR_DOMAIN** 🎉

---

## 7. Firewall (recommended)

```bash
apt install -y ufw
ufw allow OpenSSH
ufw allow 'Nginx Full'      # opens 80 + 443
# Only if the UI calls the bridges directly on their ports:
# ufw allow 3001
# ufw allow 3002
ufw enable
```

> **Do NOT** open port 8080 publicly — Nginx already proxies it. Keeping 8080 internal-only means users can only reach the app through HTTPS.

---

## 8. Everyday operations

Run these from `/opt/codesommet`:

```bash
# View running services
docker compose ps

# Follow logs
docker compose logs -f scraper
docker compose logs -f wa-bridge

# Restart everything
docker compose restart

# Stop / start
docker compose down
docker compose up -d

# Update after a code change / git pull
git pull
docker compose up -d --build
```

Your scraped data + job DB live in the mounted `./gmapsdata` folder and in Docker volumes (`wa-auth`, `email-config`), so they survive restarts and rebuilds.

---

## 9. WhatsApp login (one-time)

The WhatsApp bridge needs you to scan a QR code once to link a WhatsApp account. Open the WhatsApp page in the UI (`https://YOUR_DOMAIN/whatsapp`) and follow the QR prompt, or hit the bridge directly:

```
GET http://YOUR_VPS_IP:3001/qr
```

The session is stored in the `wa-auth` volume, so you won't need to re-scan after restarts (unless WhatsApp logs the device out).

---

## 🧯 Troubleshooting

| Symptom | Fix |
|---|---|
| `502 Bad Gateway` from Nginx | App not up. `docker compose ps` + `docker compose logs scraper`. Confirm `curl -I localhost:8080` returns 200. |
| Build fails on Chromium/Playwright | Ensure the VPS has ≥ 2 GB RAM and enough disk. Check `docker compose logs`. A swap file helps on 1 GB VPS: `fallocate -l 2G /swapfile && chmod 600 /swapfile && mkswap /swapfile && swapon /swapfile`. |
| Scrape jobs hang / browser crashes | Chromium needs shared memory — the compose file sets `shm_size: 2gb`. Make sure the VPS has enough RAM. |
| `certbot` fails | DNS must resolve to the VPS (temp domains already do). Make sure port 80 is open and Nginx is running. |
| Can't SSH | Check the IP and that OpenSSH is allowed in `ufw`. |
| Site works on IP but not domain | Confirm `server_name` matches your exact temp domain and you reloaded Nginx. |

---

## 📌 Quick reference

```bash
# One-time setup
curl -fsSL https://get.docker.com | sh
cd /opt && git clone <repo> codesommet && cd codesommet
docker compose up -d --build
apt install -y nginx certbot python3-certbot-nginx
# ...create nginx config (step 5)...
certbot --nginx -d YOUR_DOMAIN

# App URLs after deploy
https://YOUR_DOMAIN/            # Scraper
https://YOUR_DOMAIN/dashboard   # Analytics
https://YOUR_DOMAIN/leads       # Leads CRM
https://YOUR_DOMAIN/whatsapp    # WhatsApp Sender
https://YOUR_DOMAIN/email       # Email Sender
https://YOUR_DOMAIN/api/docs    # API docs
```

Built for the **CodeSommet Lead Scraper**. 💛
