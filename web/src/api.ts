export type Funnel = {
  shots: number
  scenes: number
  candidates: number
  pass_hard_filters: number
  pass_ai_speech_check: number
  caught_by_ai_audio_check: number
  break_budget: number
  considered_for_placement: number
  placed: number
  suppressed_for_brand_safety: number
}

export type Summary = {
  id: string
  duration: number
  breaks: number
  pipeline_version: string
  computed_at: string
  funnel: Funnel
  break_times: number[]
  suppressed_times: number[]
  brands: string[]
}

export type SceneRead = { description: string; dominant_activity: string; contexts: string[] }

export type Verdict = {
  brand_id: string
  fit: number
  rationale: string
  negatives: { context: string; before: string; after: string; evidence: string }[]
}

export type Placement = {
  t: number
  scene_before: SceneRead
  scene_after: SceneRead
  verdicts: Verdict[]
  decision: {
    brand_id?: string
    creative_id?: string
    seconds?: number
    fit?: number
    rationale?: string
    blocked: Record<string, string>
    unblocked: string[]
  }
}

export type Scene = {
  index: number
  start: number
  end: number
  first_shot: number
  last_shot: number
  description: string
  dominant_activity: string
  contexts: string[] | null
  mood: string
  source: string
}

export type Candidate = { t: number; score: number; signals?: string[]; rejected?: string; rationale?: string }

export type Break = {
  t: number
  score: number
  rationale: string
  placement: Placement
  boundary?: { scene_index: number; scene_start: number; shift_sec: number; note: string }
  safety_checks?: { context: string; answer: string; evidence: string }[]
  independent_flags?: Record<string, string> | null
  brand_name?: string
  creative?: { id: string; duration_sec: number }
}

export type Result = {
  episode: string
  pipeline_version: string
  computed_at: string
  media: { duration: number; width: number; height: number; fps: number }
  scenes: Scene[] | null
  pacing: Record<string, number>
  effective_pacing?: { head_margin_sec: number; tail_margin_sec: number; min_gap_sec: number; break_budget: number }
  funnel: Funnel
  breaks: Break[] | null
  unplaced?: Placement[] | null
  candidates: Candidate[]
  asr_provider: string
  ai_provider: string
}

export type AdSlot = { offset: number; breakId: string; title: string; duration: number; media: string }

async function get<T>(url: string): Promise<T> {
  const r = await fetch(url)
  if (!r.ok) throw new Error(`${url}: HTTP ${r.status}`)
  return r.json()
}

export const listEpisodes = () => get<Summary[]>('/api/episodes')
export const getEpisode = (id: string) => get<Result>(`/api/episodes/${id}`)

function clockToSec(s: string): number {
  const [h, m, sec] = s.split(':')
  return Number(h) * 3600 + Number(m) * 60 + Number(sec)
}

// The player schedules ads from the VMAP document itself, not from the debug JSON.
export async function loadVMAP(id: string, trial?: string): Promise<AdSlot[]> {
  const r = await fetch(`/api/episodes/${id}/vmap.xml${trial ? `?trial=${trial}` : ''}`)
  if (!r.ok) throw new Error(`vmap: HTTP ${r.status}`)
  const doc = new DOMParser().parseFromString(await r.text(), 'application/xml')
  const NS = 'http://www.iab.net/videosuite/vmap'
  return Array.from(doc.getElementsByTagNameNS(NS, 'AdBreak')).map((b) => ({
    offset: clockToSec(b.getAttribute('timeOffset') ?? '0'),
    breakId: b.getAttribute('breakId') ?? '',
    title: b.getElementsByTagName('AdTitle')[0]?.textContent ?? '',
    duration: clockToSec(b.getElementsByTagName('Duration')[0]?.textContent ?? '0'),
    media: (b.getElementsByTagName('MediaFile')[0]?.textContent ?? '').trim(),
  }))
}

export function fmt(sec: number): string {
  const m = Math.floor(sec / 60)
  const s = Math.floor(sec % 60)
  return `${m}:${String(s).padStart(2, '0')}`
}

export function title(id: string): string {
  if (id.startsWith('up_')) return 'Uploaded video'
  return id.replaceAll('_', ' ').replace(/\b\w/g, (c) => c.toUpperCase())
}
