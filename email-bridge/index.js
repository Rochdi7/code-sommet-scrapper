const express = require("express");
const cors = require("cors");
const nodemailer = require("nodemailer");
const fs = require("fs");
const path = require("path");
const crypto = require("crypto");

// ---------------------------------------------------------------------------
// Config
// ---------------------------------------------------------------------------
const PORT = process.env.PORT || 3002;
const DATA_DIR = process.env.DATA_DIR || path.join(__dirname, "data");
const CONFIG_FILE = path.join(DATA_DIR, "smtp-config.json");
const CAMPAIGNS_DIR = path.join(DATA_DIR, "campaigns");
const MIN_DELAY_MS = 5000;

// ---------------------------------------------------------------------------
// State
// ---------------------------------------------------------------------------
let smtpConfig = null;
let transporter = null;
let lastSendTime = 0;

// Active campaign state
let activeCampaign = null; // { id, running, aborted, ... }

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function randomDelay(minSec, maxSec) {
  const ms =
    (Math.floor(Math.random() * (maxSec - minSec + 1)) + minSec) * 1000;
  return new Promise((resolve) => setTimeout(resolve, ms));
}

function ensureDataDir() {
  fs.mkdirSync(DATA_DIR, { recursive: true });
  fs.mkdirSync(CAMPAIGNS_DIR, { recursive: true });
}

function saveConfig() {
  ensureDataDir();
  fs.writeFileSync(CONFIG_FILE, JSON.stringify(smtpConfig, null, 2), "utf-8");
}

function loadConfig() {
  try {
    if (fs.existsSync(CONFIG_FILE)) {
      const raw = fs.readFileSync(CONFIG_FILE, "utf-8");
      smtpConfig = JSON.parse(raw);
      createTransporter();
      console.log("[email-bridge] Config loaded");
      return true;
    }
  } catch (err) {
    console.error("[email-bridge] Failed to load config:", err.message);
  }
  return false;
}

function createTransporter() {
  if (!smtpConfig) { transporter = null; return; }
  transporter = nodemailer.createTransport({
    host: smtpConfig.host,
    port: Number(smtpConfig.port) || 587,
    secure: Boolean(smtpConfig.secure),
    auth: { user: smtpConfig.user, pass: smtpConfig.pass },
  });
}

async function enforceRateLimit() {
  const now = Date.now();
  const elapsed = now - lastSendTime;
  if (elapsed < MIN_DELAY_MS) {
    await new Promise((resolve) => setTimeout(resolve, MIN_DELAY_MS - elapsed));
  }
}

async function sendOneEmail({ to, subject, body, html }) {
  if (!transporter || !smtpConfig) throw new Error("SMTP not configured");
  await enforceRateLimit();
  const mailOptions = {
    from: smtpConfig.fromName
      ? `"${smtpConfig.fromName}" <${smtpConfig.fromEmail}>`
      : smtpConfig.fromEmail,
    to, subject, text: body || "",
  };
  if (html) mailOptions.html = html;
  const info = await transporter.sendMail(mailOptions);
  lastSendTime = Date.now();
  return info;
}

// ---------------------------------------------------------------------------
// Campaign Persistence
// ---------------------------------------------------------------------------

function campaignPath(id) {
  return path.join(CAMPAIGNS_DIR, id + ".json");
}

function saveCampaign(campaign) {
  ensureDataDir();
  const data = { ...campaign };
  // Don't persist runtime-only fields
  delete data.running;
  delete data.aborted;
  fs.writeFileSync(campaignPath(campaign.id), JSON.stringify(data, null, 2), "utf-8");
}

function loadCampaign(id) {
  try {
    const raw = fs.readFileSync(campaignPath(id), "utf-8");
    return JSON.parse(raw);
  } catch { return null; }
}

function listCampaigns() {
  ensureDataDir();
  try {
    const files = fs.readdirSync(CAMPAIGNS_DIR).filter(f => f.endsWith(".json"));
    return files.map(f => {
      try {
        const raw = fs.readFileSync(path.join(CAMPAIGNS_DIR, f), "utf-8");
        const c = JSON.parse(raw);
        return {
          id: c.id,
          name: c.name || "",
          createdAt: c.createdAt,
          updatedAt: c.updatedAt,
          total: c.total || 0,
          sent: c.sent || 0,
          failed: c.failed || 0,
          status: c.status || "unknown",
          delayMin: c.delayMin,
          delayMax: c.delayMax,
        };
      } catch { return null; }
    }).filter(Boolean).sort((a, b) => (b.createdAt || "").localeCompare(a.createdAt || ""));
  } catch { return []; }
}

