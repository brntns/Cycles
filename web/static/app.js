"use strict";

const STATE_ORDER = ["building", "understanding", "showing", "completed"];
const STATE_LABELS = {
  building: "Building",
  understanding: "Understanding",
  showing: "Showing",
  completed: "Completed",
  buried: "Buried",
};

const el = (id) => document.getElementById(id);

let authed = false;
let cachedStatus = null;
let cachedNotes = [];
let activeReviewFlow = null;

// ---------- utilities ----------

let toastTimer = null;
function toast(msg) {
  const t = el("toast");
  t.textContent = msg;
  t.hidden = false;
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => { t.hidden = true; }, 3200);
}

async function api(path, opts = {}) {
  const res = await fetch(path, {
    method: opts.method || "GET",
    headers: opts.body ? { "Content-Type": "application/json" } : undefined,
    body: opts.body ? JSON.stringify(opts.body) : undefined,
  });
  let data = null;
  const text = await res.text();
  if (text) {
    try { data = JSON.parse(text); } catch (_) { data = null; }
  }
  if (!res.ok) {
    const err = new Error((data && (data.detail || data.error)) || `Request failed (${res.status})`);
    err.status = res.status;
    throw err;
  }
  return data;
}

function saveStatusCache(status) {
  cachedStatus = status;
  try { localStorage.setItem("cycles:lastStatus", JSON.stringify(status)); } catch (_) { /* ignore */ }
}

function loadStatusCache() {
  try {
    const raw = localStorage.getItem("cycles:lastStatus");
    return raw ? JSON.parse(raw) : null;
  } catch (_) {
    return null;
  }
}

// ---------- router ----------

const SCREENS = ["login", "shell", "review"];

function showScreen(name) {
  for (const s of SCREENS) {
    el(`screen-${s}`).hidden = s !== name;
  }
  window.scrollTo(0, 0);
}

