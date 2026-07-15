(function () {
  "use strict";

  const escapeHTML = (s) =>
    String(s == null ? "" : s)
      .replace(/&/g, "&amp;")
      .replace(/</g, "&lt;")
      .replace(/>/g, "&gt;")
      .replace(/"/g, "&quot;")
      .replace(/'/g, "&#39;");

  const fmtTime = (iso) => {
    try {
      return new Date(iso).toLocaleTimeString([], {
        hour: "2-digit",
        minute: "2-digit",
        second: "2-digit",
      });
    } catch {
      return iso || "";
    }
  };

  const fmtDuration = (sec) => {
    sec = Math.round(sec || 0);
    if (sec < 60) return sec + "s";
    const m = Math.floor(sec / 60),
      s = sec % 60;
    if (m < 60) return m + "m " + (s < 10 ? "0" : "") + s + "s";
    const h = Math.floor(m / 60),
      mm = m % 60;
    return h + "h " + (mm < 10 ? "0" : "") + mm + "m";
  };

  const fmtCost = (n) => "$" + (Number(n) || 0).toFixed(4);

  const kindChip = (kind) => {
    const k = (kind || "").toLowerCase();
    const cls = k.includes("translat") ? "chip chip-translation" : "chip";
    return '<span class="' + cls + '">' + escapeHTML(kind || "—") + "</span>";
  };

  const reqIdFromTime = (iso, idx) => {
    const t = new Date(iso || Date.now()).getTime().toString(16).slice(-8);
    return "req_" + t + (idx != null ? idx.toString(16) : "");
  };

  /* ---------- Navigation ---------- */
  function nav(page) {
    document
      .querySelectorAll(".page")
      .forEach((el) => el.classList.remove("active"));
    document
      .querySelectorAll(".nav-item")
      .forEach((el) => el.classList.remove("active"));
    document.getElementById("page-" + page).classList.add("active");
    document
      .querySelector('.nav-item[data-page="' + page + '"]')
      .classList.add("active");
    if (page === "usage") loadUsage();
    if (page === "history") loadHistoryDates();
    if (page === "logs") loadLogFiles();
    if (page === "help") loadHelp();
  }

  /* ---------- Usage ---------- */
  let usageData = [];
  let allTimeData = [];

  async function loadUsage() {
    try {
      const [todayRes, allRes] = await Promise.all([
        fetch("/api/usage/today"),
        fetch("/api/usage/alltime"),
      ]);
      usageData = (await todayRes.json()) || [];
      allTimeData = (await allRes.json()) || [];
    } catch (e) {
      usageData = [];
      allTimeData = [];
    }
    renderUsage();
  }

  function renderUsage() {
    const tbody = document.getElementById("usageContainer");
    const search = (
      document.getElementById("usageSearch").value || ""
    ).toLowerCase();

    let tCost = 0,
      tSec = 0;
    const rows = [...usageData].reverse();
    const filtered = rows.filter((r) => {
      if (!search) return true;
      return (
        (r.kind || "").toLowerCase().includes(search) ||
        (r.model || "").toLowerCase().includes(search)
      );
    });

    rows.forEach((r) => {
      tCost += r.cost_usd || 0;
      tSec += r.duration_seconds || 0;
    });

    if (filtered.length === 0) {
      tbody.innerHTML =
        '<tr><td colspan="5"><div class="empty-state">No usage records ' +
        (search ? "matching your search" : "today yet") +
        ".</div></td></tr>";
    } else {
      tbody.innerHTML = filtered
        .map((r, i) => {
          const failed = (r.status || "").toLowerCase() === "failed" || r.error;
          return (
            '<tr class="' +
            (failed ? "row-failed" : "") +
            '">' +
            "<td><div>" +
            escapeHTML(fmtTime(r.time)) +
            '</div><div class="req-id">' +
            escapeHTML(reqIdFromTime(r.time, i)) +
            "</div></td>" +
            "<td>" +
            kindChip(r.kind) +
            "</td>" +
            '<td><span class="mono">' +
            escapeHTML(r.model || "—") +
            "</span></td>" +
            "<td>" +
            (failed ? "—" : escapeHTML(fmtDuration(r.duration_seconds))) +
            "</td>" +
            '<td class="text-right">' +
            (failed
              ? '<span class="chip chip-failed">Failed</span>'
              : escapeHTML(fmtCost(r.cost_usd))) +
            "</td>" +
            "</tr>"
          );
        })
        .join("");
    }

    document.getElementById("totalCost").innerText = tCost.toFixed(2);
    document.getElementById("totalRecords").innerText =
      rows.length.toLocaleString();
    document.getElementById("totalTime").innerText = fmtDuration(tSec);
    document.getElementById("usageFooter").innerText =
      "Showing " + filtered.length + " of " + rows.length + " entries";

    // Lifetime stats
    let ltCost = 0,
      ltSec = 0;
    allTimeData.forEach((r) => {
      ltCost += r.cost_usd || 0;
      ltSec += r.duration_seconds || 0;
    });
    document.getElementById("lifetimeCost").innerText = ltCost.toFixed(2);
    document.getElementById("lifetimeRecords").innerText =
      allTimeData.length.toLocaleString();
    document.getElementById("lifetimeTime").innerText = fmtDuration(ltSec);
  }

  /* ---------- Help ---------- */
  function loadHelp() {
    // Help page is static HTML; nothing to fetch.
  }

  /* ---------- History ---------- */
  let historyRecords = [];

  async function loadHistoryDates() {
    const sel = document.getElementById("historySelector");
    try {
      const res = await fetch("/api/history/dates");
      const dates = (await res.json()) || [];
      if (!dates.length) {
        sel.innerHTML = '<option value="">No history found</option>';
        document.getElementById("historyTableBody").innerHTML =
          '<tr><td colspan="4"><div class="empty-state">No history records found.</div></td></tr>';
        return;
      }
      sel.innerHTML = dates
        .map(
          (d) =>
            '<option value="' +
            escapeHTML(d) +
            '">' +
            escapeHTML(d) +
            "</option>",
        )
        .join("");
      loadHistoryTable(dates[0]);
    } catch (e) {
      sel.innerHTML = '<option value="">Error loading</option>';
    }
  }

  async function loadHistoryTable(date) {
    if (!date) return;
    const tbody = document.getElementById("historyTableBody");
    tbody.innerHTML =
      '<tr><td colspan="4"><div class="empty-state">Loading...</div></td></tr>';
    try {
      const res = await fetch(
        "/api/history/by-date?date=" + encodeURIComponent(date),
      );
      historyRecords = (await res.json()) || [];
    } catch (e) {
      historyRecords = [];
    }
    renderHistory();
  }

  function renderHistory() {
    const tbody = document.getElementById("historyTableBody");
    const search = (
      document.getElementById("historySearch").value || ""
    ).toLowerCase();
    const filtered = historyRecords.filter(
      (r) =>
        !search ||
        (r.text || "").toLowerCase().includes(search) ||
        (r.kind || "").toLowerCase().includes(search),
    );

    if (filtered.length === 0) {
      tbody.innerHTML =
        '<tr><td colspan="4"><div class="empty-state">No records ' +
        (search ? "match your search" : "for this day") +
        ".</div></td></tr>";
      return;
    }

    tbody.innerHTML = filtered
      .map((r, i) => {
        const enc = encodeURIComponent(r.text || "");
        return (
          "<tr>" +
          "<td>" +
          escapeHTML(fmtTime(r.time)) +
          "</td>" +
          "<td>" +
          kindChip(r.kind) +
          "</td>" +
          '<td style="white-space: pre-wrap;">' +
          escapeHTML(r.text) +
          "</td>" +
          '<td class="text-right"><button type="button" class="btn-mini" data-copy="' +
          enc +
          '">Copy</button></td>' +
          "</tr>"
        );
      })
      .join("");
  }

  /* ---------- Logs ---------- */
  async function loadLogFiles() {
    const sel = document.getElementById("logsSelector");
    const content = document.getElementById("logsContent");
    try {
      const res = await fetch("/api/logs");
      let files = (await res.json()) || [];
      files = files.filter((f) => !f.startsWith("."));
      if (!files.length) {
        sel.innerHTML = '<option value="">No files</option>';
        content.innerText = "No log files found.";
        return;
      }
      const daily = files.filter((f) => f !== "crash.log").reverse();
      const opt = (f, label) =>
        '<option value="/logs/' +
        encodeURIComponent(f) +
        '">' +
        escapeHTML(label || f) +
        "</option>";
      let html = '<optgroup label="App logs">' + daily.map((f) => opt(f)).join("") + "</optgroup>";
      if (files.includes("crash.log")) {
        html +=
          '<optgroup label="Supervisor">' +
          opt("crash.log", "Crash log") +
          "</optgroup>";
      }
      sel.innerHTML = html;
      viewLogFile(sel.value);
    } catch (e) {
      sel.innerHTML = '<option value="">Error</option>';
      content.innerText = "Error loading log files.";
    }
  }

  async function viewLogFile(url) {
    if (!url) return;
    const content = document.getElementById("logsContent");
    content.innerText = "Loading...";
    try {
      const res = await fetch(url);
      const text = await res.text();
      content.innerText = text;
      content.scrollTop = content.scrollHeight;
    } catch (e) {
      content.innerText = "Error loading file.";
    }
  }

  /* ---------- Settings ---------- */
  const KEY_LABELS = {
    STT_PROVIDER: "Speech-to-Text Provider",
    STT_MODEL: "Default Model",
    STT_LANGUAGE: "Default Audio Language",
    STT_MODE: "Transcription Mode",
    DISABLE_LLM: "LLM Post-processing",
    OPENAI_API_KEY: "OpenAI API Key",
    DEEPGRAM: "Deepgram API Key",
    STT_PROMPT: "Custom STT Prompt",
    PROMPT: "LLM Processing Prompt",
    HOTKEY_MODE: "Hotkey Mode",
    HOTKEY_START: "Start Hotkey",
    HOTKEY_STOP: "Stop Hotkey",
    HOTKEY_HISTORY: "History Panel Hotkey",
    WAVE_THEME: "Wave Color Theme",
    PASTE_MODE: "Paste Mode",
    RESTORE_CLIPBOARD: "Restore Clipboard After Paste",
    SMART_SPACING: "Smart Leading Space",
    MAX_RECORD_SECONDS: "Max Recording (seconds)",
    SAVE_RECORDINGS: "Save WAV Backups (7 days)",
    COST_STT_AUDIO_INPUT_USD_PER_1M: "STT Audio Input ($/1M)",
    COST_STT_AUDIO_USD_PER_MINUTE: "STT Audio ($/minute)",
    COST_STT_TEXT_INPUT_USD_PER_1M: "STT Text Input ($/1M)",
    COST_STT_OUTPUT_USD_PER_1M: "STT Output ($/1M)",
    COST_LLM_INPUT_USD_PER_1M: "LLM Input ($/1M)",
    COST_LLM_OUTPUT_USD_PER_1M: "LLM Output ($/1M)",
  };

  const SELECT_OPTIONS = {
    STT_PROVIDER: ["openai", "deepgram"],
    STT_MODE: ["batch", "realtime"],
    STT_LANGUAGE: ["auto", "ru", "en"],
    HOTKEY_MODE: ["hold", "toggle"],
    WAVE_THEME: ["green", "purple", "yellow", "red", "blue"],
    PASTE_MODE: ["clipboard", "type"],
    STT_MODEL: [
      "gpt-4o-mini-transcribe",
      "gpt-4o-transcribe",
      "gpt-4o-transcribe-diarize",
      "nova-2",
    ],
  };

  const TOGGLE_KEYS = {
    DISABLE_LLM: { false: "Enabled", true: "Disabled" },
    RESTORE_CLIPBOARD: { true: "Enabled", false: "Disabled" },
    SMART_SPACING: { true: "Enabled", false: "Disabled" },
    SAVE_RECORDINGS: { true: "Enabled", false: "Disabled" },
  };
  const TEXTAREA_KEYS = new Set(["STT_PROMPT", "PROMPT"]);

  const TABS = [
    {
      id: "general",
      label: "General",
      card: "General Profile",
      sub: "Core defaults for transcription.",
      keys: [
        "STT_PROVIDER",
        "STT_MODEL",
        "STT_LANGUAGE",
        "STT_MODE",
        "DISABLE_LLM",
      ],
    },
    {
      id: "apikeys",
      label: "API Keys",
      card: "API Keys",
      sub: "Credentials for the configured providers.",
      keys: ["OPENAI_API_KEY", "DEEPGRAM"],
    },
    {
      id: "prompts",
      label: "Prompts",
      card: "Prompts",
      sub: "Customize STT and post-processing prompts.",
      keys: ["STT_PROMPT", "PROMPT"],
    },
    {
      id: "hotkeys",
      label: "Hotkeys",
      card: "Hotkeys",
      sub: "Bind global keyboard shortcuts.",
      keys: ["HOTKEY_MODE", "HOTKEY_START", "HOTKEY_STOP", "HOTKEY_HISTORY"],
    },
    {
      id: "widget",
      label: "Widget",
      card: "Widget & Paste",
      sub: "Overlay appearance and text insertion behavior.",
      keys: [
        "WAVE_THEME",
        "PASTE_MODE",
        "RESTORE_CLIPBOARD",
        "SMART_SPACING",
        "MAX_RECORD_SECONDS",
        "SAVE_RECORDINGS",
      ],
    },
    {
      id: "pricing",
      label: "Pricing Plan",
      card: "Pricing Structure",
      sub: "Per-token and per-minute cost configuration.",
      keys: [
        "COST_STT_AUDIO_INPUT_USD_PER_1M",
        "COST_STT_AUDIO_USD_PER_MINUTE",
        "COST_STT_TEXT_INPUT_USD_PER_1M",
        "COST_STT_OUTPUT_USD_PER_1M",
        "COST_LLM_INPUT_USD_PER_1M",
        "COST_LLM_OUTPUT_USD_PER_1M",
      ],
    },
  ];

  let settingsData = {};
  let activeTab = "general";

  async function loadSettings() {
    try {
      const res = await fetch("/api/settings");
      settingsData = (await res.json()) || {};
    } catch (e) {
      settingsData = {};
    }
    TABS.forEach((t) =>
      t.keys.forEach((k) => {
        if (settingsData[k] == null) settingsData[k] = "";
      }),
    );
    renderSettingsTabs();
    renderSettingsBody();
  }

  function renderSettingsTabs() {
    const known = new Set();
    TABS.forEach((t) => t.keys.forEach((k) => known.add(k)));
    const otherKeys = Object.keys(settingsData).filter((k) => !known.has(k));
    const allTabs = otherKeys.length
      ? TABS.concat([
          {
            id: "other",
            label: "Other",
            card: "Other Settings",
            sub: "Additional keys discovered in your config.",
            keys: otherKeys,
          },
        ])
      : TABS;

    const tabsEl = document.getElementById("settingsTabs");
    tabsEl.innerHTML = allTabs
      .map(
        (t) =>
          '<button type="button" class="settings-tab' +
          (t.id === activeTab ? " active" : "") +
          '" data-tab="' +
          t.id +
          '">' +
          escapeHTML(t.label) +
          "</button>",
      )
      .join("");
    tabsEl._tabs = allTabs;
  }

  function renderSettingsBody() {
    const tabs = document.getElementById("settingsTabs")._tabs || TABS;
    const tab = tabs.find((t) => t.id === activeTab) || tabs[0];
    if (!tab) return;
    const body = document.getElementById("settingsBody");

    let inner =
      '<div class="settings-card"><h3 class="settings-card-title">' +
      escapeHTML(tab.card) +
      "</h3>";
    if (tab.sub)
      inner += '<p class="settings-card-sub">' + escapeHTML(tab.sub) + "</p>";
    inner +=
      '<div class="field-grid">' +
      tab.keys.map((k) => renderField(k, tab.id)).join("") +
      "</div>";
    inner += "</div>";
    body.innerHTML = inner;
  }

  function renderField(key, tabId) {
    const value = settingsData[key] != null ? String(settingsData[key]) : "";
    const label = KEY_LABELS[key] || key;
    const labelHTML =
      '<label class="field-label">' +
      escapeHTML(label) +
      '<span class="field-key">' +
      escapeHTML(key) +
      "</span></label>";

    let inputHTML;
    if (TOGGLE_KEYS[key]) {
      const map = TOGGLE_KEYS[key];
      const cur = value === "true" ? "true" : "false";
      inputHTML =
        '<input type="hidden" name="' +
        key +
        '" value="' +
        escapeHTML(cur) +
        '">' +
        '<div class="toggle-group" data-toggle="' +
        key +
        '">' +
        ["false", "true"]
          .map(
            (v) =>
              '<button type="button" class="toggle-btn' +
              (cur === v ? " active" : "") +
              '" data-value="' +
              v +
              '">' +
              escapeHTML(map[v]) +
              "</button>",
          )
          .join("") +
        "</div>";
    } else if (SELECT_OPTIONS[key]) {
      inputHTML =
        '<select class="field-select" name="' +
        key +
        '">' +
        SELECT_OPTIONS[key]
          .map(
            (o) =>
              '<option value="' +
              escapeHTML(o) +
              '"' +
              (value === o ? " selected" : "") +
              ">" +
              escapeHTML(o) +
              "</option>",
          )
          .join("") +
        "</select>";
    } else if (TEXTAREA_KEYS.has(key)) {
      inputHTML =
        '<textarea class="field-textarea" name="' +
        key +
        '">' +
        escapeHTML(value) +
        "</textarea>";
    } else {
      inputHTML =
        '<input class="field-input" type="text" name="' +
        key +
        '" value="' +
        escapeHTML(value) +
        '" autocomplete="off">';
    }

    const isFull = TEXTAREA_KEYS.has(key) || tabId === "apikeys";
    return (
      '<div class="field' +
      (isFull ? " full" : "") +
      '">' +
      labelHTML +
      inputHTML +
      "</div>"
    );
  }

  /* ---------- Event wiring ---------- */
  function bindEvents() {
    document.querySelectorAll(".nav-item").forEach((btn) => {
      btn.addEventListener("click", () => nav(btn.dataset.page));
    });

    document
      .getElementById("usageSearch")
      .addEventListener("input", renderUsage);
    document
      .getElementById("historySearch")
      .addEventListener("input", renderHistory);
    document
      .getElementById("historySelector")
      .addEventListener("change", (e) => loadHistoryTable(e.target.value));
    document
      .getElementById("logsSelector")
      .addEventListener("change", (e) => viewLogFile(e.target.value));

    document.addEventListener("click", (e) => {
      const tab = e.target.closest(".settings-tab");
      if (tab) {
        activeTab = tab.dataset.tab;
        renderSettingsTabs();
        renderSettingsBody();
        return;
      }

      const toggleBtn = e.target.closest(".toggle-btn");
      if (toggleBtn) {
        const group = toggleBtn.closest(".toggle-group");
        const key = group.dataset.toggle;
        const value = toggleBtn.dataset.value;
        group
          .querySelectorAll(".toggle-btn")
          .forEach((b) => b.classList.toggle("active", b === toggleBtn));
        const hidden = group.parentElement.querySelector(
          'input[name="' + key + '"]',
        );
        if (hidden) hidden.value = value;
        return;
      }

      const copyBtn = e.target.closest("[data-copy]");
      if (copyBtn) {
        const text = decodeURIComponent(copyBtn.dataset.copy);
        navigator.clipboard.writeText(text).then(() => {
          const old = copyBtn.innerText;
          copyBtn.innerText = "Copied";
          setTimeout(() => {
            copyBtn.innerText = old;
          }, 1500);
        });
        return;
      }

      if (e.target.id === "cancelBtn") {
        renderSettingsBody();
      }
    });

    document
      .getElementById("settingsForm")
      .addEventListener("submit", async (e) => {
        e.preventDefault();
        const updates = {};
        new FormData(e.target).forEach((v, k) => {
          updates[k] = v;
        });
        try {
          const res = await fetch("/api/settings", {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify(updates),
          });
          if (res.ok) {
            Object.assign(settingsData, updates);
            const msg = document.getElementById("saveSuccess");
            msg.classList.add("show");
            setTimeout(() => msg.classList.remove("show"), 2500);
          }
        } catch (err) {
          /* noop */
        }
      });

    const pill = document.getElementById("todayPill");
    if (pill)
      pill.innerText = new Date().toLocaleDateString([], {
        weekday: "short",
        month: "short",
        day: "numeric",
      });
  }

  /* ---------- Bootstrap ---------- */
  function init() {
    bindEvents();
    loadUsage();
    loadSettings();
  }

  document.addEventListener("DOMContentLoaded", init);
})();
