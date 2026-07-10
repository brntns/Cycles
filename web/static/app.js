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
const screens = {
  login: el("screen-login"),
  status: el("screen-status"),
  review: el("screen-review"),
  history: el("screen-history"),
};

let activeReviewFlow = null;

let toastTimer = null;
function toast(msg) {
  const t = el("toast");
  t.textContent = msg;
  t.hidden = false;
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => { t.hidden = true; }, 3200);
}

function showScreen(name) {
  for (const key of Object.keys(screens)) {
    screens[key].hidden = key !== name;
  }
  document.querySelectorAll(".tab-btn").forEach((btn) => {
    btn.classList.toggle("is-active", btn.dataset.screen === name);
  });
  window.scrollTo(0, 0);
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
    err.body = data;
    throw err;
  }
  return data;
}

// ---------- boot ----------

async function boot() {
  wireStaticHandlers();
  try {
    const status = await api("/status");
    localStorage.setItem("cycles:lastStatus", JSON.stringify(status));
    renderStatus(status, false);
    showScreen("status");
  } catch (err) {
    if (err.status === 401) {
      showScreen("login");
      return;
    }
    // Offline or network failure: fall back to the last known status.
    const cached = localStorage.getItem("cycles:lastStatus");
    if (cached) {
      renderStatus(JSON.parse(cached), true);
      showScreen("status");
    } else {
      showScreen("login");
    }
  }
}

function wireStaticHandlers() {
  el("login-form").addEventListener("submit", onLoginSubmit);
  el("logout-btn").addEventListener("click", onLogout);
  el("review-back").addEventListener("click", () => activeReviewFlow.back());
  el("review-cancel").addEventListener("click", () => {
    if (confirm("Cancel this review? Nothing will be saved.")) {
      showScreen("status");
    }
  });
  document.querySelectorAll(".tab-btn").forEach((btn) => {
    btn.addEventListener("click", () => onTabClick(btn.dataset.screen));
  });
}

async function onTabClick(name) {
  if (name === "history") {
    await loadHistory();
  } else {
    await refreshStatus();
  }
  showScreen(name);
}

async function onLoginSubmit(e) {
  e.preventDefault();
  const password = el("login-password").value;
  const errorEl = el("login-error");
  errorEl.hidden = true;
  try {
    await api("/auth/login", { method: "POST", body: { password } });
    el("login-password").value = "";
    await refreshStatus();
    showScreen("status");
  } catch (err) {
    errorEl.textContent = err.message;
    errorEl.hidden = false;
  }
}

async function onLogout() {
  try { await api("/auth/logout", { method: "POST" }); } catch (_) { /* ignore */ }
  localStorage.removeItem("cycles:lastStatus");
  showScreen("login");
}

async function refreshStatus() {
  const status = await api("/status");
  localStorage.setItem("cycles:lastStatus", JSON.stringify(status));
  renderStatus(status, false);
  return status;
}

// ---------- status view ----------

function renderStatus(status, offline) {
  const root = el("status-content");
  root.innerHTML = "";

  if (offline) {
    const note = document.createElement("p");
    note.className = "offline-note";
    note.textContent = "Offline — showing the last known status.";
    root.appendChild(note);
  }

  if (status.active_cycle) {
    root.appendChild(buildActiveCycleCard(status));
  } else {
    root.appendChild(buildEmptyStateCard());
  }

  if (status.weekly_review_due) {
    root.appendChild(buildReviewBanner(
      "The weekly review is open.",
      "Start Sunday review",
      () => reviewFlow.start(status.active_cycle)
    ));
  }

  if (status.quarterly_review_due) {
    root.appendChild(buildReviewBanner(
      "The quarterly review has unlocked.",
      "Start quarterly review",
      () => quarterlyFlow.start()
    ));
  }

  const streak = document.createElement("p");
  streak.className = "streak";
  streak.textContent = status.review_streak > 0
    ? `${status.review_streak} week review streak`
    : "No review streak yet";
  root.appendChild(streak);
}