function currentPath() {
  const h = location.hash.replace(/^#/, "");
  return h === "" ? "/" : h;
}

function go(path, replace) {
  if (replace) {
    location.replace("#" + path);
  } else {
    location.hash = path;
  }
}

function setNav(path) {
  document.querySelectorAll("[data-nav]").forEach((a) => {
    a.classList.toggle("active", a.dataset.nav === path);
  });
}

async function route() {
  const path = currentPath();

  if (!authed) {
    if (path !== "/login") { go("/login", true); return; }
    showScreen("login");
    setTimeout(() => el("login-password").focus(), 50);
    return;
  }

  // Leaving the review screen resets any in-progress flow.
  if (path !== "/review" && path !== "/quarterly" && activeReviewFlow) {
    activeReviewFlow = null;
  }

  switch (path) {
    case "/login":
      go("/", true);
      return;
    case "/":
      showScreen("shell");
      setNav("/");
      await renderStatusRoute();
      return;
    case "/history":
      showScreen("shell");
      setNav("/history");
      await renderHistoryRoute();
      return;
    case "/review":
      showScreen("review");
      if (!activeReviewFlow || activeReviewFlow !== reviewFlow) {
        await reviewFlow.begin(cachedStatus ? cachedStatus.active_cycle : null);
      }
      return;
    case "/quarterly":
      showScreen("review");
      if (!activeReviewFlow || activeReviewFlow !== quarterlyFlow) {
        await quarterlyFlow.begin();
      }
      return;
    default:
      go("/", true);
  }
}

// ---------- boot ----------

async function boot() {
  wireStaticHandlers();
  cachedStatus = loadStatusCache();
  try {
    const status = await api("/status");
    saveStatusCache(status);
    authed = true;
  } catch (err) {
    if (err.status === 401) {
      authed = false;
    } else if (cachedStatus) {
      // Network failure but we've been here before: show stale data.
      authed = true;
      cachedStatus._offline = true;
    }
  }
  window.addEventListener("hashchange", route);
  route();
}

function wireStaticHandlers() {
  el("login-form").addEventListener("submit", onLoginSubmit);
  el("logout-btn").addEventListener("click", onLogout);
  el("review-back").addEventListener("click", () => {
    if (activeReviewFlow) activeReviewFlow.back();
  });
  el("review-cancel").addEventListener("click", () => {
    if (confirm("Cancel this review? Nothing will be saved.")) {
      activeReviewFlow = null;
      go("/");
    }
  });
}

async function onLoginSubmit(e) {
  e.preventDefault();
  const password = el("login-password").value;
  const errorEl = el("login-error");
  errorEl.hidden = true;
  try {
    await api("/auth/login", { method: "POST", body: { password } });
    el("login-password").value = "";
    authed = true;
    const status = await api("/status");
    saveStatusCache(status);
    go("/", true);
    route();
  } catch (err) {
    errorEl.textContent = err.message;
    errorEl.hidden = false;
  }
}

async function onLogout() {
  try { await api("/auth/logout", { method: "POST" }); } catch (_) { /* ignore */ }
  authed = false;
  cachedStatus = null;
  try { localStorage.removeItem("cycles:lastStatus"); } catch (_) { /* ignore */ }
  go("/login", true);
  route();
}

// ---------- status view ----------

async function renderStatusRoute() {
  const root = el("shell-content");

  // Paint the cached status immediately, then refresh.
  if (cachedStatus) paintStatus(root, cachedStatus, Boolean(cachedStatus._offline), cachedNotes);

  try {
    const status = await api("/status");
    saveStatusCache(status);
    let notes = [];
    if (status.active_cycle) {
      try { notes = await api(`/cycles/${status.active_cycle.id}/notes`); } catch (_) { notes = []; }
    }
    cachedNotes = notes || [];
    if (currentPath() === "/") paintStatus(root, status, false, cachedNotes);
  } catch (err) {
    if (err.status === 401) {
      authed = false;
      go("/login", true);
      return;
    }
    if (!cachedStatus) {
      root.innerHTML = "";
      const p = document.createElement("p");
      p.className = "offline-note";
      p.textContent = "Can't reach the server right now.";
      root.appendChild(p);
    } else if (currentPath() === "/") {
      paintStatus(root, cachedStatus, true, cachedNotes);
    }
  }
}

function paintStatus(root, status, offline, notes) {
  root.innerHTML = "";

  if (offline) {
    const note = document.createElement("p");
    note.className = "offline-note";
    note.textContent = "Offline — showing the last known status.";
    root.appendChild(note);
  }

  if (status.weekly_review_due) {
    root.appendChild(buildReviewBanner(
      "It's time for the Sunday review.",
      "Start Sunday review",
      () => { go("/review"); }
    ));
  }

  if (status.quarterly_review_due) {
    root.appendChild(buildReviewBanner(
      "The quarterly review has unlocked.",
      "Start quarterly review",
      () => { go("/quarterly"); }
    ));
  }

  if (status.active_cycle) {
    root.appendChild(buildActiveCycleCard(status, notes || []));
  } else {
    root.appendChild(buildEmptyStateCard());
  }

  const streak = document.createElement("p");
  streak.className = "streak";
  streak.textContent = status.review_streak > 0
    ? `${status.review_streak}-week review streak`
    : "No review streak yet";
  root.appendChild(streak);
}

function buildActiveCycleCard(status, notes) {
  const c = status.active_cycle;
  const card = document.createElement("div");
  card.className = "card";

  const head = document.createElement("div");
  head.className = "card-head";
  const title = document.createElement("h1");
  title.className = "cycle-title";
  title.textContent = c.title;
  head.appendChild(title);

  const editBtn = document.createElement("button");
  editBtn.className = "link-btn card-edit";
  editBtn.textContent = "Edit";
  head.appendChild(editBtn);
  card.appendChild(head);

  if (c.intent) {
    const intent = document.createElement("p");
    intent.className = "cycle-intent";
    intent.textContent = c.intent;
    card.appendChild(intent);
  }

  card.appendChild(buildStateTrack(c.state));

  // "Week N of ~M": the tilde marks target_weeks as a living estimate,
  // and the one-tap −/+ makes adjusting it normal usage.
  const captionRow = document.createElement("div");
  captionRow.className = "state-caption-row";

  const caption = document.createElement("p");
  caption.className = "state-caption";
  const week = weekOf(c.started_at, c.target_weeks);
  const strong = document.createElement("strong");
  strong.textContent = STATE_LABELS[c.state] || c.state;
  caption.appendChild(strong);
  caption.appendChild(document.createTextNode(` · Week ${week} of ~${c.target_weeks}`));
  captionRow.appendChild(caption);

  const adjust = document.createElement("div");
  adjust.className = "week-adjust";
  const minus = weekAdjustButton("−", "One week less", c, -1);
  const plus = weekAdjustButton("+", "One week more", c, +1);
  adjust.appendChild(minus);
  adjust.appendChild(plus);
  captionRow.appendChild(adjust);
  card.appendChild(captionRow);

  const kv = document.createElement("div");
  kv.className = "kv";
  kv.appendChild(kvItem("This week's next step", status.this_week_next_step));
  kv.appendChild(kvItem("Friday show-slot", status.this_week_friday_show));
  card.appendChild(kv);

  const editForm = buildEditCycleForm(c, () => {
    editForm.hidden = true;
    kv.hidden = false;
  });
  editForm.hidden = true;
  card.appendChild(editForm);

  editBtn.addEventListener("click", () => {
    const editing = !editForm.hidden;
    editForm.hidden = editing;
    kv.hidden = !editing;
    if (!editing) editForm.querySelector("textarea").focus();
  });

  card.appendChild(buildNotesSection(c, notes));

  return card;
}

function weekAdjustButton(label, aria, cycle, delta) {
  const btn = document.createElement("button");
  btn.className = "week-btn";
  btn.textContent = label;
  btn.setAttribute("aria-label", aria);
  const next = cycle.target_weeks + delta;
  if (next < 1 || next > 16) btn.disabled = true;
  btn.addEventListener("click", async () => {
    btn.disabled = true;
    try {
      await api(`/cycles/${cycle.id}`, { method: "PATCH", body: { target_weeks: next } });
      await renderStatusRoute();
    } catch (err) {
      toast(err.message);
      btn.disabled = false;
    }
  });
  return btn;
}

function buildEditCycleForm(cycle, onDone) {
  const form = document.createElement("form");
  form.className = "stack edit-form";

  const intentField = textAreaField("edit-cycle-intent", "Intent");
  intentField.querySelector("textarea").value = cycle.intent || "";
  form.appendChild(intentField);

  const showField = textField("edit-cycle-show-plan", "How will this be shown?", "text");
  showField.querySelector("input").value = cycle.show_plan || "";
  form.appendChild(showField);

  const weeksField = textField("edit-cycle-weeks", "Target weeks (1–16, an estimate)", "text");
  weeksField.querySelector("input").value = String(cycle.target_weeks);
  form.appendChild(weeksField);

  const row = document.createElement("div");
  row.className = "btn-row";
  const save = document.createElement("button");
  save.type = "submit";
  save.className = "btn btn-primary";
  save.textContent = "Save";
  const cancel = document.createElement("button");
  cancel.type = "button";
  cancel.className = "btn btn-ghost";
  cancel.textContent = "Cancel";
  cancel.addEventListener("click", onDone);
  row.appendChild(save);
  row.appendChild(cancel);
  form.appendChild(row);

  const err = document.createElement("p");
  err.className = "error-text";
  err.hidden = true;
  form.appendChild(err);

  form.addEventListener("submit", async (e) => {
    e.preventDefault();
    err.hidden = true;
    const weeks = parseInt(weeksField.querySelector("input").value.trim(), 10);
    try {
      await api(`/cycles/${cycle.id}`, {
        method: "PATCH",
        body: {
          intent: intentField.querySelector("textarea").value.trim(),
          show_plan: showField.querySelector("input").value.trim(),
          target_weeks: Number.isNaN(weeks) ? cycle.target_weeks : weeks,
        },
      });
      toast("Saved.");
      await renderStatusRoute();
    } catch (e2) {
      err.textContent = e2.message;
      err.hidden = false;
    }
  });

  return form;
}

function buildNotesSection(cycle, notes) {
  const section = document.createElement("div");
  section.className = "notes";

  const head = document.createElement("div");
  head.className = "notes-head";
  const label = document.createElement("div");
  label.className = "kv-label";
  label.textContent = "Notes";
  head.appendChild(label);

  const addBtn = document.createElement("button");
  addBtn.className = "link-btn notes-add";
  addBtn.textContent = "+ Add note";
  head.appendChild(addBtn);
  section.appendChild(head);

  const form = document.createElement("form");
  form.className = "stack add-note-form";
  form.hidden = true;
  const textarea = document.createElement("textarea");
  textarea.placeholder = "What happened? What did you figure out?";
  textarea.rows = 3;
  textarea.style.minHeight = "80px";
  form.appendChild(textarea);
  const row = document.createElement("div");
  row.className = "btn-row";
  const save = document.createElement("button");
  save.type = "submit";
  save.className = "btn btn-primary";
  save.textContent = "Save note";
  const cancel = document.createElement("button");
  cancel.type = "button";
  cancel.className = "btn btn-ghost";
  cancel.textContent = "Cancel";
  row.appendChild(save);
  row.appendChild(cancel);
  form.appendChild(row);
  section.appendChild(form);

  addBtn.addEventListener("click", () => {
    form.hidden = false;
    addBtn.hidden = true;
    textarea.focus();
  });
  cancel.addEventListener("click", () => {
    form.hidden = true;
    addBtn.hidden = false;
  });
  form.addEventListener("submit", async (e) => {
    e.preventDefault();
    const text = textarea.value.trim();
    if (!text) { toast("This needs an answer."); return; }
    save.disabled = true;
    try {
      await api(`/cycles/${cycle.id}/notes`, { method: "POST", body: { text } });
      toast("Note saved.");
      await renderStatusRoute();
    } catch (err) {
      toast(err.message);
      save.disabled = false;
    }
  });

  if (notes.length > 0) {
    section.appendChild(buildNotesList(cycle.id, notes, true));
  }

  return section;
}

function buildNotesList(cycleID, notes, deletable) {
  const list = document.createElement("ul");
  list.className = "note-list";
  for (const n of notes) {
    const item = document.createElement("li");
    item.className = "note-item";

    const text = document.createElement("p");
    text.className = "note-text";
    text.textContent = n.text;
    item.appendChild(text);

    const meta = document.createElement("div");
    meta.className = "note-meta";
    const date = document.createElement("span");
    date.className = "note-date";
    date.textContent = (n.created_at || "").slice(0, 10);
    meta.appendChild(date);

    if (deletable) {
      const del = document.createElement("button");
      del.className = "note-del";
      del.setAttribute("aria-label", "Delete note");
      del.textContent = "×";
      del.addEventListener("click", async () => {
        if (!confirm("Delete this note?")) return;
        try {
          await api(`/cycles/${cycleID}/notes/${n.id}`, { method: "DELETE" });
          await renderStatusRoute();
        } catch (err) {
          toast(err.message);
        }
      });
      meta.appendChild(del);
    }

    item.appendChild(meta);
    list.appendChild(item);
  }
  return list;
}

function kvItem(label, value) {
  const wrap = document.createElement("div");
  wrap.className = "kv-item";
  const l = document.createElement("div");
  l.className = "kv-label";
  l.textContent = label;
  const v = document.createElement("div");
  v.className = "kv-value" + (value ? "" : " is-empty");
  v.textContent = value || "Not set yet";
  wrap.appendChild(l);
  wrap.appendChild(v);
  return wrap;
}

function buildStateTrack(currentState) {
  const track = document.createElement("div");
  track.className = "state-track";
  const idx = STATE_ORDER.indexOf(currentState);
  STATE_ORDER.forEach((s, i) => {
    const step = document.createElement("div");
    step.className = "state-step";
    if (idx >= 0 && i < idx) step.classList.add("is-done");
    if (i === idx) step.classList.add("is-current");
    track.appendChild(step);
  });
  return track;
}

function weekOf(startedAt, targetWeeks) {
  const start = new Date(startedAt + "T00:00:00Z");
  const days = Math.floor((Date.now() - start.getTime()) / 86400000);
  const week = Math.max(1, Math.floor(days / 7) + 1);
  return Math.min(week, targetWeeks + 4);
}

function buildEmptyStateCard() {
  const card = document.createElement("div");
  card.className = "card empty-state";

  const glyph = document.createElement("div");
  glyph.className = "empty-glyph";
  glyph.textContent = "◌";
  card.appendChild(glyph);

  const h2 = document.createElement("h2");
  h2.textContent = "No active cycle";
  card.appendChild(h2);

  const p = document.createElement("p");
  p.textContent = "Start one when you're ready to build or learn something.";
  card.appendChild(p);

  const startBtn = document.createElement("button");
  startBtn.className = "btn btn-primary";
  startBtn.textContent = "Start a new cycle";
  card.appendChild(startBtn);

  const form = buildCreateForm();
  form.hidden = true;
  card.appendChild(form);

  startBtn.addEventListener("click", () => {
    startBtn.hidden = true;
    form.hidden = false;
    form.querySelector("input").focus();
  });

  return card;
}

function buildCreateForm() {
  const form = document.createElement("form");
  form.className = "stack create-form";

  form.appendChild(textField("new-cycle-title", "Title", "text", true));
  form.appendChild(textAreaField("new-cycle-intent", "Intent — what and why (optional)"));
  form.appendChild(textField("new-cycle-weeks", "Target weeks — a first estimate, adjust anytime", "text", false, "1"));
  form.appendChild(textField("new-cycle-show-plan", "How will this be shown? (optional)", "text"));

  const submit = document.createElement("button");
  submit.type = "submit";
  submit.className = "btn btn-primary btn-block";
  submit.textContent = "Start cycle";
  form.appendChild(submit);

  const err = document.createElement("p");
  err.className = "error-text";
  err.hidden = true;
  form.appendChild(err);

  form.addEventListener("submit", async (e) => {
    e.preventDefault();
    err.hidden = true;
    const weeksRaw = el("new-cycle-weeks").value.trim();
    try {
      await api("/cycles", {
        method: "POST",
        body: {
          title: el("new-cycle-title").value.trim(),
          intent: el("new-cycle-intent").value.trim(),
          target_weeks: weeksRaw ? parseInt(weeksRaw, 10) : 1,
          show_plan: el("new-cycle-show-plan").value.trim(),
        },
      });
      toast("Cycle started.");
      await renderStatusRoute();
    } catch (e2) {
      err.textContent = e2.message;
      err.hidden = false;
    }
  });

  return form;
}

function textField(id, label, type, required, placeholder) {
  const wrap = document.createElement("label");
  wrap.className = "field";
  const span = document.createElement("span");
  span.className = "field-label";
  span.textContent = label;
  const input = document.createElement("input");
  input.type = type;
  input.id = id;
  if (required) input.required = true;
  if (placeholder) input.placeholder = placeholder;
  wrap.appendChild(span);
  wrap.appendChild(input);
  return wrap;
}

function textAreaField(id, label) {
  const wrap = document.createElement("label");
  wrap.className = "field";
  const span = document.createElement("span");
  span.className = "field-label";
  span.textContent = label;
  const textarea = document.createElement("textarea");
  textarea.id = id;
  wrap.appendChild(span);
  wrap.appendChild(textarea);
  return wrap;
}

function buildReviewBanner(text, buttonLabel, onClick) {
  const banner = document.createElement("div");
  banner.className = "banner";
  const p = document.createElement("p");
  p.className = "banner-text";
  p.textContent = text;
  const btn = document.createElement("button");
  btn.className = "btn btn-primary btn-block";
  btn.textContent = buttonLabel;
  btn.addEventListener("click", onClick);
  banner.appendChild(p);
  banner.appendChild(btn);
  return banner;
}

// ---------- history view ----------

async function renderHistoryRoute() {
  const root = el("shell-content");
  root.innerHTML = "<p class=\"offline-note\">Loading…</p>";
  try {
    const [completed, buried] = await Promise.all([
      api("/cycles?state=completed"),
      api("/cycles?state=buried"),
    ]);
    const all = [...(completed || []), ...(buried || [])].sort(
      (a, b) => new Date(b.ended_at || b.created_at) - new Date(a.ended_at || a.created_at)
    );
    if (currentPath() !== "/history") return;
    root.innerHTML = "";
    if (all.length === 0) {
      const empty = document.createElement("div");
      empty.className = "card empty-state";
      const h2 = document.createElement("h2");
      h2.textContent = "Nothing here yet";
      const p = document.createElement("p");
      p.textContent = "Finished and buried cycles will show up here — nothing evaporates.";
      empty.appendChild(h2);
      empty.appendChild(p);
      root.appendChild(empty);
      return;
    }
    for (const c of all) {
      root.appendChild(buildHistoryItem(c));
    }
  } catch (err) {
    if (err.status === 401) {
      authed = false;
      go("/login", true);
      return;
    }
    root.innerHTML = "";
    const p = document.createElement("p");
    p.className = "error-text";
    p.textContent = err.message;
    root.appendChild(p);
  }
}

function buildHistoryItem(c) {
  const item = document.createElement("div");
  item.className = "history-item";

  const head = document.createElement("div");
  head.className = "history-head";
  const h3 = document.createElement("h3");
  h3.textContent = c.title;
  head.appendChild(h3);

  const tag = document.createElement("span");
  tag.className = `state-tag ${c.state}`;
  tag.textContent = STATE_LABELS[c.state] || c.state;
  head.appendChild(tag);
  item.appendChild(head);

  const meta = document.createElement("div");
  meta.className = "history-meta";
  meta.textContent = `${c.started_at}${c.ended_at ? " — " + c.ended_at : ""}`;
  item.appendChild(meta);

  if (c.brain_dump) {
    const dump = document.createElement("p");
    dump.className = "brain-dump";
    dump.textContent = c.brain_dump;
    item.appendChild(dump);
  }

  if (c.artifact_url) {
    const a = document.createElement("a");
    a.href = c.artifact_url;
    a.target = "_blank";
    a.rel = "noopener noreferrer";
    a.textContent = c.artifact_url;
    item.appendChild(a);
  }

  // Notes load lazily per cycle so the history list stays one request.
  const notesBtn = document.createElement("button");
  notesBtn.className = "link-btn history-notes-toggle";
  notesBtn.textContent = "Show notes";
  item.appendChild(notesBtn);
  notesBtn.addEventListener("click", async () => {
    notesBtn.disabled = true;
    try {
      const notes = await api(`/cycles/${c.id}/notes`) || [];
      notesBtn.remove();
      const box = document.createElement("div");
      box.className = "history-notes";
      const label = document.createElement("div");
      label.className = "kv-label";
      label.textContent = "Notes";
      box.appendChild(label);
      if (notes.length === 0) {
        const none = document.createElement("p");
        none.className = "note-date";
        none.textContent = "No notes.";
        box.appendChild(none);
      } else {
        box.appendChild(buildNotesList(c.id, notes, false));
      }
      item.appendChild(box);
    } catch (err) {
      toast(err.message);
      notesBtn.disabled = false;
    }
  });

  return item;
}

// ---------- shared question builders ----------

function buildNotesContext(label, notes) {
  const box = document.createElement("div");
  box.className = "notes-context";
  const l = document.createElement("div");
  l.className = "kv-label";
  l.textContent = label;
  box.appendChild(l);
  const list = document.createElement("ul");
  list.className = "note-list";
  for (const n of notes) {
    const item = document.createElement("li");
    item.className = "note-item";
    const text = document.createElement("p");
    text.className = "note-text";
    text.textContent = n.text;
    item.appendChild(text);
    const date = document.createElement("span");
    date.className = "note-date";
    date.textContent = (n.created_at || "").slice(0, 10);
    item.appendChild(date);
    list.appendChild(item);
  }
  box.appendChild(list);
  return box;
}

function qShell(title, hint, buildInput, onNext) {
  const wrap = document.createElement("div");
  wrap.className = "stack";
  wrap.style.gap = "18px";

  const h = document.createElement("h2");
  h.className = "question-title";
  h.textContent = title;
  wrap.appendChild(h);

  if (hint) {
    const p = document.createElement("p");
    p.className = "question-hint";
    p.textContent = hint;
    wrap.appendChild(p);
  }

  const field = buildInput(wrap);

  const btn = document.createElement("button");
  btn.type = "button";
  btn.className = "btn btn-primary btn-block";
  btn.textContent = "Continue";
  btn.addEventListener("click", () => {
    if (!field.valid()) { toast("This needs an answer."); return; }
    onNext(field.getValue());
  });
  wrap.appendChild(btn);

  return wrap;
}

function qTextarea(title, hint, value, onNext, requireValue) {
  return qShell(title, hint, (wrap) => {
    const textarea = document.createElement("textarea");
    textarea.value = value || "";
    wrap.appendChild(textarea);
    // Cmd/Ctrl+Enter advances, matching "one question per screen" pacing.
    textarea.addEventListener("keydown", (e) => {
      if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) {
        wrap.querySelector(".btn-primary").click();
      }
    });
    setTimeout(() => textarea.focus(), 60);
    return {
      getValue: () => textarea.value.trim(),
      valid: () => !requireValue || textarea.value.trim().length > 0,
    };
  }, onNext);
}

