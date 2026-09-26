import { useEffect, useRef, useState } from 'react'
import Player, { type PlayerHandle } from './Player'
import { fmt, getEpisode, listEpisodes, loadVMAP, type AdSlot, type Result, type Summary } from './api'

function useRoute() {
  const [path, setPath] = useState(location.pathname)
  useEffect(() => {
    const on = () => setPath(location.pathname)
    addEventListener('popstate', on)
    return () => removeEventListener('popstate', on)
  }, [])
  const go = (p: string) => {
    history.pushState(null, '', p)
    setPath(p)
  }
  return { path, go }
}

export default function App() {
  const { path, go } = useRoute()
  const m = path.match(/^\/episodes\/([\w-]+)/)
  return (
    <div className="app">
      <header>
        <a href="/" onClick={(e) => (e.preventDefault(), go('/'))} className="logo">
          adbreak
        </a>
        <span className="tag">context-aware ad breaks for Bengali drama</span>
      </header>
      {m ? <Episode id={m[1]} /> : <Library open={(id) => go(`/episodes/${id}`)} />}
    </div>
  )
}

function Library({ open }: { open: (id: string) => void }) {
  const [eps, setEps] = useState<Summary[] | null>(null)
  const [err, setErr] = useState('')
  useEffect(() => {
    listEpisodes().then(setEps).catch((e) => setErr(String(e)))
  }, [])
  if (err) return <p className="err">{err}</p>
  if (!eps) return <p>Loading…</p>
  return (
    <main>
      <h2>Episodes</h2>
      <div className="grid">
        {eps.map((e) => (
          <button key={e.id} className="card" onClick={() => open(e.id)}>
            <strong>{e.id.replaceAll('_', ' ')}</strong>
            <span>{fmt(e.duration)} · {e.breaks} break{e.breaks === 1 ? '' : 's'}</span>
            <small>
              {e.funnel.shots} shots → {e.funnel.pass_hard_filters} past speech guard → {e.funnel.pass_ai_speech_check} past AI check → {e.funnel.placed} placed
            </small>
            <small className="muted">computed by pipeline v{e.pipeline_version} at {e.computed_at}</small>
          </button>
        ))}
      </div>
    </main>
  )
}

function Episode({ id }: { id: string }) {
  const [res, setRes] = useState<Result | null>(null)
  const [slots, setSlots] = useState<AdSlot[]>([])
  const [err, setErr] = useState('')
  const player = useRef<PlayerHandle>(null)
  useEffect(() => {
    Promise.all([getEpisode(id), loadVMAP(id)])
      .then(([r, s]) => (setRes(r), setSlots(s)))
      .catch((e) => setErr(String(e)))
  }, [id])
  if (err) return <p className="err">{err}</p>
  if (!res) return <p>Loading…</p>
  const f = res.funnel
  const breaks = res.breaks ?? []
  return (
    <main>
      <h2>{id.replaceAll('_', ' ')}</h2>
      <p className="muted">
        pipeline v{res.pipeline_version} · {res.asr_provider} · {res.ai_provider} · computed {res.computed_at}
      </p>
      <Player ref={player} src={`/media/episodes/${id}.mp4`} slots={slots} duration={res.media.duration} />

      <section>
        <h3>Funnel</h3>
        <div className="funnel">
          {[
            ['shot cuts', f.candidates],
            ['pass head/tail + speech guard', f.pass_hard_filters],
            ['pass AI speech check', f.pass_ai_speech_check],
            [`selected (budget ${f.break_budget})`, f.selected],
            ['placed with a brand', f.placed],
          ].map(([label, n]) => (
            <div key={label as string}>
              <b>{n}</b>
              <span>{label}</span>
            </div>
          ))}
        </div>
      </section>

      <section>
        <h3>Breaks</h3>
        {breaks.length === 0 && <p className="muted">No break cleared the rules for this episode.</p>}
        {breaks.map((b, i) => (
          <div key={i} className="break">
            <div className="row">
              <b>{fmt(b.t)}</b>
              <span className="brand">{b.brand_name}</span>
              <span className="muted">{b.creative?.id} · score {b.score.toFixed(2)} · fit {b.placement.decision.fit?.toFixed(2)}</span>
              <button onClick={() => player.current?.seek(Math.max(0, b.t - 6))}>Play 6s before</button>
            </div>
            <p>Why here: {b.rationale}</p>
            <p>Why this brand: {b.placement.decision.rationale}</p>
            <div className="scenes">
              <div>
                <small>scene before</small>
                <p>{b.placement.scene_before.description}</p>
                <small className="muted">{b.placement.scene_before.dominant_activity} · {b.placement.scene_before.contexts.join(', ')}</small>
              </div>
              <div>
                <small>scene after</small>
                <p>{b.placement.scene_after.description}</p>
                <small className="muted">{b.placement.scene_after.dominant_activity} · {b.placement.scene_after.contexts.join(', ')}</small>
              </div>
            </div>
            {Object.keys(b.placement.decision.blocked).length > 0 && (
              <div className="blocked">
                <small>blocked brands (hard rule)</small>
                {Object.entries(b.placement.decision.blocked).map(([k, v]) => (
                  <div key={k}>
                    <b>{k}</b> {v}
                  </div>
                ))}
              </div>
            )}
          </div>
        ))}
      </section>

      <section>
        <h3>Candidates reviewed by AI</h3>
        <table>
          <thead>
            <tr>
              <th>time</th>
              <th>score</th>
              <th>verdict</th>
              <th>reason</th>
            </tr>
          </thead>
          <tbody>
            {res.candidates
              .filter((c) => !c.rejected || c.rejected.startsWith('AI'))
              .map((c) => (
                <tr key={c.t} className={c.rejected ? 'rej' : ''} onClick={() => player.current?.seek(Math.max(0, c.t - 6))}>
                  <td>{fmt(c.t)}</td>
                  <td>{c.rejected ? '' : c.score.toFixed(2)}</td>
                  <td>{c.rejected ?? 'eligible'}</td>
                  <td>{c.rationale}</td>
                </tr>
              ))}
          </tbody>
        </table>
      </section>

      <section className="downloads">
        <a href={`/api/episodes/${id}/vmap.xml`} target="_blank">VMAP manifest</a>
        <a href={`/api/episodes/${id}`} target="_blank">debug JSON</a>
      </section>
    </main>
  )
}
