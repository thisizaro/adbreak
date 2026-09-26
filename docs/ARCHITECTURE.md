# Architecture

Every box and arrow here traces to a SCOPE.md item or a LOG.jsonl entry (cited as `LOG hh:mm`).
Rule: before changing code that touches architecture, re-read this file and verify it still matches
the code. If they disagree, stop and ask; never silently update one to match the other.
Items marked **(planned)** are decided but not built yet.

## 1. Modules (one Go binary)

Modular monolith, one process, one Cloud Run instance, in-process event bus (LOG 13:35).
Each box is a package under `internal/` with a small interface; `cmd/server` wires them.

```mermaid
flowchart LR
  subgraph bin["adbreak binary (cmd/server) + dev tool (cmd/analyze)"]
    server["server<br/>HTTP API /api, /media, embedded SPA"]
    config["config<br/>env, printed at startup"]
    library["library<br/>read side: results, video paths, slates on demand"]
    pipeline["pipeline<br/>stage order + per-stage JSON cache"]
    jobs["jobs<br/>async runner + in-process event bus<br/>(one worker = one job at a time)"]
    store["store<br/>Postgres repo (planned)"]
    blob["blob<br/>local disk | GCS (planned; local disk today)"]
    media["media<br/>ffprobe, shots, audio chunks, frames, clips"]
    speech["speech<br/>ASR provider: groq-whisper"]
    app["app<br/>builds per-job deps from config"]
    ai["ai<br/>Gemini via Vertex AI (ADC) or AI Studio key<br/>per-job token budget"]
    scenes["scenes<br/>histograms, proposals, scene build"]
    breaks["breaks<br/>hard filters, scene-context score, pacing DP"]
    brands["brands<br/>catalogue, hard negative block"]
    slates["slates<br/>ffmpeg ad slates"]
    manifest["manifest<br/>VMAP 1.0 + inline VAST 3.0"]
  end
  web["web/ React SPA<br/>embedded via go:embed"] --> server
  server --> library
  server --> manifest
  library --> slates
  server --> jobs
  jobs --> app
  app --> pipeline
  pipeline --> media & speech & scenes & breaks & brands
  pipeline --> ai
  jobs -.-> store
  library -.-> blob
```

Changes since first draft (LOG 14:24 and later, needs review): `pipeline` owns stage order and
caching so both the dev CLI and the future job runner call the same code; `library` is the read
side the HTTP layer uses; `ai` is one shared provider package.

Seams that would become services later, and why not yet: `speech` and `ai` are already
behind provider interfaces (self-hosted whisper.cpp / vLLM would slot in there, LOG 13:35);
`jobs` would split into an API + worker pool with a real queue once more than one instance
is needed. Today one instance handles the load, so a broker adds cost with no benefit.

## 2. Pipeline (the funnel)

Cheap deterministic stages first, AI only on survivors, code has the final say (LOG 13:35, 14:10).
Every stage records its input and output counts in the debug JSON.

```mermaid
flowchart TD
  V[video] --> P[probe<br/>ffprobe]
  P --> SH[shot detection<br/>ffmpeg scene score 0.15]
  P --> AU[audio 16k mono<br/>10 min chunks, 5s overlap]
  AU --> ASR[Whisper large-v3 on Groq<br/>segments + words, merged at overlap midpoints]
  SH --> KF[keyframe per shot<br/>colour histogram]
  KF --> SC[scene proposals<br/>histogram jump + no speech at cut]
  ASR --> SC
  SC --> SD[AI in windows of 80 shots:<br/>scene starts, description, activity, contexts]
  SH --> C[candidates = shot cuts]
  C --> G1{hard filters<br/>head/tail margin<br/>not inside Whisper segment or word + 500ms}
  G1 -- reject --> R1[rejected with reason]
  G1 -- pass --> G2{AI audio check +-5s, batched:<br/>anyone mid-utterance?}
  G2 -- yes or unsure --> R1
  G2 -- no --> S[AI natural-break score]
  SD --> SCX[scene context: +0.1 within -15s/+20s<br/>of a scene start, x0.6 mid-scene]
  S --> SCX
  SCX --> DP[pacing DP<br/>max total score s.t. min gap max 240s or 0.2 x duration,<br/>budget from per-hour rate and ad load, min score]
  DP --> B[brand match per selected break]
  B --> M[VMAP + VAST + debug JSON]
```