function buildActiveCycleCard(status) {
  const c = status.active_cycle;
  const card = document.createElement("div");
  card.className = "card";

  const title = document.createElement("h1");
  title.className = "cycle-title";
  title.textContent = c.title;
  card.appendChild(title);

  if (c.intent) {
    const intent = document.createElement("p");
    intent.className = "cycle-intent";
    intent.textContent = c.intent;
    card.appendChild(intent);
  }

  card.appendChild(buildStateTrack(c.state));

  const stateLabel = document.createElement("div");
  stateLabel.className = "state-label";
  stateLabel.textContent = STATE_LABELS[c.state] || c.state;
  card.appendChild(stateLabel);

  const week = weekOf(c.started_at, c.target_weeks);
  const weekLabel = document.createElement("div");
  weekLabel.className = "week-label";
  weekLabel.textContent = `Week ${week} of ${c.target_weeks}`;
  card.appendChild(weekLabel);

  const kv = document.createElement("div");
  kv.className = "kv";
  kv.appendChild(kvItem("This week's next step", status.this_week_next_step || "Not set yet"));
  kv.appendChild(kvItem("Friday show-slot", status.this_week_friday_show || "Not set yet"));
  card.appendChild(kv);

  return card;
}

function kvItem(label, value) {
  const wrap = document.createElement("div");
  wrap.className = "kv-item";
  const l = document.createElement("div");
  l.className = "kv-label";
  l.textContent = label;
  const v = document.createElement("div");
  v.className = "kv-value";
  v.textContent = value;
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
    if (i < idx) step.classList.add("is-done");
    if (i === idx) step.classList.add("is-current");
    track.appendChild(step);
  });
  return track;
}

function weekOf(startedAt, targetWeeks) {
  const start = new Date(startedAt + "T00:00:00Z");
  const now = new Date();
  const days = Math.floor((now - start) / 86400000);
  const week = Math.max(1, Math.floor(days / 7) + 1);
  return Math.min(week, targetWeeks + 4);
}

function buildEmptyStateCard() {
  const card = document.createElement("div");
  card.className = "card empty-state";

  const h2 = document.createElement("h2");
  h2.textContent = "No active cycle";
  card.appendChild(h2);

  const p = document.createElement("p");
  p.textContent = "Start one when you're ready to build or learn something.";
  card.appendChild(p);

  const form = document.createElement("form");
  form.className = "stack";
  form.style.marginTop = "20px";
  form.style.textAlign = "left";

  form.appendChild(textField("new-cycle-title", "Title", "text", true));
  form.appendChild(textAreaField("new-cycle-intent", "Intent — what and why (optional)"));
  form.appendChild(textField("new-cycle-weeks", "Target weeks (1–16)", "text", false, "8"));
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
    const title = el("new-cycle-title").value.trim();
    const weeksRaw = el("new-cycle-weeks").value.trim();
    const weeks = weeksRaw ? parseInt(weeksRaw, 10) : 8;
    try {
      await api("/cycles", {
        method: "POST",
        body: {
          title,
          intent: el("new-cycle-intent").value.trim(),
          target_weeks: weeks,
          show_plan: el("new-cycle-show-plan").value.trim(),
        },
      });
      toast("Cycle started.");
      await refreshStatus();
    } catch (e2) {
      err.textContent = e2.message;
      err.hidden = false;
    }
  });

  card.appendChild(form);
  return card;
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

async function loadHistory() {
  const root = el("history-content");
  root.innerHTML = "<p class=\"offline-note\">Loading…</p>";
  try {
    const [completed, buried] = await Promise.all([
      api("/cycles?state=completed"),
      api("/cycles?state=buried"),
    ]);
    const all = [...(completed || []), ...(buried || [])].sort(
      (a, b) => new Date(b.ended_at || b.created_at) - new Date(a.ended_at || a.created_at)
    );
    root.innerHTML = "";
    if (all.length === 0) {
      const empty = document.createElement("div");
      empty.className = "empty-state";
      empty.innerHTML = "<h2>Nothing here yet</h2><p>Finished and buried cycles will show up here — nothing evaporates.</p>";
      root.appendChild(empty);
      return;
    }
    for (const c of all) {
      root.appendChild(buildHistoryItem(c));
    }
  } catch (err) {
    root.innerHTML = `<p class="error-text">${escapeHtml(err.message)}</p>`;
  }
}

function buildHistoryItem(c) {
  const item = document.createElement("div");
  item.className = "history-item";

  const h3 = document.createElement("h3");
  h3.textContent = c.title;
  item.appendChild(h3);

  const meta = document.createElement("div");
  meta.className = "history-meta";
  meta.textContent = `${STATE_LABELS[c.state] || c.state} · ${c.started_at}${c.ended_at ? " – " + c.ended_at : ""}`;
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

  return item;
}

