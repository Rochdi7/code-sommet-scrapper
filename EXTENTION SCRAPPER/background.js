// background.js - Service Worker for CodeSommet Maps Scraper

const CSV_HEADERS = [
  'input_id', 'link', 'title', 'category', 'address', 'open_hours',
  'popular_times', 'website', 'phone', 'plus_code', 'review_count',
  'review_rating', 'reviews_per_rating', 'latitude', 'longitude', 'cid',
  'status', 'descriptions', 'reviews_link', 'thumbnail', 'timezone',
  'price_range', 'data_id', 'place_id', 'images', 'reservations',
  'order_online', 'menu', 'owner', 'complete_address', 'about',
  'user_reviews', 'user_reviews_extended', 'emails'
];

// Initialize storage on install
chrome.runtime.onInstalled.addListener(() => {
  chrome.storage.local.set({
    results: [],
    status: 'idle',
    progress: { found: 0, scraped: 0, total: 0 }
  });
});

// --- Helpers ---

function escapeCSVValue(val) {
  if (val === null || val === undefined) return '';
  if (typeof val === 'object') {
    val = JSON.stringify(val);
  } else {
    val = String(val);
  }
  // Escape if value contains comma, quote, or newline
  if (val.includes('"') || val.includes(',') || val.includes('\n') || val.includes('\r')) {
    val = '"' + val.replace(/"/g, '""') + '"';
  }
  return val;
}

function generateCSV(results) {
  const lines = [];
  lines.push(CSV_HEADERS.join(','));
  for (const entry of results) {
    const row = CSV_HEADERS.map(header => escapeCSVValue(entry[header]));
    lines.push(row.join(','));
  }
  return lines.join('\r\n');
}

function getTimestamp() {
  const now = new Date();
  return now.toISOString().replace(/[:.]/g, '-').slice(0, 19);
}

async function downloadFile(content, filename, mimeType) {
  const blob = new Blob([content], { type: mimeType });
  const url = URL.createObjectURL(blob);
  try {
    await chrome.downloads.download({
      url: url,
      filename: filename,
      saveAs: true
    });
  } catch (err) {
    console.error('Download failed:', err);
  }
}

async function getStorage(keys) {
  return chrome.storage.local.get(keys);
}

async function setStorage(data) {
  return chrome.storage.local.set(data);
}

// --- Message Handling ---

chrome.runtime.onMessage.addListener((message, sender, sendResponse) => {
  handleMessage(message, sender).then(sendResponse).catch(err => {
    console.error('Message handler error:', err);
    sendResponse({ error: err.message });
  });
  return true; // Keep the message channel open for async response
});

async function handleMessage(message, sender) {
  const { type } = message;

  switch (type) {
    case 'startScrape': {
      await setStorage({
        status: 'scraping',
        progress: { found: 0, scraped: 0, total: 0 }
      });
      // Forward to content script on the active tab
      const [tab] = await chrome.tabs.query({ active: true, currentWindow: true });
      if (!tab) return { error: 'No active tab found' };
      try {
        const response = await chrome.tabs.sendMessage(tab.id, {
          type: 'startScrape',
          depth: message.depth,
          extractEmails: message.extractEmails,
          extractReviews: message.extractReviews
        });
        return response || { success: true };
      } catch (err) {
        await setStorage({ status: 'error' });
        return { error: 'Could not reach content script. Make sure you are on a Google Maps page.' };
      }
    }

    case 'stopScrape': {
      await setStorage({ status: 'idle' });
      const [tab] = await chrome.tabs.query({ active: true, currentWindow: true });
      if (tab) {
        try {
          await chrome.tabs.sendMessage(tab.id, { type: 'stopScrape' });
        } catch (_) { /* Content script may not be available */ }
      }
      return { success: true };
    }

    case 'scrapeProgress': {
      const d = message.data || message;
      const progress = {
        found: d.found || 0,
        scraped: d.scraped || 0,
        total: d.found || d.total || 0
      };
      const newStatus = d.status === 'done' ? 'done' : d.status === 'error' ? 'error' : 'scraping';
      await setStorage({ progress, status: newStatus });
      return { success: true };
    }

    case 'scrapeEntry':
    case 'scrapeResult': {
      const { results = [] } = await getStorage('results');
      results.push(message.data);
      await setStorage({ results });
      return { success: true, count: results.length };
    }

    case 'scrapeResults': {
      // Bulk results from content script - replace all results
      if (Array.isArray(message.data)) {
        await setStorage({ results: message.data, status: 'done' });
      }
      return { success: true };
    }

    case 'scrapeComplete': {
      await setStorage({ status: 'done' });
      return { success: true };
    }

    case 'scrapeError': {
      await setStorage({ status: 'error' });
      return { error: message.error || 'Unknown scraping error' };
    }

    case 'contentScriptReady': {
      return { success: true };
    }

    case 'getResults': {
      const data = await getStorage(['results', 'status', 'progress']);
      return {
        results: data.results || [],
        status: data.status || 'idle',
        progress: data.progress || { found: 0, scraped: 0, total: 0 }
      };
    }

    case 'clearResults': {
      await setStorage({
        results: [],
        status: 'idle',
        progress: { found: 0, scraped: 0, total: 0 }
      });
      return { success: true };
    }

    case 'exportCSV': {
      const { results = [] } = await getStorage('results');
      if (results.length === 0) return { error: 'No results to export' };
      const csv = generateCSV(results);
      const filename = `maps-scrape-${getTimestamp()}.csv`;
      await downloadFile(csv, filename, 'text/csv;charset=utf-8;');
      return { success: true, filename };
    }

    case 'exportJSON': {
      const { results = [] } = await getStorage('results');
      if (results.length === 0) return { error: 'No results to export' };
      const json = JSON.stringify(results, null, 2);
      const filename = `maps-scrape-${getTimestamp()}.json`;
      await downloadFile(json, filename, 'application/json');
      return { success: true, filename };
    }

    default:
      return { error: `Unknown message type: ${type}` };
  }
}
