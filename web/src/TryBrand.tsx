import { useState } from 'react'
import { fmt, type Result } from './api'

const SAMPLE = `{
  "brand_id": "brand_i",
  "display_name": "Brand I",
  "category": "smartphones",
  "target_contexts": ["mobile phone", "phone call", "smartphone", "texting", "video call", "taking photos", "social media"],
  "negative_contexts": ["funeral", "grief", "hospital", "violence"],
  "creatives": [
    { "id": "i_20s_bn", "duration_sec": 20, "language": "bn", "url": "ads/brand_i/i_20s_bn.mp4" },
    { "id": "i_30s_bn", "duration_sec": 30, "language": "bn", "url": "ads/brand_i/i_30s_bn.mp4" }
  ]
}`

type Job = { id: string; status: string; stage: string; log: string[] | null; error?: string; result?: string }

type TrialBreak = { t: number; winner: string; winner_fit: number; new_brand_fit: number; outcome: string; reason: string }
type EpisodeTrial = { episode: string; breaks: TrialBreak[]; won: number }

// Submits a try-a-brand job and polls it; returns the trial name.
async function runJob(url: string, body: string, onJob: (j: Job) => void): Promise<string> {
  const r = await fetch(url, { method: 'POST', body })
  let j: Job = await r.json()
  if (!r.ok) throw new Error((j as unknown as { error: string }).error)
  while (j.status === 'queued' || j.status === 'running') {
    onJob(j)
    await new Promise((res) => setTimeout(res, 1500))
    const pr = await fetch(`/api/jobs/${j.id}`)
    if (pr.status === 404) throw new Error('job lost: the server restarted. Please try again.')
    j = await pr.json()
  }
  onJob(j)
  if (j.status !== 'done' || !j.result) throw new Error(j.error ?? 'job failed')
  return j.result
}

function brandName(text: string): string {
  try {
    return JSON.parse(text).display_name ?? 'the new brand'
  } catch {
    return 'the new brand'
  }
}

function Outcome({ b, name, onWatch }: { b: TrialBreak; name: string; onWatch?: () => void }) {
  const label = { won: `${name} placed`, lost: `lost to ${b.winner}`, blocked: `${name} blocked`, unfit: `${name} not a fit` }[b.outcome] ?? b.outcome
  return (
    <div className={`outcome ${b.outcome}`}>
      <b>{fmt(b.t)}</b>
      <span className="tag-o">{label}</span>
      <span className="muted">{b.reason}</span>
      {onWatch && <button onClick={onWatch}>Watch</button>}
    </div>
  )
}

function JobLog({ job }: { job: Job | null }) {
  if (!job) return null
  return (
    <div className="joblog">
      <b>
        {job.status} {job.stage && `· ${job.stage}`}
      </b>
      {(job.log ?? []).map((l, i) => (
        <div key={i}>{l}</div>
      ))}
    </div>
  )
}

// Client-side version of the server summary, for a single-episode trial.
export function summarize(res: Result): TrialBreak[] {
  const nb = res.trial_brand ?? ''
  return (res.breaks ?? []).map((b) => {
    const d = b.placement.decision
    const nv = b.placement.verdicts.find((v) => v.brand_id === nb)
    const base = { t: b.t, winner: b.brand_name ?? '', winner_fit: d.fit ?? 0, new_brand_fit: nv?.fit ?? 0 }
    if (d.brand_id === nb) return { ...base, outcome: 'won', reason: d.rationale ?? '' }
    if (d.blocked[nb]) return { ...base, outcome: 'blocked', reason: d.blocked[nb] }
    if (d.unfit?.[nb]) return { ...base, outcome: 'unfit', reason: d.unfit[nb] }
    return { ...base, outcome: 'lost', reason: `${b.brand_name} fits better (${(d.fit ?? 0).toFixed(2)} vs ${(nv?.fit ?? 0).toFixed(2)})` }
  })
}

