# Build stage for Playwright dependencies (driver + chromium baked into the image)
FROM ubuntu:20.04 AS playwright-deps
ENV PLAYWRIGHT_BROWSERS_PATH=/opt/browsers
# Optional override for the Playwright driver download host. The playwright-go
# client's default mirrors (playwright.azureedge.net) can be unreachable; pass
#   --build-arg PLAYWRIGHT_DOWNLOAD_HOST=<mirror>
# to point the build at a working mirror without editing this file. Empty by
# default so the pinned client uses its own resolution.
ARG PLAYWRIGHT_DOWNLOAD_HOST=""
ENV PLAYWRIGHT_DOWNLOAD_HOST=${PLAYWRIGHT_DOWNLOAD_HOST}
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
    # Pin the CLI to the SAME version the Go client requires (go.mod: v0.5700.1
    # => Playwright driver 1.57.0). Never use @latest: it drifts from go.mod and
    # produces a driver the compiled binary cannot use.
    && go install github.com/playwright-community/playwright-go/cmd/playwright@v0.5700.1 \
    && mkdir -p /opt/browsers \
    && playwright install chromium --with-deps

# Build stage
FROM golang:1.26.2-trixie AS builder
WORKDIR /app
COPY go.mod go.sum ./
COPY scrapemate-local/ ./scrapemate-local/
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-w -s" -o /usr/bin/google-maps-scraper

# Final stage
FROM debian:trixie-slim
ENV PLAYWRIGHT_BROWSERS_PATH=/opt/browsers
# Must point at the folder that directly contains `node` + `package/cli.js`.
# The pinned CLI (v0.5700.1 => 1.57.0) writes the driver to
# ~/.cache/ms-playwright-go/1.57.0, which is copied to /opt/ms-playwright-go/1.57.0 below.
ENV PLAYWRIGHT_DRIVER_PATH=/opt/ms-playwright-go/1.57.0

# Install runtime dependencies for Chromium
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    libnss3 \
    libnspr4 \
    libatk1.0-0 \
    libatk-bridge2.0-0 \
    libcups2 \
    libdrm2 \
    libdbus-1-3 \
    libxkbcommon0 \
    libatspi2.0-0 \
    libx11-6 \
    libxcomposite1 \
    libxdamage1 \
    libxext6 \
    libxfixes3 \
    libxrandr2 \
    libgbm1 \
    libpango-1.0-0 \
    libcairo2 \
    libasound2 \
    libx11-xcb1 \
    libxcb1 \
    libxcursor1 \
    libgtk-3-0 \
    libgdk-pixbuf-2.0-0 \
    libegl1 \
    libvulkan1 \
    fonts-liberation \
    xdg-utils \
    && apt-get clean \
    && rm -rf /var/lib/apt/lists/*

COPY --from=playwright-deps /opt/browsers /opt/browsers
COPY --from=playwright-deps /root/.cache/ms-playwright-go /opt/ms-playwright-go

RUN chmod -R 755 /opt/browsers \
    && chmod -R 755 /opt/ms-playwright-go

COPY --from=builder /usr/bin/google-maps-scraper /usr/bin/

ENTRYPOINT ["google-maps-scraper"]
