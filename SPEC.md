# SPEC — Cycle Companion (working title)

A planning tool for **process people**: people whose fulfillment comes from the process of building and understanding, not from finished things. It replaces "what do I want?" with "what is my current cycle, and what's the next step?"

This document is the handoff spec for Claude Code. Build exactly this scope; resist adding features.

---

## Core philosophy (do not violate these)

1. **Cycles, not tasks.** The core object is a project cycle (~6–12 weeks), not a todo list. There are no due dates on individual tasks. There is no task management at all in v1.
2. **A cycle is only finished when it has been shown.** Completing = producing a public artifact (repo, writeup, post, video). The state machine enforces this.
3. **Dying is allowed, vanishing is not.** A cycle may be buried before completion — but only with a short brain-dump ("what I learned, why I'm stopping"). Nothing evaporates.
4. **The Sunday review is a guided ritual, not an empty form.** The app asks questions in sequence; the user answers. Low friction, always the same structure (predictability matters — the user is autistic; consistency is a feature, not polish).
5. **Big life questions are parked, not suppressed.** They live in a container that only opens at the quarterly review (every 12 weeks), so they stop running as background noise.
6. **Everyday companion.** Primary usage is from an iPhone and a MacBook throughout the day, plus a Linux desktop. Anything that adds friction to a 30-second check-in is a bug.

## Architecture & deployment (Railway)

```
┌──────────────┐  ┌──────────────┐  ┌───────────────────┐
│ Web UI (PWA)  │  │ Web UI (PWA)  │  │ Quickshell panel   │
│ iPhone        │  │ MacBook/Desktop│ │ (QML — cycle 2,    │
│               │  │               │  │  NOT in v1)        │
└──────┬───────┘  └──────┬───────┘  └─────────┬─────────┘
       │        HTTPS/JSON (REST)             │
       └───────────────┬──────────────────────┘
                 ┌─────▼──────┐   ┌──────────────┐
                 │  Backend    │──▶│  PostgreSQL   │
                 │  (Railway)  │   │  (Railway     │
                 │  serves API │   │   add-on)     │
                 │  + web UI   │   └──────────────┘
                 └────────────┘
```