function qInput(title, hint, value, onNext) {
  return qShell(title, hint, (wrap) => {
    const input = document.createElement("input");
    input.type = "url";
    input.value = value || "";
    input.placeholder = "https://…";
    wrap.appendChild(input);
    input.addEventListener("keydown", (e) => {
      if (e.key === "Enter") wrap.querySelector(".btn-primary").click();
    });
    setTimeout(() => input.focus(), 60);
    return {
      getValue: () => input.value.trim(),
      valid: () => input.value.trim().length > 0,
    };
  }, onNext);
}

function qOptions(title, hint, options, onNext) {
  const wrap = document.createElement("div");
  wrap.className = "stack";
  wrap.style.gap = "18px";

  const h = document.createElement("h2");
  h.className = "question-title";
  h.textContent = title;
  wrap.appendChild(h);

  if (hint) {
    const p = document.createElement("p");
    p.className = "question-hint";
    p.textContent = hint;
    wrap.appendChild(p);
  }

  const list = document.createElement("div");
  list.className = "option-list";
  for (const opt of options) {
    const btn = document.createElement("button");
    btn.type = "button";
    btn.className = "btn btn-option";
    btn.textContent = opt.label;
    btn.addEventListener("click", () => onNext(opt.value));
    list.appendChild(btn);
  }
  wrap.appendChild(list);
  return wrap;
}

