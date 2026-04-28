// Google Maps Scraper - Content Script
// Extracts business data from Google Maps search results and place pages.
// Replicates the Go-based scraper logic (entry.go, job.go, place.go) in browser context.

(function () {
  "use strict";

  // ---------------------------------------------------------------------------
  // State
  // ---------------------------------------------------------------------------

  let scraping = false;
  let abortController = null;
  let stats = { found: 0, scraped: 0, current: "", status: "idle" };
  const scrapedEntries = [];

  // ---------------------------------------------------------------------------
  // Helpers
  // ---------------------------------------------------------------------------

  function delay(ms) {
    return new Promise((resolve) => setTimeout(resolve, ms));
  }

  function waitForElement(selector, timeout = 10000) {
    return new Promise((resolve, reject) => {
      const el = document.querySelector(selector);
      if (el) return resolve(el);

      const observer = new MutationObserver(() => {
        const el = document.querySelector(selector);
        if (el) {
          observer.disconnect();
          resolve(el);
        }
      });

      observer.observe(document.body, { childList: true, subtree: true });

      setTimeout(() => {
        observer.disconnect();
        reject(new Error(`Timeout waiting for selector: ${selector}`));
      }, timeout);
    });
  }

  function extractEmailsFromText(html) {
    if (!html) return [];
    const re = /[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}/g;
    const matches = html.match(re) || [];
    // Deduplicate and filter junk
    const unique = [...new Set(matches)].filter((e) => {
      const lower = e.toLowerCase();
      return (
        !lower.endsWith(".png") &&
        !lower.endsWith(".jpg") &&
        !lower.endsWith(".gif") &&
        !lower.endsWith(".svg") &&
        !lower.includes("sentry") &&
        !lower.includes("example.com") &&
        !lower.includes("wixpress")
      );
    });
    return unique;
  }

  function parseCoordinatesFromURL(url) {
    const m = url.match(/@(-?\d+\.\d+),(-?\d+\.\d+),(\d+\.?\d*)z/);
    if (m) return { lat: parseFloat(m[1]), lng: parseFloat(m[2]), zoom: parseFloat(m[3]) };
    // Fallback: !3d / !4d parameters
    const latM = url.match(/!3d(-?\d+\.\d+)/);
    const lngM = url.match(/!4d(-?\d+\.\d+)/);
    if (latM && lngM) return { lat: parseFloat(latM[1]), lng: parseFloat(lngM[1]), zoom: 0 };
    return { lat: 0, lng: 0, zoom: 0 };
  }

  function parsePlaceIdFromURL(url) {
    const m = url.match(/place_id[=:]([A-Za-z0-9_\-]+)/);
    return m ? m[1] : "";
  }

  function parseCidFromURL(url) {
    const m = url.match(/[?&]cid=(\d+)/);
    return m ? m[1] : "";
  }

  function isWebsiteValidForEmail(website) {
    if (!website) return false;
    const blocked = ["facebook", "instagram", "twitter"];
    return !blocked.some((b) => website.includes(b));
  }

  function extractActualURL(googleURL) {
    if (!googleURL || !googleURL.startsWith("/url?q=")) return googleURL || "";
    try {
      const parsed = new URL(googleURL, "https://www.google.com");
      return parsed.searchParams.get("q") || googleURL;
    } catch {
      return googleURL;
    }
  }

  function sendProgress() {
    try {
      chrome.runtime.sendMessage({
        type: "scrapeProgress",
        data: { ...stats },
      });
    } catch {
      // Extension context may have been invalidated
    }
  }

  // ---------------------------------------------------------------------------
  // Safe nested-array accessor (mirrors Go's getNthElementAndCast)
  // ---------------------------------------------------------------------------

  function getN(arr, ...indexes) {
    let current = arr;
    for (let i = 0; i < indexes.length; i++) {
      if (!Array.isArray(current)) return undefined;
      if (indexes[i] >= current.length) return undefined;
      current = current[indexes[i]];
      if (current === null || current === undefined) return undefined;
    }
    return current;
  }

  function getNStr(arr, ...indexes) {
    const v = getN(arr, ...indexes);
    return typeof v === "string" ? v : "";
  }

  function getNNum(arr, ...indexes) {
    const v = getN(arr, ...indexes);
    return typeof v === "number" ? v : 0;
  }

  // ---------------------------------------------------------------------------
  // APP_INITIALIZATION_STATE extraction (mirrors place.go js constant)
  // ---------------------------------------------------------------------------

  function extractAppInitState() {
    try {
      if (
        !window.APP_INITIALIZATION_STATE ||
        !window.APP_INITIALIZATION_STATE[3]
      ) {
        return null;
      }
      const appState = window.APP_INITIALIZATION_STATE[3];
      for (const key of Object.keys(appState)) {
        const arr = appState[key];
        if (Array.isArray(arr)) {
          for (const idx of [6, 5]) {
            const item = arr[idx];
            if (typeof item === "string" && item.startsWith(")]}'")) {
              return item;
            }
          }
        }
      }
    } catch {
      // ignore
    }
    return null;
  }

  function parseRawJSON(rawStr) {
    if (!rawStr) return null;
    const prefix = ")]}'";
    let cleaned = rawStr.trimStart();
    if (cleaned.startsWith(prefix)) {
      cleaned = cleaned.slice(prefix.length).trimStart();
    }
    // Also strip newline after prefix
    if (cleaned.startsWith("\n")) cleaned = cleaned.slice(1);
    try {
      return JSON.parse(cleaned);
    } catch {
      return null;
    }
  }

  // ---------------------------------------------------------------------------
  // Entry from JSON data (mirrors EntryFromJSON in entry.go)
  // ---------------------------------------------------------------------------

  function buildEntryFromJSONData(jd, pageURL) {
    if (!Array.isArray(jd) || jd.length < 7) return null;
    const darray = jd[6];
    if (!Array.isArray(darray)) return null;

    const categoriesI = getN(darray, 13) || [];
    const categories = Array.isArray(categoriesI)
      ? categoriesI.filter((c) => typeof c === "string")
      : [];

    const rawAddress = getNStr(darray, 18);
    const title = getNStr(darray, 11);
    const address = rawAddress.replace(new RegExp("^" + escapeRegExp(title) + ",\\s*"), "");

    const entry = createEmptyEntry();

    entry.link = getNStr(darray, 27) || pageURL || "";
    entry.title = title;
    entry.categories = categories;
    entry.category = categories.length > 0 ? categories[0] : "";
    entry.address = address;
    entry.open_hours = extractHours(darray);
    entry.popular_times = extractPopularTimes(darray);
    entry.website = extractActualURL(getNStr(darray, 7, 0));
    entry.phone = getNStr(darray, 178, 0, 0);
    entry.plus_code = getNStr(darray, 183, 2, 2, 0);
    entry.review_count = getNNum(darray, 4, 8);
    entry.review_rating = getNNum(darray, 4, 7);
    entry.latitude = getNNum(darray, 9, 2);
    entry.longitude = getNNum(darray, 9, 3);
    entry.cid = getNStr(jd, 25, 3, 0, 13, 0, 0, 1);
    entry.status = getNStr(darray, 34, 4, 4);
    entry.description = getNStr(darray, 32, 1, 1);
    entry.reviews_link = getNStr(darray, 4, 3, 0);
    entry.thumbnail = getNStr(darray, 72, 0, 1, 6, 0);
    entry.timezone = getNStr(darray, 30);
    entry.price_range = getNStr(darray, 4, 2);
    entry.data_id = getNStr(darray, 10);
    entry.place_id = getNStr(darray, 78);

    // Images
    const imgItems = getN(darray, 171, 0) || [];
    if (Array.isArray(imgItems)) {
      for (const item of imgItems) {
        if (!Array.isArray(item)) continue;
        const imgLink = getNStr(item, 3, 0, 6, 0);
        const imgTitle = getNStr(item, 2);
        if (imgLink) entry.images.push({ title: imgTitle, image: imgLink });
      }
    }

    // Reservations
    const resItems = getN(darray, 46) || [];
    if (Array.isArray(resItems)) {
      for (const item of resItems) {
        if (!Array.isArray(item)) continue;
        const link = getNStr(item, 0);
        const source = getNStr(item, 1);
        if (link && source) entry.reservations.push({ link, source });
      }
    }

    // Order online
    let orderItems = getN(darray, 75, 0, 1, 2) || getN(darray, 75, 0, 0, 2) || [];
    if (Array.isArray(orderItems)) {
      for (const item of orderItems) {
        if (!Array.isArray(item)) continue;
        const link = getNStr(item, 1, 2, 0);
        const source = getNStr(item, 0, 0);
        if (link && source) entry.order_online.push({ link, source });
      }
    }

    // Menu
    entry.menu = {
      link: getNStr(darray, 38, 0),
      source: getNStr(darray, 38, 1),
    };

    // Owner
    const ownerId = getNStr(darray, 57, 2);
    entry.owner = {
      id: ownerId,
      name: getNStr(darray, 57, 1),
      link: ownerId ? `https://www.google.com/maps/contrib/${ownerId}` : "",
    };

    // Complete address
    entry.complete_address = {
      borough: getNStr(darray, 183, 1, 0),
      street: getNStr(darray, 183, 1, 1),
      city: getNStr(darray, 183, 1, 3),
      postal_code: getNStr(darray, 183, 1, 4),
      state: getNStr(darray, 183, 1, 5),
      country: getNStr(darray, 183, 1, 6),
    };

    // About
    const aboutI = getN(darray, 100, 1) || [];
    if (Array.isArray(aboutI)) {
      for (const aboutEl of aboutI) {
        if (!Array.isArray(aboutEl)) continue;
        const about = {
          id: getNStr(aboutEl, 0),
          name: getNStr(aboutEl, 1),
          options: [],
        };
        const optsI = getN(aboutEl, 2) || [];
        if (Array.isArray(optsI)) {
          for (const optEl of optsI) {
            if (!Array.isArray(optEl)) continue;
            const name = getNStr(optEl, 1);
            if (name) {
              about.options.push({
                name,
                enabled: getNNum(optEl, 2, 1, 0, 0) === 1,
              });
            }
          }
        }
        entry.about.push(about);
      }
    }

    // Reviews per rating
    entry.reviews_per_rating = {
      1: getNNum(darray, 175, 3, 0),
      2: getNNum(darray, 175, 3, 1),
      3: getNNum(darray, 175, 3, 2),
      4: getNNum(darray, 175, 3, 3),
      5: getNNum(darray, 175, 3, 4),
    };

    // Inline reviews
    let reviewsI = getN(darray, 175, 9, 0, 0) || getN(darray, 175, 9, 0) || [];
    if (Array.isArray(reviewsI) && reviewsI.length > 0) {
      entry.user_reviews = parseReviewsFromArray(reviewsI);
    }

    // Coordinates fallback from URL
    if (entry.latitude === 0 && entry.longitude === 0 && pageURL) {
      const coords = parseCoordinatesFromURL(pageURL);
      entry.latitude = coords.lat;
      entry.longitude = coords.lng;
    }

    // CID fallback from URL
    if (!entry.cid && pageURL) {
      entry.cid = parseCidFromURL(pageURL);
    }

    return entry;
  }

  function escapeRegExp(s) {
    return s.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  }

  // ---------------------------------------------------------------------------
  // Hours extraction (mirrors getHours in entry.go)
  // ---------------------------------------------------------------------------

  function extractHours(darray) {
    let items = getN(darray, 203, 0);
    if (!Array.isArray(items) || items.length === 0) {
      items = getN(darray, 34, 1);
    }
    if (!Array.isArray(items)) return {};

    const hours = {};
    for (const item of items) {
      if (!Array.isArray(item)) continue;
      const day = getNStr(item, 0);
      if (!day) continue;

      // New structure: [3] = time slots
      const timeSlots = getN(item, 3);
      if (Array.isArray(timeSlots) && timeSlots.length > 0) {
        const times = [];
        for (const slot of timeSlots) {
          if (!Array.isArray(slot)) continue;
          const timeStr = getNStr(slot, 0);
          if (timeStr) times.push(timeStr);
        }
        if (times.length > 0) hours[day] = times;
      } else {
        // Old structure: [1] = times
        const timesI = getN(item, 1);
        if (Array.isArray(timesI)) {
          const times = timesI.filter((t) => typeof t === "string");
          if (times.length > 0) hours[day] = times;
        }
      }
    }
    return hours;
  }

  // ---------------------------------------------------------------------------
  // Popular times extraction (mirrors getPopularTimes in entry.go)
  // ---------------------------------------------------------------------------

  function extractPopularTimes(darray) {
    const items = getN(darray, 84, 0);
    if (!Array.isArray(items)) return {};

    const dayOfWeek = {
      1: "Monday",
      2: "Tuesday",
      3: "Wednesday",
      4: "Thursday",
      5: "Friday",
      6: "Saturday",
      7: "Sunday",
    };

    const popularTimes = {};
    for (const item of items) {
      if (!Array.isArray(item)) continue;
      const dayNum = getNNum(item, 0);
      const dayName = dayOfWeek[dayNum];
      if (!dayName) continue;

      const timesI = getN(item, 1);
      if (!Array.isArray(timesI)) continue;

      const times = {};
      for (const t of timesI) {
        if (!Array.isArray(t) || t.length < 2) continue;
        const h = typeof t[0] === "number" ? t[0] : 0;
        const v = typeof t[1] === "number" ? t[1] : 0;
        times[h] = v;
      }
      popularTimes[dayName] = times;
    }
    return popularTimes;
  }

  // ---------------------------------------------------------------------------
  // Review parsing (mirrors parseReviews in entry.go)
  // ---------------------------------------------------------------------------

  function parseReviewsFromArray(reviewsI) {
    if (!Array.isArray(reviewsI)) return [];
    const reviews = [];

    for (const reviewRaw of reviewsI) {
      try {
        let el = getN(reviewRaw, 0);
        if (!Array.isArray(el) || el.length === 0) {
          el = Array.isArray(reviewRaw) ? reviewRaw : null;
          if (!el) continue;
        }

        // Author name (try multiple paths)
        let authorName = getNStr(el, 1, 4, 5, 0);
        if (!authorName) authorName = getNStr(el, 1, 4, 4);
        if (!authorName) authorName = getNStr(el, 0, 1);
        if (!authorName) continue;

        // Profile picture
        let profilePic = getNStr(el, 1, 4, 5, 1);
        if (!profilePic) profilePic = getNStr(el, 1, 2, 0);
        if (!profilePic) profilePic = getNStr(el, 0, 2, 0);

        // Rating
        let rating = getNNum(el, 2, 0, 0);
        if (!rating) rating = getNNum(el, 2, 0);
        if (!rating) rating = getNNum(el, 1, 0, 0);

        // Description
        let description = getNStr(el, 2, 15, 0, 0);
        if (!description) description = getNStr(el, 2, 15, 0);
        if (!description) description = getNStr(el, 3, 0);

        // Timestamp
        let time = getN(el, 2, 2, 0, 1, 21, 6, 8);
        if (!Array.isArray(time) || time.length === 0) {
          time = getN(el, 2, 2, 0, 1, 6, 8);
        }
        const when =
          Array.isArray(time) && time.length >= 3
            ? `${time[0]}-${time[1]}-${time[2]}`
            : "";

        // Images
        let optsI = getN(el, 2, 2, 0, 1, 21, 7);
        if (!Array.isArray(optsI) || optsI.length === 0) {
          optsI = getN(el, 2, 2, 0, 1, 7);
        }
        const images = [];
        if (Array.isArray(optsI)) {
          for (const imgVal of optsI) {
            if (typeof imgVal === "string" && imgVal.length > 2) {
              images.push(imgVal.slice(2));
            }
          }
        }

        reviews.push({
          name: authorName,
          profile_picture: profilePic || "",
          rating: rating || 0,
          description: description || "",
          images,
          when,
        });
      } catch {
        continue;
      }
    }

    return reviews;
  }

  // ---------------------------------------------------------------------------
  // DOM-based fallback extraction (when APP_INITIALIZATION_STATE unavailable)
  // ---------------------------------------------------------------------------

  function extractFromDOM(pageURL) {
    const entry = createEmptyEntry();
    entry.link = pageURL || window.location.href;

    try {
      // Title
      const h1 = document.querySelector("h1");
      if (h1) entry.title = h1.textContent.trim();

      // Category
      const categoryBtn = document.querySelector(
        'button[jsaction*="category"]'
      );
      if (categoryBtn) {
        entry.category = categoryBtn.textContent.trim();
        entry.categories = [entry.category];
      }

      // Address
      const addrBtn = document.querySelector(
        'button[data-item-id="address"]'
      );
      if (addrBtn) {
        const label = addrBtn.getAttribute("aria-label") || "";
        entry.address = label.replace(/^Address:\s*/i, "").trim();
      }

      // Phone
      const phoneBtn = document.querySelector(
        'button[data-item-id^="phone"]'
      );
      if (phoneBtn) {
        const label = phoneBtn.getAttribute("aria-label") || "";
        entry.phone = label.replace(/^Phone:\s*/i, "").trim();
      }

      // Website
      const webLink = document.querySelector('a[data-item-id="authority"]');
      if (webLink) {
        entry.website = webLink.href || "";
      }

      // Rating
      const ratingEl = document.querySelector(
        'div.fontDisplayLarge, span.fontDisplayLarge'
      );
      if (ratingEl) {
        entry.review_rating = parseFloat(ratingEl.textContent) || 0;
      }

      // Review count
      const reviewCountEl = document.querySelector(
        'span[aria-label*="review"]'
      );
      if (reviewCountEl) {
        const m = (reviewCountEl.getAttribute("aria-label") || "").match(
          /([\d,]+)\s*review/i
        );
        if (m) entry.review_count = parseInt(m[1].replace(/,/g, ""), 10) || 0;
      }

      // Coordinates from URL
      const coords = parseCoordinatesFromURL(
        pageURL || window.location.href
      );
      entry.latitude = coords.lat;
      entry.longitude = coords.lng;

      // Plus code
      const plusCodeBtn = document.querySelector(
        'button[data-item-id="oloc"]'
      );
      if (plusCodeBtn) {
        const label = plusCodeBtn.getAttribute("aria-label") || "";
        entry.plus_code = label.replace(/^Plus code:\s*/i, "").trim();
      }

      // Hours from tooltip table
      const hoursTable = document.querySelector(
        "table[data-hide-tooltip-on-mouse-move]"
      );
      if (hoursTable) {
        const rows = hoursTable.querySelectorAll("tr");
        for (const row of rows) {
          const cells = row.querySelectorAll("td");
          if (cells.length >= 2) {
            const day = cells[0].textContent.trim();
            const timeText = cells[1].textContent.trim();
            if (day && timeText) {
              entry.open_hours[day] = [timeText];
            }
          }
        }
      }
    } catch {
      // best effort
    }

    return entry;
  }

  // ---------------------------------------------------------------------------
  // Empty entry factory
  // ---------------------------------------------------------------------------

  function createEmptyEntry() {
    return {
      input_id: "",
      link: "",
      title: "",
      category: "",
      categories: [],
      address: "",
      complete_address: {
        borough: "",
        street: "",
        city: "",
        postal_code: "",
        state: "",
        country: "",
      },
      open_hours: {},
      popular_times: {},
      website: "",
      phone: "",
      plus_code: "",
      review_count: 0,
      review_rating: 0.0,
      reviews_per_rating: { 1: 0, 2: 0, 3: 0, 4: 0, 5: 0 },
      latitude: 0.0,
      longitude: 0.0,
      cid: "",
      status: "",
      description: "",
      reviews_link: "",
      thumbnail: "",
      timezone: "",
      price_range: "",
      data_id: "",
      place_id: "",
      images: [],
      reservations: [],
      order_online: [],
      menu: { link: "", source: "" },
      owner: { id: "", name: "", link: "" },
      about: [],
      user_reviews: [],
      emails: [],
    };
  }

  // ---------------------------------------------------------------------------
  // Extract full place data from the current page
  // ---------------------------------------------------------------------------

  async function extractPlaceData(url) {
    // Wait a bit for the page data to populate
    let rawStr = null;
    for (let attempt = 0; attempt < 15; attempt++) {
      rawStr = extractAppInitState();
      if (rawStr) break;
      await delay(500);
    }

    let entry = null;

    if (rawStr) {
      const jd = parseRawJSON(rawStr);
      if (jd) {
        entry = buildEntryFromJSONData(jd, url);
      }
    }

    // Fallback to DOM extraction
    if (!entry || !entry.title) {
      const domEntry = extractFromDOM(url);
      if (!entry) {
        entry = domEntry;
      } else {
        // Merge: fill blanks from DOM
        if (!entry.title) entry.title = domEntry.title;
        if (!entry.address) entry.address = domEntry.address;
        if (!entry.phone) entry.phone = domEntry.phone;
        if (!entry.website) entry.website = domEntry.website;
        if (!entry.review_rating) entry.review_rating = domEntry.review_rating;
        if (!entry.review_count) entry.review_count = domEntry.review_count;
        if (!entry.latitude) entry.latitude = domEntry.latitude;
        if (!entry.longitude) entry.longitude = domEntry.longitude;
      }
    }

    return entry;
  }

  // ---------------------------------------------------------------------------
  // Email extraction from a business website
  // ---------------------------------------------------------------------------

  async function fetchEmailsFromWebsite(websiteURL, signal) {
    if (!websiteURL || !isWebsiteValidForEmail(websiteURL)) return [];

    try {
      const resp = await fetch(websiteURL, {
        signal,
        mode: "cors",
        redirect: "follow",
        headers: { Accept: "text/html" },
      });

      if (!resp.ok) return [];

      const html = await resp.text();
      return extractEmailsFromText(html);
    } catch {
      // CORS errors are common — try no-cors as a last resort (opaque response)
      // We cannot read opaque responses, so just return empty.
      return [];
    }
  }

  // ---------------------------------------------------------------------------
  // DOM review extraction (for reviews visible in the current page)
  // ---------------------------------------------------------------------------

  function extractReviewsFromDOM() {
    const reviews = [];
    const reviewEls = document.querySelectorAll(
      'div[data-review-id], div[class*="review"][jsaction]'
    );

    for (const el of reviewEls) {
      try {
        const nameEl = el.querySelector(
          'div[class*="d4r55"], button[class*="reviewer"], a[class*="reviewer"]'
        );
        const ratingEl = el.querySelector('span[role="img"]');
        const textEl = el.querySelector(
          'span[class*="wiI7pd"], div[class*="review-text"]'
        );
        const dateEl = el.querySelector(
          'span[class*="rsqaWe"], span[class*="review-date"]'
        );
        const imgEl = el.querySelector("img");

        const name = nameEl ? nameEl.textContent.trim() : "";
        if (!name) continue;

        let rating = 0;
        if (ratingEl) {
          const ariaLabel = ratingEl.getAttribute("aria-label") || "";
          const m = ariaLabel.match(/(\d)/);
          if (m) rating = parseInt(m[1], 10);
        }

        reviews.push({
          name,
          profile_picture: imgEl ? imgEl.src || "" : "",
          rating,
          description: textEl ? textEl.textContent.trim() : "",
          images: [],
          when: dateEl ? dateEl.textContent.trim() : "",
        });
      } catch {
        continue;
      }
    }

    return reviews;
  }

  // ---------------------------------------------------------------------------
  // Scroll results container to load more (mirrors scroll() in job.go)
  // ---------------------------------------------------------------------------

  async function scrollResults(container, depth, signal) {
    let currentScrollHeight = 0;
    let staleCount = 0;
    const maxStale = 3;
    let waitTime = 100;
    const maxWait = 2000;
    let cnt = 0;

    for (let i = 0; i < depth; i++) {
      if (signal && signal.aborted) break;

      cnt++;
      container.scrollTop = container.scrollHeight;

      const scrollWait = Math.min(500 * cnt, maxWait);
      await delay(scrollWait);

      const newHeight = container.scrollHeight;
      if (newHeight === currentScrollHeight) {
        staleCount++;
        if (staleCount >= maxStale) break;
        // Wait longer and retry without counting as a depth iteration
        await delay(1500);
        i--;
        continue;
      }

      staleCount = 0;
      currentScrollHeight = newHeight;

      waitTime = Math.min(waitTime * 1.5, maxWait);
      await delay(waitTime);

      // Check for "end of results" indicator
      const endEl = container.querySelector(
        'span.fontBodyMedium > span > span'
      );
      if (endEl) {
        const text = endEl.textContent.toLowerCase();
        if (
          text.includes("end of list") ||
          text.includes("you've reached the end") ||
          text.includes("no more results")
        ) {
          break;
        }
      }
    }

    return cnt;
  }

  // ---------------------------------------------------------------------------
  // Collect place links from the feed (mirrors GmapJob.Process)
  // ---------------------------------------------------------------------------

  function collectPlaceLinks() {
    const links = new Set();
    const anchors = document.querySelectorAll(
      'div[role="feed"] div[jsaction] > a'
    );
    for (const a of anchors) {
      const href = a.getAttribute("href");
      if (href && href.includes("/maps/place/")) {
        links.add(href.startsWith("http") ? href : `https://www.google.com${href}`);
      }
    }
    return [...links];
  }

  // ---------------------------------------------------------------------------
  // Navigate to a place page in-browser and extract data
  // ---------------------------------------------------------------------------

  async function scrapePlace(placeURL, options, signal) {
    // Click the result link in the feed rather than full navigation when possible
    const anchor = document.querySelector(
      `div[role="feed"] a[href*="${new URL(placeURL).pathname.split("/").slice(0, 5).join("/")}"]`
    );

    if (anchor) {
      anchor.click();
      // Wait for the place panel to load
      await delay(2000);
    } else {
      // Direct navigation
      window.location.href = placeURL;
      await delay(3000);
    }

    // Wait for place content to appear
    try {
      await waitForElement("h1", 10000);
    } catch {
      // Continue anyway
    }

    await delay(1000);

    const entry = await extractPlaceData(placeURL);
    if (!entry) return null;

    // Extract reviews from DOM if enabled and we're on the place page
    if (options.extractReviews) {
      const domReviews = extractReviewsFromDOM();
      if (domReviews.length > 0 && entry.user_reviews.length === 0) {
        entry.user_reviews = domReviews;
      }
    }

    // Extract emails if enabled
    if (options.extractEmails && entry.website && isWebsiteValidForEmail(entry.website)) {
      try {
        const emails = await fetchEmailsFromWebsite(entry.website, signal);
        entry.emails = emails;
      } catch {
        // Non-fatal
      }
    }

    return entry;
  }

  // ---------------------------------------------------------------------------
  // Main scrape orchestrator
  // ---------------------------------------------------------------------------

  async function startScraping(params) {
    if (scraping) return;
    scraping = true;
    abortController = new AbortController();
    const signal = abortController.signal;
    scrapedEntries.length = 0;

    const depth = params.depth || 20;
    const extractEmails = params.extractEmails || false;
    const extractReviews = params.extractReviews || false;

    stats = { found: 0, scraped: 0, current: "", status: "scrolling" };
    sendProgress();

    try {
      const url = window.location.href;

      // ---------- Single place page ----------
      if (url.includes("/maps/place/")) {
        stats.status = "scraping";
        stats.found = 1;
        stats.current = url;
        sendProgress();

        const entry = await extractPlaceData(url);
        if (entry) {
          if (extractReviews) {
            const domReviews = extractReviewsFromDOM();
            if (domReviews.length > 0 && entry.user_reviews.length === 0) {
              entry.user_reviews = domReviews;
            }
          }
          if (extractEmails && entry.website && isWebsiteValidForEmail(entry.website)) {
            try {
              entry.emails = await fetchEmailsFromWebsite(entry.website, signal);
            } catch { /* non-fatal */ }
          }
          scrapedEntries.push(entry);
          stats.scraped = 1;
        }

        stats.status = "done";
        sendProgress();
        sendResults();
        scraping = false;
        return;
      }

      // ---------- Search results page ----------

      // Dismiss cookie consent if present
      dismissCookieConsent();

      // Wait for the feed
      let feedContainer = null;
      try {
        feedContainer = await waitForElement('div[role="feed"]', 15000);
      } catch {
        stats.status = "error";
        stats.current = "Could not find results feed. Make sure you are on a Google Maps search page.";
        sendProgress();
        scraping = false;
        return;
      }

      if (signal.aborted) { cleanup("stopped"); return; }

      // Scroll to load results
      stats.status = "scrolling";
      sendProgress();
      await scrollResults(feedContainer, depth, signal);

      if (signal.aborted) { cleanup("stopped"); return; }

      // Collect all place links
      const placeLinks = collectPlaceLinks();
      stats.found = placeLinks.length;
      stats.status = "scraping";
      sendProgress();

      if (placeLinks.length === 0) {
        stats.status = "done";
        stats.current = "No places found.";
        sendProgress();
        scraping = false;
        return;
      }

      // Scrape each place by clicking its result in the panel
      for (let i = 0; i < placeLinks.length; i++) {
        if (signal.aborted) { cleanup("stopped"); return; }

        const placeURL = placeLinks[i];
        stats.current = `(${i + 1}/${placeLinks.length}) ${placeURL.split("/place/")[1]?.split("/")[0] || placeURL}`;
        sendProgress();

        try {
          // Click the corresponding anchor in the feed to open the side panel
          const encodedPath = new URL(placeURL).pathname;
          const feedAnchors = document.querySelectorAll(
            'div[role="feed"] a[href]'
          );
          let clicked = false;

          for (const a of feedAnchors) {
            if (a.href === placeURL || a.getAttribute("href") === placeURL) {
              a.click();
              clicked = true;
              break;
            }
          }

          if (!clicked) {
            // Try partial match
            for (const a of feedAnchors) {
              if (a.href && a.href.includes(encodedPath.slice(0, 60))) {
                a.click();
                clicked = true;
                break;
              }
            }
          }

          if (!clicked) {
            // Skip this place if we can't navigate to it without leaving the page
            continue;
          }

          // Wait for the place panel to update
          await delay(2500);

          try {
            await waitForElement("h1", 8000);
          } catch {
            // proceed anyway
          }

          await delay(500);

          const entry = await extractPlaceData(placeURL);
          if (entry && entry.title) {
            entry.input_id = `place_${i}`;

            if (extractReviews) {
              const domReviews = extractReviewsFromDOM();
              if (domReviews.length > 0 && entry.user_reviews.length === 0) {
                entry.user_reviews = domReviews;
              }
            }

            if (extractEmails && entry.website && isWebsiteValidForEmail(entry.website)) {
              try {
                entry.emails = await fetchEmailsFromWebsite(entry.website, signal);
              } catch { /* non-fatal */ }
            }

            scrapedEntries.push(entry);
            stats.scraped = scrapedEntries.length;
            sendProgress();

            // Send individual entry to background
            try {
              chrome.runtime.sendMessage({
                type: "scrapeEntry",
                data: entry,
              });
            } catch { /* extension context may be invalid */ }
          }
        } catch (err) {
          console.warn(`[GMS] Error scraping place ${i + 1}:`, err);
          continue;
        }
      }

      stats.status = "done";
      sendProgress();
      sendResults();
    } catch (err) {
      console.error("[GMS] Scrape error:", err);
      stats.status = "error";
      stats.current = err.message || "Unknown error";
      sendProgress();
    } finally {
      scraping = false;
    }
  }

  function cleanup(status) {
    stats.status = status;
    sendProgress();
    sendResults();
    scraping = false;
  }

  function sendResults() {
    try {
      chrome.runtime.sendMessage({
        type: "scrapeResults",
        data: scrapedEntries,
      });
    } catch { /* context may be invalid */ }
  }

  function dismissCookieConsent() {
    try {
      const form = document.querySelector('form[action*="consent.google"]');
      if (form) {
        const btn = form.querySelector('button, input[type="submit"]');
        if (btn) { btn.click(); return; }
      }
      const buttons = document.querySelectorAll('button, input[type="submit"]');
      for (const btn of buttons) {
        const text = (btn.textContent || btn.value || "").toLowerCase();
        if (
          text.includes("reject") ||
          text.includes("decline") ||
          text.includes("ablehnen")
        ) {
          btn.click();
          return;
        }
      }
    } catch { /* ignore */ }
  }

  // ---------------------------------------------------------------------------
  // Stop scraping
  // ---------------------------------------------------------------------------

  function stopScraping() {
    if (abortController) {
      abortController.abort();
    }
    scraping = false;
    stats.status = "stopped";
    sendProgress();
  }

  // ---------------------------------------------------------------------------
  // Message listener
  // ---------------------------------------------------------------------------

  chrome.runtime.onMessage.addListener((message, _sender, sendResponse) => {
    switch (message.action) {
      case "startScrape":
        startScraping(message.params || {});
        sendResponse({ ok: true, status: "started" });
        break;

      case "stopScrape":
        stopScraping();
        sendResponse({ ok: true, status: "stopped" });
        break;

      case "getStatus":
        sendResponse({
          ok: true,
          scraping,
          stats: { ...stats },
          results: scrapedEntries,
        });
        break;

      case "getResults":
        sendResponse({ ok: true, results: scrapedEntries });
        break;

      default:
        sendResponse({ ok: false, error: "Unknown action" });
    }

    // Return true to indicate async response
    return true;
  });

  // ---------------------------------------------------------------------------
  // Notify that content script is ready
  // ---------------------------------------------------------------------------

  try {
    chrome.runtime.sendMessage({ type: "contentScriptReady" });
  } catch { /* ignore */ }

  console.log("[GMS] Google Maps Scraper content script loaded.");
})();
