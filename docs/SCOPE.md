# SCOPE

Status: approved by Aranya 14:15 IST, 26 Sep 2026. Changes go through LOG.jsonl.

## What this is

adbreak ingests a long-form Bengali drama episode, finds scene boundaries where
an ad break would feel natural (never mid-dialogue), decides how many breaks the
episode actually warrants under explicit pacing rules, and assigns each break the
most contextually fitting brand from a runtime-loaded synthetic catalogue, with
negative contexts enforced as a hard block per brand. It emits a VMAP manifest
(with VAST ad responses), a debug JSON that explains every accept and reject, and
a web player that plays the episode, cuts to the ad and resumes. AI does the
judgment (scene semantics, break quality, brand fit); deterministic code enforces
the guarantees (speech guard, pacing, negative-context block). One Go binary,
deployed on Cloud Run.

## NON-GOALS

Re-read before any significant change. If a request contradicts this list, say so.

- No problem statements other than P1.
- No hard-coded timestamps or brand assignments for any sample video, not even
  temporarily after the vertical slice.
- No real company names. Only the synthetic brands from brands.json (and any
  brand added at runtime).
- No self-hosted models today. Every AI dependency sits behind a provider
  interface; the self-hosted implementations (whisper.cpp, vLLM) are a named
  seam, not built.
- No sending the full episode as video to the VLM.
- No synchronous processing requests. Upload returns a job id, the browser polls.
- No user accounts, auth, multi-tenancy or billing.
- No message broker, Kubernetes, compose stack or observability stack. In-process
  event bus, one instance.
- No custom domain today. Demo URL is the *.run.app one.
- No real ad creatives. Ad slates are generated with ffmpeg at exact durations.
- No Google IMA SDK dependency in the player; our own player consumes our VMAP.
- No fine-tuning, training or model hosting.
- No editing UI for breaks (read-only results, plus add-a-brand and re-run).
- No mobile-specific layout work beyond "does not break".

## THE DEMO

The exact sequence shown at the end. Anything not in here is out of scope.

1. Open the *.run.app URL. Library of the 6 sample episodes, each marked
   "computed by pipeline vX at T", plus pacing config shown on screen.
2. Open one episode. Timeline shows scenes, candidate breaks (rejected ones
   greyed with reason), and the selected breaks with their brand.
3. Open the scene list: every scene with start, end, duration, description and
   dominant activity, clickable to seek. Judged on its own (blind-viewing).
4. Show the funnel: shots -> scenes -> candidates -> pass speech guard ->
   scored -> selected -> matched, with counts.
5. Click a selected break. Player seeks to a few seconds before it, the drama
   cuts to the brand slate, the slate plays, the drama resumes.
6. Open the break detail: why here (speech gap, scene change, AI rationale),
   why this brand, and which brands were blocked and by which negative context.
7. Show a negative-context block on a food-heavy episode (e.g. Brand B blocked
   for "eating").
8. Add a 9th brand as JSON in the UI, re-run brand matching, it gets placed
   with zero code change.
9. Upload a fresh video (held-out style): job runs async with live stage
   progress, results appear.
10. Download the VMAP and debug JSON.

## Tasks

Vertical slice target: 16:00 to 16:30 IST. Feature freeze: 23:00 IST. Submit by 00:30.

### 0. Setup
- [x] 0.1 Repo, Go module, layout, docs folder
- [x] 0.2 Postgres (Docker), ffmpeg (static), assets downloaded and probed, API keys verified
- [x] 0.3 Public GitHub repo, first push
- [ ] 0.4 GCP project, bucket, budget alert, gcloud auth (with Aranya)

### 1. Docs
- [x] 1.1 SCOPE.md approved
- [x] 1.2 LOG.jsonl (append-only, ongoing)
- [x] 1.3 ARCHITECTURE.md (Mermaid, every node traceable to SCOPE or LOG)

### 2. Foundation
- [x] 2.1 Config printed at startup, health endpoint, embedded SPA with fallback
- [ ] 2.2 Postgres schema + migrations (jobs, stage results, AI call cache)
- [x] 2.3 In-process event bus + job runner with stage progress
- [ ] 2.4 Storage interface (local disk in dev, GCS in prod)
- [~] 2.5 Dockerfile (debian-slim + ffmpeg + fonts) built and run locally; deploy pending

### 3. Vertical slice
- [x] 3.1 Probe + shot detection
- [x] 3.2 Groq Whisper, chunked, word + segment timestamps
- [x] 3.3 Candidates at shot boundaries + speech guard (segment spans + margin)
- [x] 3.4 Pacing DP
- [x] 3.5 Brand pick with per-brand negative block
- [x] 3.6 Ad slate generator (15/20/30s)
- [x] 3.7 VMAP + VAST + debug JSON
- [x] 3.8 Player: seek before break, cut to slate, resume

### 4. Depth
- [x] 4.1 Scene segmentation: histogram + transcript propose, AI describes and refines
- [x] 4.2 Scene list view + scenes in debug JSON
- [x] 4.3 Gemini audio check on surviving candidates (second speech signal)
- [x] 4.4 AI break scoring with rationale
- [x] 4.5 Semantic per-brand negative check on scenes before and after, uncertain = blocked; ranking with rationale
- [x] 4.6 Debug JSON with rejections + funnel counts; timeline, break detail, funnel UI
- [~] 4.7 Async upload: signed URL to GCS, job id, polling, stage progress
- [ ] 4.8 AI call cache by content hash + Re-run live
- [ ] 4.9 Add-a-9th-brand flow in UI
- [~] 4.10 Budget guard: max upload MB + duration and one job at a time done; token cap not done
- [ ] 4.11 Precompute all 6 episodes on the deployed instance

### 5. Tests (alongside the code)
- [x] 5.1 Speech guard
- [x] 5.2 Pacing DP
- [x] 5.3 Negative filter (never passes, uncertain blocks, both scenes checked)
- [x] 5.4 9th brand fixture
- [x] 5.5 VMAP/VAST well-formed, durations match
- [x] 5.6 Whisper chunk merge
- [ ] 5.7 End-to-end smoke on a short clip with recorded AI responses

### 6. CI
- [x] 6.1 GitHub Actions: go vet, go test, web build (parallel jobs) on push and PR

### 7. Validation checkpoints (other chat)
- [ ] 7.1 SCOPE  - [ ] 7.2 after slice  - [ ] 7.3 brand matching  - [ ] 7.4 cut list ~21:30

### 8. Ship
- [ ] 8.1 Freeze 23:00, README, full demo pass on prod, record video, submit before 00:30