- **Deployment target: Railway.** One Railway project with two services: the app (built from this repo via Dockerfile) and a Railway PostgreSQL database. Use Railway-provided env vars (`DATABASE_URL`, `PORT`). Claude Code should use the Railway CLI/MCP integration to provision and deploy where available.
- **Database: PostgreSQL** (Railway add-on). Use a lightweight query layer or minimal ORM with migrations that run automatically on startup. No SQLite — app instances must be stateless (no reliance on local disk) so redeploys never lose data.
- **Backend**: small HTTP service, JSON REST API. The same process serves the built web UI as static files (one service to deploy). Reads `PORT` from env (Railway convention), falls back to 4715 locally.
- **Language/stack**: pick a mainstream stack that deploys cleanly on Railway via Dockerfile (Go, Node/TypeScript, or Python/FastAPI — Claude Code's choice, optimized for maintainability and a small image).
- **Portability guard**: everything runs locally with `docker compose up` (app + Postgres) using the same Dockerfile. No Railway-proprietary APIs in application code — Railway specifics live only in config. Migrating away must never require code changes.
- **Web UI**: a **PWA** — installable to the iPhone home screen (manifest + icons + service worker), responsive, **mobile-first**. The most common session is: phone in hand, 30 seconds, "where does my cycle stand?" or the Sunday review from the couch. Desktop layout may simply be a centered column; do not build a dashboard.
- **API-first**: every feature must work via the API alone (curl-able). The web UI is a thin client. A future Quickshell panel and a future voice assistant will consume the same API.

## Auth

- **Single-user auth**: one user, password from env (`CYCLE_PASSWORD`). Login form → long-lived session cookie (90 days, httpOnly, secure). No registration, no user table beyond one row, no OAuth. Structure code so multi-user could be added later, but do not build it.
- Rate-limit login attempts. HTTPS is provided by Railway. That is the entire security scope of v1.

## Data model

### Cycle
| Field | Type | Notes |
|---|---|---|
| id | uuid | |
| title | text | |
| intent | text | one or two sentences: what am I building/learning and why |
| state | enum | see state machine |
| started_at | date | |
| target_weeks | int | default 1, range 1–16 — a **living estimate**, not a commitment; freely editable while the cycle is active |
| show_plan | text | how this will be shown at the end (repo, post, video…) — asked at creation, editable |
| artifact_url | text nullable | filled when shown |
| brain_dump | text nullable | required to enter `buried` or `completed` |
| ended_at | date nullable | |

### State machine (enforced by the backend)
```
building ──► understanding ──► showing ──► completed
    │              │              │
    └──────────────┴──────────────┴──► buried (requires brain_dump)
```
- Only **one cycle may be in a non-terminal state at a time** (single active cycle is a deliberate constraint — reject creation of a second active cycle with a clear error).
- Transition to `completed` requires `artifact_url` AND `brain_dump` to be set.
- Transition to `buried` requires `brain_dump` ("what I learned, why I'm stopping").
- While a cycle is active, `intent`, `show_plan` and `target_weeks` remain editable via PATCH — extending or shortening the estimate is normal usage, not an exception.

### CycleNote (progress notes / Zwischenstände)
| Field | Type | Notes |
|---|---|---|
| id | uuid | |
| cycle_id | uuid | FK to Cycle |
| created_at | timestamp | |
| text | text | prose, not checkboxes — notes are **distributed brain-dumping**, never task items |

When a cycle transitions to `completed` or `buried`, the brain-dump screen shows all of the cycle's notes alongside the input as source material.

### WeeklyReview
| Field | Type | Notes |
|---|---|---|
| id | uuid | |
| date | date | |
| cycle_id | uuid nullable | active cycle at time of review |
| answers | jsonb | keyed answers to the guided questions (below) |
| next_step | text | the single concrete next step for the week |
| friday_show | text | what the Friday show-slot will produce this week (may be "nothing this week") |

### ParkedQuestion
| Field | Type | Notes |
|---|---|---|
| id | uuid | |
| question | text | e.g. "change job?", "leave Germany?" |
| created_at | date | |
| status | enum | `parked`, `answered`, `dropped` |
| notes | jsonb | array of {date, note} — appended at quarterly reviews only |

### QuarterlyReview
| Field | Type | Notes |
|---|---|---|
| id | uuid | |
| date | date | |
| answers | jsonb | guided answers (below) |

## The guided rituals

### Weekly review (Sunday) — question sequence, one at a time
1. "How did the week go?" (free text, short)
2. "Is the cycle still alive?" (yes / dying / dead) — "dying/dead" offers the bury flow (brain-dump prompt)
3. "What state is the cycle in?" (offer valid transitions only)
4. "What is the ONE next step for this week?" (free text — exactly one; the notes added since the last review are shown above the question as context)
5. "What will Friday's show-slot produce?" (free text, "nothing" is a valid answer)
Persist as a WeeklyReview. Show a small "streak" of consecutive weekly reviews (subtle, not gamified — no badges, no confetti).

### Quarterly review — unlocked automatically every 12 weeks (i.e. when the last QuarterlyReview is ≥12 weeks old, the UI and API surface it)
1. "Is the job still carrying the foundation?" (free text)
2. "Did the cycles work this quarter?" (free text; show list of cycles completed/buried in the quarter)
3. Then iterate over each `parked` ParkedQuestion: "Anything changed on: {question}?" — append a note, or mark answered/dropped.

## API (v1)

```
POST   /auth/login                 → session cookie
POST   /auth/logout
GET    /status                     → active cycle (or null), days since last weekly review,
                                     weekly_review_due (bool, true if today ≥ next Sunday since last),
                                     quarterly_review_due (bool)
POST   /cycles                     → create (rejects if an active cycle exists)
GET    /cycles                     → list, filterable by state
GET    /cycles/{id}
PATCH  /cycles/{id}                → edit fields / transition state (validates state machine);
                                     intent, show_plan, target_weeks stay editable on an active cycle
POST   /cycles/{id}/notes          → add a progress note
GET    /cycles/{id}/notes          → list notes, newest first (optional ?since=YYYY-MM-DD)
DELETE /cycles/{id}/notes/{noteId}
POST   /reviews/weekly             → submit a completed weekly review
GET    /reviews/weekly?limit=N
POST   /reviews/quarterly
GET    /reviews/quarterly
GET    /questions                  → parked questions
POST   /questions
PATCH  /questions/{id}             → status change / append note
GET    /export                     → full JSON dump (data freedom from day one)
GET    /health                     → 200 + db connectivity check (for Railway healthcheck)
```

All responses JSON. Errors: `{ "error": "...", "detail": "..." }` with proper status codes. Provide an OpenAPI spec file.

## Web UI (v1)

Three screens only, mobile-first:

1. **Status view** (default): active cycle title, state (as a simple 4-step indicator), "week N of ~M" (the tilde signals the estimate), this week's `next_step` and `friday_show`. One-tap "+1 week" / "-1 week" adjusts the estimate in place — no warnings, no guilt UI. An edit affordance opens `intent`, `show_plan` and `target_weeks` for editing. A one-tap "Add note" opens a single text field (save, done — usable in <10 seconds on a phone); the cycle's notes are listed newest first. If a review is due: a single clear "Start Sunday review" button. Large type, generous spacing, thumb-reachable actions.
2. **Review flow**: the guided questions, one per screen, big text input / big option buttons, forward-only with a back button. The brain-dump screen (bury/complete) shows all of the cycle's notes alongside the input. On finish: return to status view.
3. **History view**: past cycles (completed and buried) with their brain-dumps, artifact links and progress notes — the visible proof that nothing evaporates anymore. Read-only list, newest first.

PWA requirements: web app manifest (name, icons, standalone display), service worker with cache-first for the static shell (offline shows the last-known status; writes require connectivity — no offline sync in v1).

Design: calm and minimal. No dashboard aesthetics, no charts, no stats page. The app should feel like a quiet companion, not a productivity tool shouting metrics.

## Explicitly OUT of scope for v1 (do not build)

- Quickshell/QML panel (cycle 2 — the API-first design is the preparation, not the implementation)
- Any AI/Claude API integration (future: voice brain-dump → doc generation)
- Calendar, day planning, notifications, reminders, push
- Task lists inside cycles
- Multi-user, registration, OAuth, cloud sync beyond the single backend
- Offline write-sync
- Gamification beyond the plain review streak counter

## Definition of done

- `curl` can drive a full lifecycle: login → create cycle → weekly reviews → transitions → completed with artifact_url + brain_dump.
- Attempting to complete without artifact/brain-dump, or to create a second active cycle, fails with a clear error.
- Web UI installs as a PWA on iOS, shows status, runs the weekly review end-to-end on a phone screen.
- Deployed and reachable on Railway (app service + Postgres), with migrations running automatically and `/health` green.
- `docker compose up` runs the identical stack locally.
- README exists (setup, Railway deploy steps, local dev) and the repo is public-ready (the project must follow its own rule: it ships shown).