// ---------- weekly review flow ----------

const reviewFlow = {
  cycle: null,
  answers: {},
  path: [],
  notes: [],

  async begin(activeCycle) {
    activeReviewFlow = this;
    this.cycle = activeCycle || null;
    this.answers = {};
    this.path = [];
    this.notes = [];
    el("review-step-label").textContent = "Weekly review";
    if (this.cycle) {
      try { this.notes = await api(`/cycles/${this.cycle.id}/notes`) || []; } catch (_) { this.notes = []; }
    }
    this.goStep("how");
  },

  // Notes added since the last weekly review (all of them if none yet).
  notesSinceLastReview() {
    const since = cachedStatus && cachedStatus.last_review_date;
    if (!since) return this.notes;
    return this.notes.filter((n) => (n.created_at || "").slice(0, 10) >= since);
  },

  back() {
    if (this.path.length <= 1) {
      activeReviewFlow = null;
      go("/");
      return;
    }
    this.path.pop();
    this.render(this.path[this.path.length - 1]);
  },

  goStep(stepId) {
    this.path.push(stepId);
    this.render(stepId);
  },

  render(stepId) {
    const root = el("review-content");
    root.innerHTML = "";
    window.scrollTo(0, 0);

    if (stepId === "how") {
      root.appendChild(qTextarea(
        "How did the week go?",
        null,
        this.answers.how_did_it_go,
        (val) => {
          this.answers.how_did_it_go = val;
          this.goStep(this.cycle ? "alive" : "next_step");
        }
      ));
      return;
    }

    if (stepId === "alive") {
      root.appendChild(qOptions(
        "Is the cycle still alive?",
        null,
        [
          { value: "yes", label: "Yes" },
          { value: "dying", label: "Dying" },
          { value: "dead", label: "Dead" },
        ],
        (val) => {
          this.answers.alive = val;
          this.goStep(val === "dying" || val === "dead" ? "brain_dump_bury" : "state");
        }
      ));
      return;
    }

    if (stepId === "brain_dump_bury") {
      // The cycle's notes are the distributed brain-dump — surface all of
      // them as source material next to the input.
      if (this.notes.length > 0) {
        root.appendChild(buildNotesContext("Your notes from this cycle", this.notes));
      }
      root.appendChild(qTextarea(
        "What did you learn, and why are you stopping?",
        "This is saved as the brain-dump if you bury the cycle. Nothing evaporates.",
        this.answers.brainDump,
        (val) => {
          this.answers.brainDump = val;
          this.goStep("state");
        }
      ));
      return;
    }

    if (stepId === "state") {
      root.appendChild(qOptions(
        "What state is the cycle in?",
        null,
        this.stateOptions(),
        (val) => {
          this.answers.newState = val;
          if (val === this.cycle.state) {
            this.goStep("next_step");
          } else if (val === "buried" && !this.answers.brainDump) {
            this.goStep("brain_dump_bury");
          } else if (val === "completed") {
            if (!this.cycle.artifact_url && !this.answers.artifactUrl) {
              this.goStep("artifact_url");
            } else if (!this.cycle.brain_dump && !this.answers.brainDump) {
              this.goStep("brain_dump_bury");
            } else {
              this.goStep("next_step");
            }
          } else {
            this.goStep("next_step");
          }
        }
      ));
      return;
    }

    if (stepId === "artifact_url") {
      root.appendChild(qInput(
        "Where can it be seen?",
        "A link to the repo, post, or video — this is how the cycle gets shown.",
        this.answers.artifactUrl,
        (val) => {
          this.answers.artifactUrl = val;
          if (!this.cycle.brain_dump && !this.answers.brainDump) {
            this.goStep("brain_dump_bury");
          } else {
            this.goStep("next_step");
          }
        }
      ));
      return;
    }

    if (stepId === "next_step") {
      const recent = this.notesSinceLastReview();
      if (recent.length > 0) {
        root.appendChild(buildNotesContext("Notes since your last review", recent));
      }
      root.appendChild(qTextarea(
        "What is the ONE next step for this week?",
        "Exactly one thing.",
        this.answers.nextStep,
        (val) => {
          this.answers.nextStep = val;
          this.goStep("friday_show");
        },
        true
      ));
      return;
    }

    if (stepId === "friday_show") {
      root.appendChild(qTextarea(
        "What will Friday's show-slot produce?",
        "“Nothing this week” is a valid answer.",
        this.answers.fridayShow,
        (val) => {
          this.answers.fridayShow = val;
          this.submit();
        }
      ));
      return;
    }
  },

  stateOptions() {
    const transitions = {
      building: ["understanding", "buried"],
      understanding: ["showing", "buried"],
      showing: ["completed", "buried"],
    };
    const current = this.cycle.state;
    const opts = [{ value: current, label: `Still ${STATE_LABELS[current].toLowerCase()}` }];
    for (const s of transitions[current] || []) {
      const label = s === "completed" ? "Complete it — show what I made"
        : s === "buried" ? "Bury it"
        : `Move to ${STATE_LABELS[s].toLowerCase()}`;
      opts.push({ value: s, label });
    }
    return opts;
  },

  async submit() {
    const root = el("review-content");
    root.innerHTML = "<p class=\"offline-note\">Saving…</p>";
    try {
      await api("/reviews/weekly", {
        method: "POST",
        body: {
          cycle_id: this.cycle ? this.cycle.id : null,
          answers: {
            how_did_it_go: this.answers.how_did_it_go || "",
            alive: this.answers.alive || null,
          },
          next_step: this.answers.nextStep || "",
          friday_show: this.answers.fridayShow || "",
        },
      });

      if (this.cycle && this.answers.newState && this.answers.newState !== this.cycle.state) {
        const patch = { state: this.answers.newState };
        if (this.answers.artifactUrl) patch.artifact_url = this.answers.artifactUrl;
        if (this.answers.brainDump) patch.brain_dump = this.answers.brainDump;
        await api(`/cycles/${this.cycle.id}`, { method: "PATCH", body: patch });
      }

      toast("Weekly review saved.");
      activeReviewFlow = null;
      go("/");
    } catch (err) {
      root.innerHTML = "";
      const p = document.createElement("p");
      p.className = "error-text";
      p.textContent = err.message;
      root.appendChild(p);
      const retry = document.createElement("button");
      retry.className = "btn btn-block";
      retry.textContent = "Back";
      retry.addEventListener("click", () => this.render(this.path[this.path.length - 1]));
      root.appendChild(retry);
    }
  },
};