// Adds one brand the code has never seen to one episode and re-runs brand matching only.
export default function TryBrand({ id, onResult, onWatch }: { id: string; onResult: (r: Result, trial: string) => void; onWatch: (t: number) => void }) {
  const [text, setText] = useState(SAMPLE)
  const [job, setJob] = useState<Job | null>(null)
  const [err, setErr] = useState('')
  const [summary, setSummary] = useState<TrialBreak[] | null>(null)
  const [busy, setBusy] = useState(false)

  async function run() {
    setErr('')
    setSummary(null)
    setBusy(true)
    try {
      const trial = await runJob(`/api/episodes/${id}/try-brand`, text, setJob)
      const res: Result = await (await fetch(`/api/episodes/${id}/trials/${trial}`)).json()
      onResult(res, trial)
      setSummary(summarize(res))
    } catch (e) {
      setErr((e as Error).message)
    } finally {
      setBusy(false)
    }
  }

  const name = brandName(text)
  const won = summary?.filter((b) => b.outcome === 'won').length ?? 0
  return (
    <section className="trybrand">
      <h3>Add a brand the system has never seen</h3>
      <p className="muted">Paste a catalogue entry with a synthetic brand name and new ids. Break positions stay; only brand matching re-runs, with no code change.</p>
      <textarea value={text} onChange={(e) => setText(e.target.value)} rows={12} spellCheck={false} />
      <button onClick={run} disabled={busy}>
        Re-match this episode with {name}
      </button>
      {err && <p className="err">{err}</p>}
      {!summary && <JobLog job={job} />}
      {summary && (
        <div className="trial-summary">
          <p>
            <b>
              {name} won {won} of {summary.length} break{summary.length === 1 ? '' : 's'}.
            </b>{' '}
            The player and the breaks above now use this re-match; winning breaks are marked NEW BRAND.
          </p>
          {summary.map((b) => (
            <Outcome key={b.t} b={b} name={name} onWatch={() => onWatch(b.t)} />
          ))}
        </div>
      )}
    </section>
  )
}

// Tests one new brand against every analysed episode in a single job.
export function TryBrandAll({ open }: { open: (path: string) => void }) {
  const [text, setText] = useState(SAMPLE)
  const [job, setJob] = useState<Job | null>(null)
  const [err, setErr] = useState('')
  const [rows, setRows] = useState<{ trial: string; eps: EpisodeTrial[] } | null>(null)
  const [busy, setBusy] = useState(false)

  async function run() {
    setErr('')
    setRows(null)
    setBusy(true)
    try {
      const trial = await runJob('/api/try-brand', text, setJob)
      const eps: EpisodeTrial[] = await (await fetch(`/api/trials/${trial}`)).json()
      setRows({ trial, eps })
    } catch (e) {
      setErr((e as Error).message)
    } finally {
      setBusy(false)
    }
  }

  const name = brandName(text)
  const total = rows?.eps.reduce((n, e) => n + e.breaks.length, 0) ?? 0
  const won = rows?.eps.reduce((n, e) => n + e.won, 0) ?? 0
  return (
    <section className="trybrand all">
      <h3>Test a new brand across every episode</h3>
      <p className="muted">One job re-matches all episodes with this brand added. See where it wins, where it loses and where it is blocked.</p>
      <textarea value={text} onChange={(e) => setText(e.target.value)} rows={12} spellCheck={false} />
      <button onClick={run} disabled={busy}>
        Test {name} across all episodes
      </button>
      {err && <p className="err">{err}</p>}
      {!rows && <JobLog job={job} />}
      {rows && (
        <div className="trial-summary">
          <p>
            <b>
              {name} won {won} of {total} scheduled breaks across {rows.eps.length} episodes.
            </b>
          </p>
          {rows.eps.map((e) => (
            <div key={e.episode} className="trial-ep">
              <div className="row">
                <strong>{e.episode.replaceAll('_', ' ')}</strong>
                <span className="muted">
                  {e.breaks.length === 0 ? 'no scheduled breaks' : `won ${e.won} of ${e.breaks.length}`}
                </span>
                {e.breaks.length > 0 && <button onClick={() => open(`/episodes/${e.episode}?trial=${rows.trial}`)}>Open re-matched episode</button>}
              </div>
              {e.breaks.map((b) => (
                <Outcome key={b.t} b={b} name={name} />
              ))}
            </div>
          ))}
        </div>
      )}
    </section>
  )
}
