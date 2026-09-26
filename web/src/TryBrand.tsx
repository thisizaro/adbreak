import { useState } from 'react'
import type { Result } from './api'

const SAMPLE = `{
  "brand_id": "brand_i",
  "display_name": "Brand I",
  "category": "home security",
  "target_contexts": ["night", "street", "watching", "locks", "home safety", "walking alone", "surveillance"],
  "negative_contexts": ["funeral", "grief", "hospital"],
  "creatives": [
    { "id": "i_20s_bn", "duration_sec": 20, "language": "bn", "url": "ads/brand_i/i_20s_bn.mp4" },
    { "id": "i_30s_bn", "duration_sec": 30, "language": "bn", "url": "ads/brand_i/i_30s_bn.mp4" }
  ]
}`

type Job = { id: string; status: string; stage: string; log: string[] | null; error?: string; result?: string }

// Adds one brand the code has never seen and re-runs brand matching only.
export default function TryBrand({ id, onResult }: { id: string; onResult: (r: Result, trial: string) => void }) {
  const [text, setText] = useState(SAMPLE)
  const [job, setJob] = useState<Job | null>(null)
  const [err, setErr] = useState('')

  async function run() {
    setErr('')
    setJob(null)
    try {
      const r = await fetch(`/api/episodes/${id}/try-brand`, { method: 'POST', body: text })
      let j: Job = await r.json()
      if (!r.ok) throw new Error((j as unknown as { error: string }).error)
      while (j.status === 'queued' || j.status === 'running') {
        setJob(j)
        await new Promise((res) => setTimeout(res, 1500))
        const pr = await fetch(`/api/jobs/${j.id}`)
        if (pr.status === 404) throw new Error('job lost: the server restarted. Please try again.')
        j = await pr.json()
      }
      setJob(j)
      if (j.status !== 'done' || !j.result) throw new Error(j.error ?? 'job failed')
      const res: Result = await (await fetch(`/api/episodes/${id}/trials/${j.result}`)).json()
      onResult(res, j.result)
    } catch (e) {
      setErr((e as Error).message)
    }
  }

  return (
    <section className="trybrand">
      <h3>Add a brand the system has never seen</h3>
      <p className="muted">Paste a catalogue entry with a synthetic brand name and new ids. Break positions stay; only brand matching re-runs, with no code change.</p>
      <textarea value={text} onChange={(e) => setText(e.target.value)} rows={12} spellCheck={false} />
      <button onClick={run} disabled={job?.status === 'running' || job?.status === 'queued'}>
        Re-match with this brand
      </button>
      {err && <p className="err">{err}</p>}
      {job && (
        <div className="joblog">
          <b>
            {job.status} {job.stage && `· ${job.stage}`}
          </b>
          {(job.log ?? []).map((l, i) => (
            <div key={i}>{l}</div>
          ))}
        </div>
      )}
    </section>
  )
}