function deleteCampaign(id) {
  try { fs.unlinkSync(campaignPath(id)); return true; } catch { return false; }
}

// Get all emails ever sent across all campaigns (for dedup)
function getSentEmails() {
  ensureDataDir();
  const sentSet = new Set();
  try {
    const files = fs.readdirSync(CAMPAIGNS_DIR).filter(f => f.endsWith(".json"));
    for (const f of files) {
      try {
        const raw = fs.readFileSync(path.join(CAMPAIGNS_DIR, f), "utf-8");
        const c = JSON.parse(raw);
        if (Array.isArray(c.messages)) {
          c.messages.forEach(m => {
            if (m.status === "sent" && m.to) sentSet.add(m.to.toLowerCase());
          });
        }
      } catch {}
    }
  } catch {}
  return Array.from(sentSet);
}

// ---------------------------------------------------------------------------
// Campaign Send Loop
// ---------------------------------------------------------------------------

async function runCampaignLoop(campaign) {
  const minSec = campaign.delayMin || 5;
  const maxSec = campaign.delayMax || 10;

  for (let i = 0; i < campaign.messages.length; i++) {
    if (campaign.aborted) {
      // Mark remaining pending as paused
      for (let j = i; j < campaign.messages.length; j++) {
        if (campaign.messages[j].status === "pending" || campaign.messages[j].status === "sending") {
          campaign.messages[j].status = "paused";
        }
      }
      campaign.status = "paused";
      campaign.updatedAt = new Date().toISOString();
      saveCampaign(campaign);
      break;
    }

    const entry = campaign.messages[i];

    // Skip already-sent or already-failed
    if (entry.status === "sent" || entry.status === "skipped") continue;

    campaign.current = entry.to;
    entry.status = "sending";

    try {
      await sendOneEmail({
        to: entry.to,
        subject: entry.subject || "Message from CodeSommet",
        body: entry.body || "",
        html: entry.html || null,
      });
      entry.status = "sent";
      entry.sentAt = new Date().toISOString();
      campaign.sent++;
    } catch (err) {
      entry.status = "failed";
      entry.error = err.message;
      campaign.failed++;
      console.error(`[email-bridge] Failed for ${entry.to}: ${err.message}`);
    }

    campaign.updatedAt = new Date().toISOString();
    saveCampaign(campaign);

    // Delay between emails (skip after last)
    if (i < campaign.messages.length - 1 && !campaign.aborted) {
      await randomDelay(minSec, maxSec);
    }
  }

  campaign.running = false;
  campaign.current = null;

  if (campaign.status !== "paused") {
    campaign.status = "completed";
  }
  campaign.updatedAt = new Date().toISOString();
  saveCampaign(campaign);
  activeCampaign = null;

  console.log(`[email-bridge] Campaign ${campaign.id} done — sent: ${campaign.sent}, failed: ${campaign.failed}`);
}

// ---------------------------------------------------------------------------
// Express app
// ---------------------------------------------------------------------------
const app = express();
app.use(cors());
app.use(express.json({ limit: "10mb" }));

// ---- GET /status ----------------------------------------------------------
app.get("/status", (_req, res) => {
  res.json({
    configured: !!smtpConfig,
    email: smtpConfig ? smtpConfig.fromEmail : null,
    host: smtpConfig ? smtpConfig.host : null,
  });
});

// ---- GET /config ----------------------------------------------------------
app.get("/config", (_req, res) => {
  if (!smtpConfig) return res.json({ configured: false });
  res.json({
    configured: true,
    host: smtpConfig.host || "",
    port: smtpConfig.port || 587,
    secure: smtpConfig.secure || false,
    user: smtpConfig.user || "",
    fromName: smtpConfig.fromName || "",
    fromEmail: smtpConfig.fromEmail || "",
    hasPassword: !!smtpConfig.pass,
  });
});

// ---- POST /configure ------------------------------------------------------
app.post("/configure", (req, res) => {
  const { host, port, secure, user, pass, fromName, fromEmail, keepPassword } = req.body || {};
  const effectivePass = (!pass && keepPassword && smtpConfig) ? smtpConfig.pass : pass;
  if (!host || !user || !effectivePass || !fromEmail) {
    return res.status(400).json({ error: "host, user, pass, and fromEmail are required" });
  }
  smtpConfig = { host, port: Number(port) || 587, secure: Boolean(secure), user, pass: effectivePass, fromName: fromName || "", fromEmail };
  createTransporter();
  saveConfig();
  res.json({ configured: true, email: fromEmail });
});

