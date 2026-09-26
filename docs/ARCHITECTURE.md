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
  subgraph bin["adbreak binary (cmd/server)"]
    server["server<br/>HTTP API /api + embedded SPA"]
    config["config<br/>env, printed at startup"]
    jobs["jobs<br/>job runner + event bus (planned)"]
    store["store<br/>Postgres repo (planned)"]
    blob["blob<br/>local disk | GCS (planned)"]
    media["media<br/>ffprobe, shots, audio, keyframes (planned)"]
    speech["speech<br/>ASR provider: groq-whisper (planned)"]
    ai["ai<br/>LLM/VLM provider: gemini (planned)"]
    scenes["scenes<br/>boundaries + descriptions (planned)"]
    breaks["breaks<br/>candidates, speech guard, scoring, pacing DP (planned)"]
    brands["brands<br/>catalogue, negative block, ranking (planned)"]
    slates["slates<br/>ffmpeg ad slate generator (planned)"]
    manifest["manifest<br/>VMAP + VAST + debug JSON (planned)"]
  end
  web["web/ React SPA<br/>embedded via go:embed"] --> server
  server --> jobs
  jobs --> media & speech & scenes & breaks & brands & manifest
  scenes --> ai
  breaks --> ai
  brands --> ai
  jobs --> store
  media --> blob
  slates --> blob
  manifest --> blob
```

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
  P --> SH[shot detection<br/>ffmpeg]
  P --> AU[audio extract 16k mono<br/>chunked]
  AU --> ASR[ASR: Whisper large-v3 on Groq<br/>word + segment timestamps]
  SH --> SC[scene boundaries proposed<br/>colour histogram + transcript gaps]
  ASR --> SC
  SC --> SD[AI per scene: description, activity,<br/>contexts; confirm or merge boundary]
  SD --> C[candidates = scene boundaries]
  C --> G1{hard filters<br/>head/tail margin<br/>not inside Whisper segment + margin}
  G1 -- reject --> R1[rejected with reason]
  G1 -- pass --> G2{AI audio check +-5s<br/>anyone mid-utterance?}
  G2 -- reject --> R1
  G2 -- pass --> S[score survivors<br/>signals + AI rationale]
  S --> DP[pacing DP<br/>max score s.t. min gap, per hour cap,<br/>ad load, min score threshold]
  DP --> B[brand match per selected break]
  B --> M[VMAP + VAST + debug JSON]
```

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
  Q --> H{code: any yes or unsure<br/>in either scene?}
  H -- yes --> BLK[blocked, reason recorded]
  H -- no --> RANK[AI ranks eligible brands by dominant<br/>activity vs target_contexts, with rationale]
  RANK --> PICK[top brand + creative that fits the slot]
  BLK --> DBG[debug JSON]
  PICK --> DBG
```

## 4. Upload and job flow (planned)

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
  A->>G: create signed resumable URL
  A-->>B: upload URL + video id
  B->>G: PUT video bytes
  B->>A: POST /api/jobs {video id}
  A->>D: insert job (queued)
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

Decided: Cloud Run + GCS + Neon Postgres, one instance, Rs 300 budget alert (LOG 13:35).
Deploy is deferred until Aranya is back (LOG 14:18).

```mermaid
flowchart LR
  U[judge browser] -->|https *.run.app| CR["Cloud Run service<br/>max instances 1<br/>debian-slim + ffmpeg + fonts"]
  CR --> NEON[(Neon Postgres)]
  CR --> GCS[(GCS bucket<br/>videos, slates, manifests)]
  U -->|signed URLs: upload + playback| GCS
  CR --> GROQ[Groq Whisper API]
  CR --> GEM[Gemini API]
```

Local dev mirrors this with Postgres in Docker, local disk instead of GCS, static ffmpeg (LOG 14:15).
