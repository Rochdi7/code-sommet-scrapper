// popup.js - Popup script for CodeSommet Maps Scraper

// --- DOM References ---
const btnScrape = document.getElementById('btn-scrape');
const btnCSV = document.getElementById('btn-csv');
const btnJSON = document.getElementById('btn-json');
const btnClear = document.getElementById('btn-clear');
const depthSelect = document.getElementById('depth');
const optEmails = document.getElementById('opt-emails');
const optReviews = document.getElementById('opt-reviews');
const statusBadge = document.getElementById('status-badge');
const progressBar = document.getElementById('progress-bar');
const progressText = document.getElementById('progress-text');
const progressSection = document.getElementById('progress-section');
const resultsCount = document.getElementById('results-count');
const resultsList = document.getElementById('results-list');
const emptyState = document.getElementById('empty-state');
const filterBtns = document.querySelectorAll('.filter-btn');

// --- State ---
let currentResults = [];
let currentFilter = 'all';
let pollInterval = null;
let isScraping = false;

// --- Helper Functions ---

function renderStars(rating) {
  if (!rating || isNaN(rating)) return '<span class="stars">N/A</span>';
  const r = parseFloat(rating);
  let stars = '';
  for (let i = 1; i <= 5; i++) {
    if (i <= Math.floor(r)) {
      stars += '<span class="star star-full">&#9733;</span>';
    } else if (i - r < 1 && i - r > 0) {
      stars += '<span class="star star-half">&#9733;</span>';
    } else {
      stars += '<span class="star star-empty">&#9734;</span>';
    }
  }
  return `<span class="stars">${stars} <small>${r.toFixed(1)}</small></span>`;
}

function truncate(str, len) {
  if (!str) return '';
  str = String(str);
  return str.length > len ? str.substring(0, len) + '...' : str;
}

function formatNumber(n) {
  if (n === null || n === undefined) return '0';
  return Number(n).toLocaleString();
}

function updateProgress(found, scraped) {
  const pct = found > 0 ? Math.round((scraped / found) * 100) : 0;
  progressBar.style.width = pct + '%';
  progressText.textContent = `Scraped ${formatNumber(scraped)} / ${formatNumber(found)} businesses`;
}

function setStatus(status) {
  statusBadge.className = 'status-badge';
  switch (status) {
    case 'scraping':
      statusBadge.textContent = 'Scraping...';
      statusBadge.classList.add('status-scraping');
      btnScrape.disabled = false;
      btnScrape.innerHTML = `
        <svg width="16" height="16" viewBox="0 0 16 16" fill="none">
          <rect x="3" y="3" width="10" height="10" rx="1" stroke="currentColor" stroke-width="1.5"/>
        </svg>
        Stop Scraping`;
      isScraping = true;
      break;
    case 'done':
      statusBadge.textContent = 'Done';
      statusBadge.classList.add('status-done');
      resetScrapeButton();
      isScraping = false;
      stopPolling();
      break;
    case 'error':
      statusBadge.textContent = 'Error';
      statusBadge.classList.add('status-error');
      resetScrapeButton();
      isScraping = false;
      stopPolling();
      break;
    default:
      statusBadge.textContent = 'Idle';
      statusBadge.classList.add('status-idle');
      resetScrapeButton();
      isScraping = false;
      stopPolling();
  }
}

function resetScrapeButton() {
  btnScrape.disabled = false;
  btnScrape.innerHTML = `
    <svg width="16" height="16" viewBox="0 0 16 16" fill="none">
      <path d="M2 3h12M2 8h12M2 13h8" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"/>
    </svg>
    Scrape Current Page`;
}