// ---------- quarterly review flow ----------

const quarterlyFlow = {
  answers: {},
  questions: [],
  path: [],

  async begin() {
    activeReviewFlow = this;
    el("review-step-label").textContent = "Quarterly review";
    this.answers = {};
    this.path = [];
    try {
      this.questions = await api("/questions?status=parked") || [];
    } catch (_) {
      this.questions = [];
    }
    this.goStep("job");
  },

  back() {
    if (this.path.length <= 1) {
      activeReviewFlow = null;
      go("/");
      return;
    }
    this.path.pop();
    this.render(this.path[this.path.length - 1]);
  },

  goStep(stepId) {
    this.path.push(stepId);
    this.render(stepId);
  },

  render(stepId) {
    const root = el("review-content");
    root.innerHTML = "";
    window.scrollTo(0, 0);

    if (stepId === "job") {
      root.appendChild(qTextarea(
        "Is the job still carrying the foundation?",
        null,
        this.answers.job,
        (val) => { this.answers.job = val; this.goStep("cycles"); }
      ));
      return;
    }

    if (stepId === "cycles") {
      root.appendChild(qTextarea(
        "Did the cycles work this quarter?",
        "Think about what got completed or buried in the last 12 weeks.",
        this.answers.cycles,
        (val) => {
          this.answers.cycles = val;
          this.nextQuestionOrSubmit(0);
        }
      ));
      return;
    }

    if (stepId.startsWith("pq:")) {
      const idx = parseInt(stepId.split(":")[1], 10);
      const q = this.questions[idx];
      const wrap = document.createElement("div");
      wrap.className = "stack";
      wrap.style.gap = "18px";

      const h = document.createElement("h2");
      h.className = "question-title";
      h.textContent = `Anything changed on: “${q.question}”?`;
      wrap.appendChild(h);

      const textarea = document.createElement("textarea");
      textarea.placeholder = "Optional note";
      wrap.appendChild(textarea);

      const list = document.createElement("div");
      list.className = "option-list";
      wrap.appendChild(list);

      const addBtn = (label, cls, handler) => {
        const btn = document.createElement("button");
        btn.type = "button";
        btn.className = cls;
        btn.textContent = label;
        btn.addEventListener("click", handler);
        list.appendChild(btn);
      };

      addBtn("Still open — continue", "btn btn-primary btn-block", async () => {
        await this.patchQuestion(q.id, { append_note: textarea.value.trim() || undefined });
        this.nextQuestionOrSubmit(idx + 1);
      });
      addBtn("Mark answered", "btn btn-option", async () => {
        await this.patchQuestion(q.id, { status: "answered", append_note: textarea.value.trim() || undefined });
        this.nextQuestionOrSubmit(idx + 1);
      });
      addBtn("Drop it", "btn btn-option", async () => {
        await this.patchQuestion(q.id, { status: "dropped", append_note: textarea.value.trim() || undefined });
        this.nextQuestionOrSubmit(idx + 1);
      });

      root.appendChild(wrap);
      return;
    }
  },

  async patchQuestion(id, body) {
    try { await api(`/questions/${id}`, { method: "PATCH", body }); } catch (_) { /* best-effort */ }
  },

  nextQuestionOrSubmit(idx) {
    if (idx < this.questions.length) {
      this.goStep(`pq:${idx}`);
    } else {
      this.submit();
    }
  },

  async submit() {
    const root = el("review-content");
    root.innerHTML = "<p class=\"offline-note\">Saving…</p>";
    try {
      await api("/reviews/quarterly", {
        method: "POST",
        body: {
          answers: {
            job_carrying_foundation: this.answers.job,
            cycles_this_quarter: this.answers.cycles,
          },
        },
      });
      toast("Quarterly review saved.");
      activeReviewFlow = null;
      go("/");
    } catch (err) {
      root.innerHTML = "";
      const p = document.createElement("p");
      p.className = "error-text";
      p.textContent = err.message;
      root.appendChild(p);
    }
  },
};

// ---------- service worker ----------

if ("serviceWorker" in navigator) {
  window.addEventListener("load", () => {
    navigator.serviceWorker.register("/sw.js").catch(() => { /* ignore */ });
  });
}

boot();