Each break also records how it relates to the nearest scene start (shift in seconds and why).

Change (LOG, needs review): candidates are shot cuts shaped by scene context, not exact scene
boundaries only, because narration runs across transitions and exact boundaries all fail the speech guard.

## 3. Brand matching: model proposes, code guarantees

Negative contexts are a hard block per brand, never a soft penalty; uncertainty blocks;
both scenes around the break are checked; brands come from the catalogue at runtime,
so a 9th brand needs zero code change (LOG 13:35, SCOPE demo step 8).

```mermaid
flowchart TD
  BR[selected break] --> CTX[scene before + scene after]
  CAT[brands.json + runtime brands] --> LOOP[for each brand]
  CTX --> LOOP
  LOOP --> Q[AI: for each negative_context of THIS brand<br/>present? yes / no / unsure + evidence]
  BR --> SAF[second model, independent: every negative context<br/>in frames from -30s to +40s: yes / no / unsure]
  CTX --> TAG[scene tags in that window<br/>matched to negative contexts]
  SAF --> FL[independent flags]
  TAG --> FL
  FL --> H
  Q --> H{code: any yes or unsure from placement model<br/>in either scene, or any independent flag?}
  H -- yes --> BLK[blocked, reason recorded]
  H -- no --> RANK[AI ranks eligible brands by dominant<br/>activity vs target_contexts, with rationale]
  RANK --> PICK[top brand + creative that fits the slot]
  BLK --> DBG[debug JSON]
  PICK --> DBG
```

## 4. Upload and job flow (built with local upload URL; GCS signed URL planned)

Cloud Run caps request bodies at 32 MiB and a Cloudflare proxy would cut requests at 100 s,
so uploads go straight to GCS and processing is always async (LOG 13:35).

```mermaid
sequenceDiagram
  participant B as Browser
  participant A as adbreak API
  participant G as GCS bucket
  participant J as job runner
  participant D as Postgres
  B->>A: POST /api/uploads
  A->>G: create signed resumable URL (local dev: /api/uploads/{id} on this server)
  A-->>B: upload URL + video id
  B->>G: PUT video bytes
  B->>A: POST /api/jobs {video id}
  A->>D: insert job (queued) (today: in-memory map, Postgres planned)
  A-->>B: job id (immediately)
  A->>J: event job.created
  loop each stage
    J->>D: stage result + progress
    B->>A: GET /api/jobs/{id}
    A-->>B: status + funnel counts
  end
  J->>G: VMAP, VAST, debug JSON, slates
```

## 5. Deployment (planned)

Decided: Cloud Run + GCS, one instance, budget alerts; Postgres pending decision (LOG 13:35, 16:34). Gemini through Vertex AI because the trial credit excludes AI Studio (LOG 16:34).
Deploy is deferred until Aranya is back (LOG 14:18).

```mermaid
flowchart LR
  U[judge browser] -->|https *.run.app| CR["Cloud Run service<br/>max instances 1<br/>debian-slim + ffmpeg + fonts"]
  CR --> GCS[(GCS bucket<br/>videos, slates, manifests)]
  U -->|signed URLs: upload + playback| GCS
  CR --> GROQ[Groq Whisper API]
  CR --> GEM["Vertex AI / Agent Platform: Gemini generateContent<br/>(service account, trial credit)"]
```

Local dev mirrors this with Postgres in Docker, local disk instead of GCS, static ffmpeg (LOG 14:15).