function applyFilter(results, filter) {
  switch (filter) {
    case 'no-website':
      return results.filter(r => !r.website || r.website.trim() === '');
    case 'low-rating':
      return results.filter(r => {
        const rating = parseFloat(r.review_rating);
        return !isNaN(rating) && rating < 4.0;
      });
    case 'has-email':
      return results.filter(r => {
        if (Array.isArray(r.emails)) return r.emails.length > 0;
        if (typeof r.emails === 'string') return r.emails.trim() !== '';
        return false;
      });
    case 'has-phone':
      return results.filter(r => r.phone && r.phone.trim() !== '');
    default:
      return results;
  }
}

function renderResults(results, filter) {
  const filtered = applyFilter(results, filter);

  // Update count
  resultsCount.textContent = filtered.length;

  // Clear list but keep empty state
  const cards = resultsList.querySelectorAll('.result-card');
  cards.forEach(c => c.remove());

  if (filtered.length === 0) {
    emptyState.style.display = 'flex';
    return;
  }

  emptyState.style.display = 'none';

  const fragment = document.createDocumentFragment();
  filtered.forEach((entry, idx) => {
    const card = document.createElement('div');
    card.className = 'result-card';
    card.dataset.index = idx;

    const title = truncate(entry.title || 'Unknown', 35);
    const category = truncate(entry.category || '', 25);
    const address = truncate(entry.address || '', 40);
    const website = truncate(entry.website || '', 30);
    const phone = entry.phone || '';
    const stars = renderStars(entry.review_rating);
    const reviewCount = entry.review_count ? `(${formatNumber(entry.review_count)})` : '';

    card.innerHTML = `
      <div class="card-top">
        <div class="card-info">
          <div class="card-title">${escapeHTML(title)}</div>
          <div class="card-category">${escapeHTML(category)}</div>
        </div>
        <button class="card-copy-btn" title="Copy all data">
          <svg width="12" height="12" viewBox="0 0 12 12" fill="none">
            <rect x="3.5" y="3.5" width="7" height="7" rx="1" stroke="currentColor" stroke-width="1.2"/>
            <path d="M8.5 3.5V2a1 1 0 00-1-1H2a1 1 0 00-1 1v5.5a1 1 0 001 1h1.5" stroke="currentColor" stroke-width="1.2"/>
          </svg>
        </button>
      </div>
      <div class="card-rating">${stars} ${reviewCount}</div>
      ${phone ? `<div class="card-detail"><span class="detail-label">Phone:</span> ${escapeHTML(phone)}</div>` : ''}
      ${address ? `<div class="card-detail"><span class="detail-label">Addr:</span> ${escapeHTML(address)}</div>` : ''}
      ${website ? `<div class="card-detail"><span class="detail-label">Web:</span> ${escapeHTML(website)}</div>` : ''}
    `;

    // Click card to copy all data
    card.addEventListener('click', (e) => {
      if (e.target.closest('.card-copy-btn')) return;
      copyToClipboard(entry);
    });

    // Copy button
    const copyBtn = card.querySelector('.card-copy-btn');
    copyBtn.addEventListener('click', (e) => {
      e.stopPropagation();
      copyToClipboard(entry);
    });

    fragment.appendChild(card);
  });

  resultsList.appendChild(fragment);
}

function escapeHTML(str) {
  const div = document.createElement('div');
  div.textContent = str;
  return div.innerHTML;
}

async function copyToClipboard(entry) {
  try {
    const text = JSON.stringify(entry, null, 2);
    await navigator.clipboard.writeText(text);
    showToast('Copied to clipboard!');
  } catch (err) {
    console.error('Copy failed:', err);
  }
}

function showToast(msg) {
  let toast = document.querySelector('.toast');
  if (!toast) {
    toast = document.createElement('div');
    toast.className = 'toast';
    document.body.appendChild(toast);
  }
  toast.textContent = msg;
  toast.classList.add('toast-show');
  setTimeout(() => toast.classList.remove('toast-show'), 1500);
}

// --- Polling ---

