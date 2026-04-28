const express = require("express");
const cors = require("cors");
const QRCode = require("qrcode");
const fs = require("fs");
const path = require("path");
const { Client } = require("whatsapp-web.js");
// ---------------------------------------------------------------------------
// Config
// ---------------------------------------------------------------------------
const PORT = process.env.PORT || 3001;
const DATA_DIR = process.env.DATA_DIR || path.join(__dirname, "data");
const CAMPAIGNS_FILE = path.join(DATA_DIR, "campaigns.json");

// Ensure data dir exists
if (!fs.existsSync(DATA_DIR)) fs.mkdirSync(DATA_DIR, { recursive: true });

// ---------------------------------------------------------------------------
// State
// ---------------------------------------------------------------------------
let client = null;
let latestQR = null;
let connected = false;
let phoneNumber = null;

// Bulk-send state (active campaign in memory)
let bulk = {
  running: false,
  aborted: false,
  paused: false,
  total: 0,
  sent: 0,
  failed: 0,
  current: null,
  messages: [],
  nextSendAt: null,
  campaignId: null,
};

// Daily send counter (resets at midnight)
let dailySendCount = 0;
let dailySendDate = new Date().toISOString().slice(0, 10);

function getDailySendCount() {
  const today = new Date().toISOString().slice(0, 10);
  if (today !== dailySendDate) {
    dailySendCount = 0;
    dailySendDate = today;
  }
  return dailySendCount;
}

function incrementDailySend() {
  getDailySendCount();
  dailySendCount++;
}

// Safe delay constants
const SAFE_MIN_DELAY = 30;

// ---------------------------------------------------------------------------
// Campaign persistence
// ---------------------------------------------------------------------------
function loadCampaigns() {
  try {
    if (fs.existsSync(CAMPAIGNS_FILE)) {
      return JSON.parse(fs.readFileSync(CAMPAIGNS_FILE, "utf8"));
    }
  } catch (e) {
    console.error("[wa-bridge] Error loading campaigns:", e.message);
  }
  return [];
}

function saveCampaigns(campaigns) {
  try {
    fs.writeFileSync(CAMPAIGNS_FILE, JSON.stringify(campaigns, null, 2), "utf8");
  } catch (e) {
    console.error("[wa-bridge] Error saving campaigns:", e.message);
  }
}