// ---- POST /test -----------------------------------------------------------
app.post("/test", async (req, res) => {
  if (!transporter || !smtpConfig) return res.status(400).json({ error: "SMTP not configured" });
  const testTo = req.body.to || smtpConfig.fromEmail;
  try {
    await transporter.verify();
    await sendOneEmail({
      to: testTo,
      subject: "CodeSommet Email Bridge - Test",
      body: "This is a test email.\n\nYour SMTP configuration is working correctly.",
      html: `<div style="font-family:sans-serif;padding:20px"><h2 style="color:#2563eb">CodeSommet Email Bridge</h2><p style="color:#16a34a;font-weight:bold">SMTP working!</p></div>`,
    });
    res.json({ success: true, message: "Test email sent to " + testTo });
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

// ---- POST /send (single) -------------------------------------------------
app.post("/send", async (req, res) => {
  const { to, subject, body, html } = req.body || {};
  if (!to || !subject) return res.status(400).json({ error: "to and subject required" });
  if (!transporter || !smtpConfig) return res.status(503).json({ error: "SMTP not configured" });
  try {
    const info = await sendOneEmail({ to, subject, body, html });
    res.json({ sent: true, messageId: info.messageId, to });
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

// ---- GET /sent-emails -----------------------------------------------------
app.get("/sent-emails", (_req, res) => {
  res.json({ emails: getSentEmails() });
});

// ---- Campaigns ------------------------------------------------------------

// List all campaigns
app.get("/campaigns", (_req, res) => {
  const list = listCampaigns();
  // Mark active campaign
  if (activeCampaign) {
    const idx = list.findIndex(c => c.id === activeCampaign.id);
    if (idx >= 0) {
      list[idx].status = "sending";
      list[idx].sent = activeCampaign.sent;
      list[idx].failed = activeCampaign.failed;
    }
  }
  res.json(list);
});

// Get single campaign details
app.get("/campaigns/:id", (req, res) => {
  // If it's the active campaign, return live state
  if (activeCampaign && activeCampaign.id === req.params.id) {
    return res.json({
      ...activeCampaign,
      status: "sending",
    });
  }
  const c = loadCampaign(req.params.id);
  if (!c) return res.status(404).json({ error: "Campaign not found" });
  res.json(c);
});

// Create + start a new campaign
app.post("/campaigns", (req, res) => {
  const { name, messages, delayMin, delayMax } = req.body || {};
  if (!Array.isArray(messages) || messages.length === 0) {
    return res.status(400).json({ error: "messages array is required" });
  }
  if (!transporter || !smtpConfig) {
    return res.status(503).json({ error: "SMTP not configured" });
  }
  if (activeCampaign && activeCampaign.running) {
    return res.status(409).json({ error: "A campaign is already running" });
  }

  const id = crypto.randomBytes(8).toString("hex");
  const minSec = Math.max(5, Number(delayMin) || 5);
  const maxSec = Math.max(minSec, Number(delayMax) || 10);

  const campaign = {
    id,
    name: name || "Campaign " + new Date().toLocaleDateString(),
    createdAt: new Date().toISOString(),
    updatedAt: new Date().toISOString(),
    status: "sending",
    total: messages.length,
    sent: 0,
    failed: 0,
    current: null,
    delayMin: minSec,
    delayMax: maxSec,
    running: true,
    aborted: false,
    messages: messages.map(m => ({
      to: m.to,
      business: m.business || "",
      subject: m.subject || "",
      body: m.body || "",
      template: m.template || "",
      status: "pending",
      error: null,
      sentAt: null,
    })),
  };

  saveCampaign(campaign);
  activeCampaign = campaign;

  // Fire-and-forget
  runCampaignLoop(campaign);

  res.json({ id, started: true, total: messages.length });
});

// Resume a paused/stopped campaign
app.post("/campaigns/:id/resume", (req, res) => {
  if (!transporter || !smtpConfig) {
    return res.status(503).json({ error: "SMTP not configured" });
  }
  if (activeCampaign && activeCampaign.running) {
    return res.status(409).json({ error: "A campaign is already running" });
  }

  const campaign = loadCampaign(req.params.id);
  if (!campaign) return res.status(404).json({ error: "Campaign not found" });

  // Reset paused/failed messages back to pending for retry
  let resumable = 0;
  campaign.messages.forEach(m => {
    if (m.status === "paused" || m.status === "pending") {
      m.status = "pending";
      resumable++;
    }
    // Optionally retry failed too
    if (req.body && req.body.retryFailed && m.status === "failed") {
      m.status = "pending";
      m.error = null;
      resumable++;
    }
  });

  if (resumable === 0) {
    return res.json({ resumed: false, message: "No messages to resume" });
  }

  // Update delay if provided
  if (req.body && req.body.delayMin) campaign.delayMin = Math.max(5, Number(req.body.delayMin));
  if (req.body && req.body.delayMax) campaign.delayMax = Math.max(campaign.delayMin, Number(req.body.delayMax));

  campaign.status = "sending";
  campaign.running = true;
  campaign.aborted = false;
  campaign.current = null;
  campaign.updatedAt = new Date().toISOString();

  saveCampaign(campaign);
  activeCampaign = campaign;

  runCampaignLoop(campaign);

  res.json({ resumed: true, remaining: resumable });
});

// Stop/pause active campaign
app.post("/campaigns/:id/stop", (req, res) => {
  if (!activeCampaign || activeCampaign.id !== req.params.id) {
    return res.json({ stopped: false, message: "Campaign not active" });
  }
  activeCampaign.aborted = true;
  res.json({ stopped: true });
});

// Update campaign settings (name, delay)
app.patch("/campaigns/:id", (req, res) => {
  if (activeCampaign && activeCampaign.id === req.params.id && activeCampaign.running) {
    return res.status(409).json({ error: "Cannot edit a running campaign. Stop it first." });
  }
  const c = loadCampaign(req.params.id);
  if (!c) return res.status(404).json({ error: "Campaign not found" });

  const { name, delayMin, delayMax } = req.body || {};
  if (name !== undefined) c.name = String(name).trim() || c.name;
  if (delayMin !== undefined) c.delayMin = Math.max(5, Number(delayMin) || 5);
  if (delayMax !== undefined) c.delayMax = Math.max(c.delayMin, Number(delayMax) || 10);
  c.updatedAt = new Date().toISOString();

  saveCampaign(c);
  res.json(c);
});

// Delete a campaign
app.delete("/campaigns/:id", (req, res) => {
  if (activeCampaign && activeCampaign.id === req.params.id && activeCampaign.running) {
    return res.status(409).json({ error: "Cannot delete running campaign. Stop it first." });
  }
  deleteCampaign(req.params.id);
  res.json({ deleted: true });
});

// ---- Legacy bulk endpoints (redirect to campaign system) ------------------
app.get("/bulk-status", (_req, res) => {
  if (!activeCampaign) {
    return res.json({ total: 0, sent: 0, failed: 0, current: null, running: false, messages: [] });
  }
  res.json({
    id: activeCampaign.id,
    total: activeCampaign.total,
    sent: activeCampaign.sent,
    failed: activeCampaign.failed,
    current: activeCampaign.current,
    running: activeCampaign.running,
    status: activeCampaign.status,
    messages: activeCampaign.messages,
  });
});

app.post("/bulk-stop", (_req, res) => {
  if (!activeCampaign || !activeCampaign.running) {
    return res.json({ stopped: false });
  }
  activeCampaign.aborted = true;
  res.json({ stopped: true });
});

// ---------------------------------------------------------------------------
// Start
// ---------------------------------------------------------------------------
app.listen(PORT, () => {
  console.log(`[email-bridge] REST API listening on http://0.0.0.0:${PORT}`);
  loadConfig();
  ensureDataDir();

  // Recover campaigns stuck in "sending" after a restart
  try {
    const files = fs.readdirSync(CAMPAIGNS_DIR).filter(f => f.endsWith(".json"));
    for (const f of files) {
      try {
        const raw = fs.readFileSync(path.join(CAMPAIGNS_DIR, f), "utf-8");
        const c = JSON.parse(raw);
        if (c.status === "sending") {
          c.status = "paused";
          c.messages.forEach(m => {
            if (m.status === "sending" || m.status === "pending") m.status = "paused";
          });
          c.updatedAt = new Date().toISOString();
          fs.writeFileSync(path.join(CAMPAIGNS_DIR, f), JSON.stringify(c, null, 2), "utf-8");
          console.log(`[email-bridge] Recovered stuck campaign: ${c.name || c.id}`);
        }
      } catch {}
    }
  } catch {}
});

function shutdown() {
  console.log("[email-bridge] Shutting down...");
  if (activeCampaign) {
    activeCampaign.aborted = true;
    // Give a moment for the loop to save
    setTimeout(() => process.exit(0), 500);
  } else {
    process.exit(0);
  }
}
process.on("SIGINT", shutdown);
process.on("SIGTERM", shutdown);