function escapeHtml(s) {
  const d = document.createElement("div");
  d.textContent = s;
  return d.innerHTML;
}

// ---------- weekly review flow ----------

const reviewFlow = {
  cycle: null,
  answers: {},
  path: [],

  start(activeCycle) {
    activeReviewFlow = this;
    this.cycle = activeCycle || null;
    this.answers = {};
    this.path = [];
    showScreen("review");
    this.go("how");
  },

  back() {
    if (this.path.length <= 1) {
      showScreen("status");
      return;
    }
    this.path.pop();
    this.render(this.path[this.path.length - 1]);
  },

  go(stepId) {
    this.path.push(stepId);
    this.render(stepId);
  },

  render(stepId) {
    const root = el("review-content");
    root.innerHTML = "";
    el("review-step-label").textContent = "Weekly review";

    if (stepId === "how") {
      root.appendChild(this.questionTextarea(
        "How did the week go?",
        null,
        this.answers.how_did_it_go,
        (val) => {
          this.answers.how_did_it_go = val;
          this.go(this.cycle ? "alive" : "next_step");
        }
      ));
      return;
    }

    if (stepId === "alive") {
      root.appendChild(this.questionOptions(
        "Is the cycle still alive?",
        null,
        [
          { value: "yes", label: "Yes" },
          { value: "dying", label: "Dying" },
          { value: "dead", label: "Dead" },
        ],
        (val) => {
          this.answers.alive = val;
          if (val === "dying" || val === "dead") {
            this.go("brain_dump_bury");
          } else {
            this.go("state");
          }
        }
      ));
      return;
    }

    if (stepId === "brain_dump_bury") {
      root.appendChild(this.questionTextarea(
        "What did you learn, and why are you stopping?",
        "This is saved as the brain-dump if you bury the cycle. Nothing evaporates.",
        this.answers.brainDump,
        (val) => {
          this.answers.brainDump = val;
          this.go("state");
        }
      ));
      return;
    }

    if (stepId === "state") {
      const options = this.stateOptions();
      root.appendChild(this.questionOptions(
        "What state is the cycle in?",
        null,
        options,
        (val) => {
          this.answers.newState = val;
          if (val === this.cycle.state) {
            this.go("next_step");
          } else if (val === "buried" && !this.answers.brainDump) {
            this.go("brain_dump_bury");
          } else if (val === "completed") {
            if (!this.cycle.artifact_url && !this.answers.artifactUrl) {
              this.go("artifact_url");
            } else if (!this.cycle.brain_dump && !this.answers.brainDump) {
              this.go("brain_dump_bury");
            } else {
              this.go("next_step");
            }
          } else {
            this.go("next_step");
          }
        }
      ));
      return;
    }

    if (stepId === "artifact_url") {
      root.appendChild(this.questionInput(
        "Where can it be seen?",
        "A link to the repo, post, or video — this is how the cycle gets shown.",
        this.answers.artifactUrl,
        (val) => {
          this.answers.artifactUrl = val;
          if (!this.cycle.brain_dump && !this.answers.brainDump) {
            this.go("brain_dump_bury");
          } else {
            this.go("next_step");
          }
        }
      ));
      return;
    }

    if (stepId === "next_step") {
      root.appendChild(this.questionTextarea(
        "What is the ONE next step for this week?",
        "Exactly one thing.",
        this.answers.nextStep,
        (val) => {
          this.answers.nextStep = val;
          this.go("friday_show");
        },
        true
      ));
      return;
    }

    if (stepId === "friday_show") {
      root.appendChild(this.questionTextarea(
        "What will Friday's show-slot produce?",
        "\"Nothing this week\" is a valid answer.",
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
        : STATE_LABELS[s];
      opts.push({ value: s, label });
    }
    return opts;
  },

  questionTextarea(title, hint, value, onNext, requireValue) {
    return this.questionShell(title, hint, (wrap) => {
      const textarea = document.createElement("textarea");
      textarea.value = value || "";
      textarea.autofocus = true;
      wrap.appendChild(textarea);
      return {
        getValue: () => textarea.value.trim(),
        valid: () => !requireValue || textarea.value.trim().length > 0,
      };
    }, onNext);
  },

  questionInput(title, hint, value, onNext) {
    return this.questionShell(title, hint, (wrap) => {
      const input = document.createElement("input");
      input.type = "url";
      input.value = value || "";
      input.autofocus = true;
      wrap.appendChild(input);
      return {
        getValue: () => input.value.trim(),
        valid: () => input.value.trim().length > 0,
      };
    }, onNext);
  },

  questionOptions(title, hint, options, onNext) {
    const wrap = document.createElement("div");
    wrap.className = "stack";
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
  },

  questionShell(title, hint, buildInput, onNext) {
    const wrap = document.createElement("div");
    wrap.className = "stack";
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
      await refreshStatus();
      showScreen("status");
    } catch (err) {
      root.innerHTML = `<p class="error-text">${escapeHtml(err.message)}</p>`;
      const retry = document.createElement("button");
      retry.className = "btn btn-block";
      retry.textContent = "Back";
      retry.addEventListener("click", () => this.render(this.path[this.path.length - 1]));
      root.appendChild(retry);
    }
  },
};

// ---------- quarterly review flow ----------
// Reuses the review screen with its own short, linear sequence:
// job question -> cycles-this-quarter question -> one prompt per parked
// question -> submit.

const quarterlyFlow = {
  answers: {},
  questions: [],
  qIndex: 0,
  path: [],

  async start() {
    activeReviewFlow = this;
    showScreen("review");
    el("review-step-label").textContent = "Quarterly review";
    this.answers = { notes: {} };
    this.qIndex = 0;
    this.path = ["job"];
    try {
      this.questions = await api("/questions?status=parked");
    } catch (_) {
      this.questions = [];
    }
    this.render("job");
  },

  back() {
    if (this.path.length <= 1) {
      showScreen("status");
      return;
    }
    this.path.pop();
    this.render(this.path[this.path.length - 1]);
  },

  go(stepId) {
    this.path.push(stepId);
    this.render(stepId);
  },

  render(stepId) {
    const root = el("review-content");
    root.innerHTML = "";

    if (stepId === "job") {
      root.appendChild(reviewFlow.questionTextarea(
        "Is the job still carrying the foundation?",
        null,
        this.answers.job,
        (val) => { this.answers.job = val; this.go("cycles"); }
      ));
      return;
    }

    if (stepId === "cycles") {
      root.appendChild(reviewFlow.questionTextarea(
        "Did the cycles work this quarter?",
        "Think about what got completed or buried in the last 12 weeks.",
        this.answers.cycles,
        (val) => {
          this.answers.cycles = val;
          this.qIndex = 0;
          this.goToNextQuestionOrSubmit();
        }
      ));
      return;
    }

    if (stepId.startsWith("pq:")) {
      const idx = parseInt(stepId.split(":")[1], 10);
      const q = this.questions[idx];
      const wrap = document.createElement("div");
      wrap.className = "stack";
      const h = document.createElement("h2");
      h.className = "question-title";
      h.textContent = `Anything changed on: "${q.question}"?`;
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

      addBtn("Still open — save note & continue", "btn btn-primary btn-block", async () => {
        await this.patchQuestion(q.id, { append_note: textarea.value.trim() || undefined });
        this.qIndex = idx + 1;
        this.goToNextQuestionOrSubmit();
      });
      addBtn("Mark answered", "btn btn-option", async () => {
        await this.patchQuestion(q.id, { status: "answered", append_note: textarea.value.trim() || undefined });
        this.qIndex = idx + 1;
        this.goToNextQuestionOrSubmit();
      });
      addBtn("Drop it", "btn btn-option", async () => {
        await this.patchQuestion(q.id, { status: "dropped", append_note: textarea.value.trim() || undefined });
        this.qIndex = idx + 1;
        this.goToNextQuestionOrSubmit();
      });

      root.appendChild(wrap);
      return;
    }
  },

  async patchQuestion(id, body) {
    try { await api(`/questions/${id}`, { method: "PATCH", body }); } catch (_) { /* best-effort */ }
  },

  goToNextQuestionOrSubmit() {
    if (this.qIndex < this.questions.length) {
      this.go(`pq:${this.qIndex}`);
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
        body: { answers: { job_carrying_foundation: this.answers.job, cycles_this_quarter: this.answers.cycles } },
      });
      toast("Quarterly review saved.");
      await refreshStatus();
      showScreen("status");
    } catch (err) {
      root.innerHTML = `<p class="error-text">${escapeHtml(err.message)}</p>`;
    }
  },
};

if ("serviceWorker" in navigator) {
  window.addEventListener("load", () => {
    navigator.serviceWorker.register("/sw.js").catch(() => { /* ignore */ });
  });
}

boot();