function saveCampaignState() {
  if (!bulk.campaignId) return;
  const campaigns = loadCampaigns();
  const idx = campaigns.findIndex(c => c.id === bulk.campaignId);
  if (idx === -1) return;
  const c = campaigns[idx];
  c.sent = bulk.sent;
  c.failed = bulk.failed;
  c.messages = bulk.messages;
  c.updatedAt = new Date().toISOString();
  if (bulk.paused) {
    c.status = "paused";
  } else if (!bulk.running) {
    const pending = bulk.messages.filter(m => m.status === "pending" || m.status === "sending").length;
    c.status = pending > 0 ? "paused" : "completed";
  }
  campaigns[idx] = c;
  saveCampaigns(campaigns);
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function cleanPhone(raw) {
  if (!raw) return null;
  let num = String(raw).replace(/[\s\-\+\(\)]/g, "");
  if (/^0[567]\d{8}$/.test(num)) {
    num = "212" + num.slice(1);
  }
  return num;
}



// ---------------------------------------------------------------------------
// WhatsApp connection using whatsapp-web.js
// ---------------------------------------------------------------------------

function initializeClient() {
  client = new Client({
    session: null,
    puppeteer: {
      args: ["--no-sandbox", "--disable-setuid-sandbox"],
    },
  });

  client.on("qr", async (qr) => {
    console.log("[wa-bridge] QR code received — scan with WhatsApp");
    try {
      latestQR = await QRCode.toDataURL(qr, { width: 300 });
    } catch (err) {
      console.error("[wa-bridge] Error generating QR image:", err);
    }
  });

  client.on("ready", () => {
    connected = true;
    latestQR = null;
    phoneNumber = client.info?.wid?.user || null;
    console.log(`[wa-bridge] Connected as ${phoneNumber}`);
  });

  client.on("authenticated", () => {
    console.log("[wa-bridge] Authenticated");
  });

  client.on("auth_failure", () => {
    console.log("[wa-bridge] Authentication failed");
    connected = false;
    latestQR = null;
    phoneNumber = null;
  });

  client.on("disconnected", (reason) => {
    console.log(`[wa-bridge] Disconnected: ${reason}`);
    connected = false;
    latestQR = null;
    phoneNumber = null;
    client = null;
    setTimeout(() => initializeClient(), 5000);
  });

  client.initialize();
}

// Start on boot
initializeClient();

// ---------------------------------------------------------------------------
// Express app
// ---------------------------------------------------------------------------
const app = express();
app.use(cors());
app.use(express.json());

// ---- GET /status ----------------------------------------------------------
app.get("/status", (_req, res) => {
  res.json({ connected, phone: phoneNumber });
});

// ---- GET /qr --------------------------------------------------------------
app.get("/qr", async (_req, res) => {
  if (connected) {
    return res.json({ connected: true, phone: phoneNumber });
  }
  if (latestQR) {
    return res.json({ qr: latestQR });
  }
  return res.status(202).json({
    connected: false,
    qr: null,
    message: "QR code not yet generated. Please wait and retry.",
  });
});

// ---- POST /disconnect -----------------------------------------------------
app.post("/disconnect", async (_req, res) => {
  try {
    if (client) {
      await client.logout().catch(() => {});
      await client.destroy().catch(() => {});
    }
  } catch (_) {
    /* best-effort */
  }
  connected = false;
  phoneNumber = null;
  latestQR = null;
  client = null;
  setTimeout(() => initializeClient(), 2000);
  res.json({ disconnected: true });
});

// ---- POST /send -----------------------------------------------------------
app.post("/send", async (req, res) => {
  const { phone, message } = req.body || {};
  if (!phone || !message) {
    return res.status(400).json({ error: "phone and message are required" });
  }
  if (!connected || !client) {
    return res.status(503).json({ error: "WhatsApp not connected" });
  }

  const chatId = cleanPhone(phone) + "@c.us";
  try {
    await client.sendMessage(chatId, message);
    incrementDailySend();
    res.json({ sent: true, phone: cleanPhone(phone) });
  } catch (err) {
    console.error("[wa-bridge] Send error:", err.message);
    res.status(500).json({ error: err.message });
  }
});

// ---- POST /bulk-send ------------------------------------------------------
app.post("/bulk-send", (req, res) => {
  const { messages, delayMin, delayMax, action, campaignName } = req.body || {};

  // Handle pause/resume actions
  if (action === 'pause') {
    bulk.paused = true;
    saveCampaignState();
    return res.json({ paused: true });
  }
  if (action === 'resume') {
    bulk.paused = false;
    saveCampaignState();
    return res.json({ resumed: true });
  }

  const minDelay = Math.max(SAFE_MIN_DELAY, delayMin || 60);
  const maxDelay = Math.max(minDelay + 10, delayMax || 120);
  if (!Array.isArray(messages) || messages.length === 0) {
    return res.status(400).json({ error: "messages array is required" });
  }
  if (!connected || !client) {
    return res.status(503).json({ error: "WhatsApp not connected" });
  }
  if (bulk.running) {
    return res.status(409).json({ error: "A bulk send is already in progress" });
  }

  // Create campaign
  const campaignId = "camp_" + Date.now();
  const mappedMessages = messages.map((m) => ({
    phone: cleanPhone(m.phone),
    business: m.business || "",
    message: m.message || "",
    status: "pending",
    error: null,
    sentAt: null,
  }));

  bulk = {
    running: true,
    aborted: false,
    paused: false,
    total: messages.length,
    sent: 0,
    failed: 0,
    current: null,
    messages: mappedMessages,
    nextSendAt: null,
    campaignId,
  };

  // Save campaign to disk
  const campaigns = loadCampaigns();
  campaigns.unshift({
    id: campaignId,
    name: campaignName || "Campaign " + new Date().toLocaleString(),
    status: "running",
    total: messages.length,
    sent: 0,
    failed: 0,
    messages: mappedMessages,
    delayMin: minDelay,
    delayMax: maxDelay,
    createdAt: new Date().toISOString(),
    updatedAt: new Date().toISOString(),
  });
  saveCampaigns(campaigns);

  console.log(`[wa-bridge] Campaign ${campaignId} started — ${messages.length} messages, delay: ${minDelay}-${maxDelay}s`);

  runBulkSend(minDelay, maxDelay);

  res.json({ started: true, total: messages.length, campaignId });
});

// Shared send loop used by both new campaigns and resume
function runBulkSend(minDelay, maxDelay) {
  (async () => {
    for (let i = 0; i < bulk.messages.length; i++) {
      const entry = bulk.messages[i];

      // Skip already sent/failed (important for resumed campaigns)
      if (entry.status === "sent" || entry.status === "failed") continue;

      // Wait while paused
      while (bulk.paused && !bulk.aborted) {
        await new Promise((resolve) => setTimeout(resolve, 500));
      }

      // If paused and then stopped (aborted while paused), mark remaining as pending and save
      if (bulk.aborted) {
        // Leave remaining as "pending" (not "skipped") so they can be resumed
        break;
      }
      if (!connected || !client) {
        entry.status = "failed";
        entry.error = "WhatsApp disconnected";
        bulk.failed++;
        saveCampaignState();
        continue;
      }

      bulk.current = entry.phone;
      entry.status = "sending";

      try {
        const chatId = entry.phone + "@c.us";
        const text = entry.message || `Hello${entry.business ? " " + entry.business : ""}!`;
        await client.sendMessage(chatId, text);
        entry.status = "sent";
        entry.sentAt = new Date().toISOString();
        bulk.sent++;
        incrementDailySend();
        console.log(`[wa-bridge] Sent ${bulk.sent}/${bulk.total} to ${entry.phone} (today: ${getDailySendCount()})`);
      } catch (err) {
        entry.status = "failed";
        entry.error = err.message;
        bulk.failed++;
        console.error(`[wa-bridge] Bulk send failed for ${entry.phone}: ${err.message}`);
      }

      // Save after each message
      saveCampaignState();

      // Delay before next message
      const pendingLeft = bulk.messages.filter(m => m.status === "pending").length;
      if (pendingLeft > 0 && !bulk.aborted) {
        const delaySec = Math.floor(Math.random() * (maxDelay - minDelay + 1)) + minDelay;
        bulk.nextSendAt = Date.now() + delaySec * 1000;
        console.log(`[wa-bridge] Waiting ${delaySec}s before next message...`);
        await new Promise((resolve) => setTimeout(resolve, delaySec * 1000));
        bulk.nextSendAt = null;
      }
    }

    bulk.running = false;
    bulk.current = null;
    bulk.nextSendAt = null;

    // Final save — determine if completed or paused (has pending left)
    saveCampaignState();

    console.log(`[wa-bridge] Campaign ${bulk.campaignId} finished — sent: ${bulk.sent}, failed: ${bulk.failed}`);
  })();
}

// ---- POST /campaign-resume ------------------------------------------------
app.post("/campaign-resume", (req, res) => {
  const { campaignId, delayMin, delayMax } = req.body || {};
  if (!campaignId) {
    return res.status(400).json({ error: "campaignId is required" });
  }
  if (!connected || !client) {
    return res.status(503).json({ error: "WhatsApp not connected" });
  }
  if (bulk.running) {
    return res.status(409).json({ error: "A bulk send is already in progress" });
  }

  const campaigns = loadCampaigns();
  const campaign = campaigns.find(c => c.id === campaignId);
  if (!campaign) {
    return res.status(404).json({ error: "Campaign not found" });
  }

  const pendingMessages = campaign.messages.filter(m => m.status === "pending" || m.status === "sending");
  if (pendingMessages.length === 0) {
    return res.status(400).json({ error: "No pending messages to resume" });
  }

  const minDelay = Math.max(SAFE_MIN_DELAY, delayMin || campaign.delayMin || 60);
  const maxDelay = Math.max(minDelay + 10, delayMax || campaign.delayMax || 120);

  // Reset any "sending" back to "pending"
  campaign.messages.forEach(m => {
    if (m.status === "sending") m.status = "pending";
  });

  const sentCount = campaign.messages.filter(m => m.status === "sent").length;
  const failedCount = campaign.messages.filter(m => m.status === "failed").length;

  bulk = {
    running: true,
    aborted: false,
    paused: false,
    total: campaign.messages.length,
    sent: sentCount,
    failed: failedCount,
    current: null,
    messages: campaign.messages,
    nextSendAt: null,
    campaignId: campaign.id,
  };

  campaign.status = "running";
  campaign.updatedAt = new Date().toISOString();
  saveCampaigns(campaigns);

  console.log(`[wa-bridge] Resuming campaign ${campaignId} — ${pendingMessages.length} remaining, delay: ${minDelay}-${maxDelay}s`);

  runBulkSend(minDelay, maxDelay);

  res.json({ resumed: true, remaining: pendingMessages.length, campaignId });
});

// ---- GET /campaigns -------------------------------------------------------
app.get("/campaigns", (_req, res) => {
  const campaigns = loadCampaigns();
  // Return summary (without full message bodies to keep response small)
  const summary = campaigns.map(c => ({
    id: c.id,
    name: c.name,
    status: c.status,
    total: c.total,
    sent: c.messages ? c.messages.filter(m => m.status === "sent").length : c.sent,
    failed: c.messages ? c.messages.filter(m => m.status === "failed").length : c.failed,
    pending: c.messages ? c.messages.filter(m => m.status === "pending" || m.status === "sending").length : 0,
    delayMin: c.delayMin,
    delayMax: c.delayMax,
    createdAt: c.createdAt,
    updatedAt: c.updatedAt,
  }));
  res.json(summary);
});

// ---- GET /campaigns/:id ---------------------------------------------------
app.get("/campaigns/:id", (req, res) => {
  const campaigns = loadCampaigns();
  const campaign = campaigns.find(c => c.id === req.params.id);
  if (!campaign) {
    return res.status(404).json({ error: "Campaign not found" });
  }
  res.json(campaign);
});

// ---- DELETE /campaigns/:id ------------------------------------------------
app.delete("/campaigns/:id", (req, res) => {
  let campaigns = loadCampaigns();
  const idx = campaigns.findIndex(c => c.id === req.params.id);
  if (idx === -1) {
    return res.status(404).json({ error: "Campaign not found" });
  }
  // Don't allow deleting a running campaign
  if (bulk.running && bulk.campaignId === req.params.id) {
    return res.status(409).json({ error: "Cannot delete a running campaign. Stop it first." });
  }
  campaigns.splice(idx, 1);
  saveCampaigns(campaigns);
  res.json({ deleted: true });
});

// ---- GET /bulk-status -----------------------------------------------------
app.get("/bulk-status", (_req, res) => {
  const next_send_in = bulk.nextSendAt ? Math.max(0, Math.round((bulk.nextSendAt - Date.now()) / 1000)) : undefined;
  res.json({
    total: bulk.total,
    sent: bulk.sent,
    failed: bulk.failed,
    current: bulk.current,
    running: bulk.running,
    paused: bulk.paused,
    next_send_in,
    campaignId: bulk.campaignId,
    messages: bulk.messages,
  });
});

// ---- POST /bulk-stop ------------------------------------------------------
app.post("/bulk-stop", (_req, res) => {
  if (!bulk.running) {
    return res.json({ stopped: false, message: "No bulk send in progress" });
  }
  bulk.aborted = true;
  // State will be saved when the loop detects abort
  res.json({ stopped: true });
});

// ---- GET /daily-count -----------------------------------------------------
app.get("/daily-count", (_req, res) => {
  res.json({ sent_today: getDailySendCount(), date: dailySendDate });
});

// ---- GET /stats -----------------------------------------------------------
app.get("/stats", (_req, res) => {
  const campaigns = loadCampaigns();
  let totalSent = 0;
  let totalFailed = 0;
  let totalPending = 0;
  let completed = 0;
  let paused = 0;
  let running = 0;

  campaigns.forEach(c => {
    const msgs = c.messages || [];
    const s = msgs.filter(m => m.status === "sent").length;
    const f = msgs.filter(m => m.status === "failed").length;
    const p = msgs.filter(m => m.status === "pending" || m.status === "sending").length;
    totalSent += s;
    totalFailed += f;
    totalPending += p;
    if (c.status === "completed") completed++;
    else if (c.status === "paused") paused++;
    else if (c.status === "running") running++;
  });

  res.json({
    total_campaigns: campaigns.length,
    completed_campaigns: completed,
    paused_campaigns: paused,
    running_campaigns: running,
    total_sent: totalSent,
    total_failed: totalFailed,
    total_pending: totalPending,
    sent_today: getDailySendCount(),
  });
});

// ---------------------------------------------------------------------------
// Start
// ---------------------------------------------------------------------------

app.listen(PORT, () => {
  console.log(`[wa-bridge] REST API listening on http://0.0.0.0:${PORT}`);
});

// Graceful shutdown
function shutdown() {
  console.log("[wa-bridge] Shutting down...");
  bulk.aborted = true;
  // Save campaign state so it can be resumed after restart
  saveCampaignState();
  if (client) {
    try {
      client.destroy();
    } catch (_) {}
  }
  process.exit(0);
}

process.on("SIGINT", shutdown);
process.on("SIGTERM", shutdown);