function startPolling() {
  stopPolling();
  pollInterval = setInterval(async () => {
    try {
      const response = await chrome.runtime.sendMessage({ type: 'getResults' });
      if (response) {
        currentResults = response.results || [];
        setStatus(response.status);
        if (response.progress) {
          updateProgress(response.progress.found, response.progress.scraped);
        }
        renderResults(currentResults, currentFilter);
      }
    } catch (err) {
      console.error('Poll error:', err);
    }
  }, 500);
}

function stopPolling() {
  if (pollInterval) {
    clearInterval(pollInterval);
    pollInterval = null;
  }
}

// --- Event Listeners ---

// Scrape / Stop button
btnScrape.addEventListener('click', async () => {
  if (isScraping) {
    // Stop scraping
    try {
      await chrome.runtime.sendMessage({ type: 'stopScrape' });
      setStatus('idle');
    } catch (err) {
      console.error('Stop error:', err);
    }
    return;
  }

  // Check if on Google Maps
  const [tab] = await chrome.tabs.query({ active: true, currentWindow: true });
  if (!tab || !tab.url || !tab.url.includes('google.com/maps')) {
    showToast('Please navigate to Google Maps first!');
    return;
  }

  const depth = parseInt(depthSelect.value, 10);
  const extractEmails = optEmails.checked;
  const extractReviews = optReviews.checked;

  setStatus('scraping');
  updateProgress(0, 0);
  startPolling();

  try {
    const response = await chrome.runtime.sendMessage({
      type: 'startScrape',
      depth,
      extractEmails,
      extractReviews
    });
    if (response && response.error) {
      showToast(response.error);
      setStatus('error');
    }
  } catch (err) {
    console.error('Scrape start error:', err);
    showToast('Failed to start scraping');
    setStatus('error');
  }
});

// Export CSV
btnCSV.addEventListener('click', async () => {
  try {
    const response = await chrome.runtime.sendMessage({ type: 'exportCSV' });
    if (response && response.error) {
      showToast(response.error);
    } else {
      showToast('CSV export started!');
    }
  } catch (err) {
    showToast('Export failed');
  }
});

// Export JSON
btnJSON.addEventListener('click', async () => {
  try {
    const response = await chrome.runtime.sendMessage({ type: 'exportJSON' });
    if (response && response.error) {
      showToast(response.error);
    } else {
      showToast('JSON export started!');
    }
  } catch (err) {
    showToast('Export failed');
  }
});

// Clear
btnClear.addEventListener('click', async () => {
  try {
    await chrome.runtime.sendMessage({ type: 'clearResults' });
    currentResults = [];
    setStatus('idle');
    updateProgress(0, 0);
    renderResults([], currentFilter);
    showToast('Results cleared');
  } catch (err) {
    showToast('Clear failed');
  }
});

// Filter buttons
filterBtns.forEach(btn => {
  btn.addEventListener('click', () => {
    filterBtns.forEach(b => b.classList.remove('active'));
    btn.classList.add('active');
    currentFilter = btn.dataset.filter;
    renderResults(currentResults, currentFilter);
  });
});

// --- Storage change listener for real-time updates ---
chrome.storage.onChanged.addListener((changes, area) => {
  if (area !== 'local') return;

  if (changes.status) {
    setStatus(changes.status.newValue);
  }

  if (changes.progress) {
    const p = changes.progress.newValue || { found: 0, scraped: 0 };
    updateProgress(p.found, p.scraped);
  }

  if (changes.results) {
    currentResults = changes.results.newValue || [];
    renderResults(currentResults, currentFilter);
  }
});

// --- Initialize on load ---
document.addEventListener('DOMContentLoaded', async () => {
  try {
    const response = await chrome.runtime.sendMessage({ type: 'getResults' });
    if (response) {
      currentResults = response.results || [];
      setStatus(response.status);
      if (response.progress) {
        updateProgress(response.progress.found, response.progress.scraped);
      }
      renderResults(currentResults, currentFilter);

      // Resume polling if currently scraping
      if (response.status === 'scraping') {
        isScraping = true;
        startPolling();
      }
    }
  } catch (err) {
    console.error('Init error:', err);
  }
});
