import { useEffect, useRef, useState } from 'react'
import Player, { type PlayerHandle } from './Player'
import Upload from './Upload'
import TryBrand from './TryBrand'
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
  const [pacing, setPacing] = useState<Record<string, number> | null>(null)
  const [err, setErr] = useState('')
  useEffect(() => {
    listEpisodes().then(setEps).catch((e) => setErr(String(e)))
    fetch('/api/config')
      .then((r) => r.json())
      .then((c) => setPacing(c.pacing))
      .catch(() => {})
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
            <span>
              {fmt(e.duration)} · {e.funnel.considered_for_placement} safe break{e.funnel.considered_for_placement === 1 ? '' : 's'} found · {e.funnel.placed} placed
              {e.funnel.suppressed_for_brand_safety > 0 && ` · ${e.funnel.suppressed_for_brand_safety} suppressed for brand safety`}
            </span>
            <small>
              {e.funnel.shots} shots → {e.funnel.scenes} scenes → {e.funnel.pass_hard_filters} past transcript guard → AI audio check caught{' '}
              {e.funnel.caught_by_ai_audio_check} mid-dialogue cuts
            </small>
            <small className="muted">computed by pipeline v{e.pipeline_version} at {e.computed_at}</small>
          </button>
        ))}
      </div>
      <Upload done={open} />
      {pacing && (
        <section>
          <h3>Pacing rules (config, enforced in code)</h3>
          <div className="pacing">
            {Object.entries(pacing).map(([k, v]) => (
              <div key={k}>
                <b>{v}</b>
                <span>{k.replaceAll('_', ' ')}</span>
              </div>
            ))}
          </div>
        </section>
      )}
    </main>
  )
}

function Episode({ id }: { id: string }) {
  const [base, setBase] = useState<Result | null>(null)
  const [trial, setTrial] = useState<{ res: Result; name: string } | null>(null)
  const [slots, setSlots] = useState<AdSlot[]>([])
  const [err, setErr] = useState('')
  const player = useRef<PlayerHandle>(null)
  useEffect(() => {
    Promise.all([getEpisode(id), loadVMAP(id)])
      .then(([r, s]) => (setBase(r), setSlots(s)))
      .catch((e) => setErr(String(e)))
  }, [id])
  const res = trial ? trial.res : base
  async function showTrial(r: Result, name: string) {
    setTrial({ res: r, name })
    setSlots(await loadVMAP(id, name))
  }
  async function showBase() {
    setTrial(null)
    setSlots(await loadVMAP(id))
  }
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
      {trial && (
        <p className="trialbar">
          Showing re-match with a runtime brand ({trial.name}). <button onClick={showBase}>Back to original catalogue</button>
        </p>
      )}
      <Player ref={player} src={`/media/episodes/${id}.mp4`} slots={slots} duration={res.media.duration} sceneStarts={(res.scenes ?? []).slice(1).map((s) => s.start)} />
      <p className="legend"><i className="lg-scene" /> scene boundary <i className="lg-break" /> ad break</p>

      {res.effective_pacing && (
        <p className="muted">
          Pacing for this {fmt(res.media.duration)} episode: head {Math.round(res.effective_pacing.head_margin_sec)}s, tail{' '}
          {Math.round(res.effective_pacing.tail_margin_sec)}s, min gap {Math.round(res.effective_pacing.min_gap_sec)}s, budget{' '}
          {res.effective_pacing.break_budget} breaks
        </p>
      )}
      <section>
        <h3>Funnel</h3>
        <div className="funnel">
          {[
            ['shots', f.shots],
            ['scenes', f.scenes],
            ['pass head/tail + transcript guard', f.pass_hard_filters],
            ['mid-dialogue cuts caught by AI audio check (transcript said silent)', f.caught_by_ai_audio_check],
            [`safe breaks considered (budget ${f.break_budget})`, f.considered_for_placement],
            ['placed with a brand', f.placed],
            ['suppressed for brand safety', f.suppressed_for_brand_safety],
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
        {breaks.length === 0 && (
          <p className="muted">
            No break was placed. {f.suppressed_for_brand_safety > 0 ? 'Every safe pause sits next to content no brand may appear against (see below).' : 'No pause cleared the rules.'}
          </p>
        )}
        {breaks.map((b, i) => (
          <div key={i} className="break">
            <div className="row">
              <b>{fmt(b.t)}</b>
              <span className="brand">{b.brand_name}</span>
              <span className="muted">{b.creative?.id} · score {b.score.toFixed(2)} · fit {b.placement.decision.fit?.toFixed(2)}</span>
              <button onClick={() => player.current?.seek(Math.max(0, b.t - 6))}>Play 6s before</button>
            </div>
            <p>Why here: {b.rationale}</p>
            {b.boundary && <p className="muted">Scene boundary: {b.boundary.note}</p>}
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
            {b.safety_checks && (
              <div className="safety">
                <small>independent safety check (second model, 30s before to 40s after)</small>
                <div>
                  {b.safety_checks.map((c) => (
                    <span key={c.context} className={c.answer === 'no' ? 'chip ok' : 'chip bad'} title={c.evidence}>
                      {c.context}: {c.answer}
                    </span>
                  ))}
                </div>
              </div>
            )}
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

      {(res.unplaced ?? []).length > 0 && (
        <section>
          <h3>Suppressed for brand safety ({(res.unplaced ?? []).length})</h3>
          <p className="muted">Speech-safe pauses where no brand was allowed. A wrongly placed ad is a violation; a skipped break costs one impression.</p>
          {(res.unplaced ?? []).map((u) => {
            const reasons = Object.values(u.decision.blocked)
            const top = reasons.find((r) => r.includes('independent')) ?? reasons[0] ?? 'no brand fit this moment'
            return (
              <div key={u.t} className="break suppressed">
                <div className="row">
                  <b>{fmt(u.t)}</b>
                  <span className="muted">{Object.keys(u.decision.blocked).length} of {Object.keys(u.decision.blocked).length + u.decision.unblocked.length} brands blocked</span>
                  <button onClick={() => player.current?.seek(Math.max(0, u.t - 6))}>Watch</button>
                </div>
                <p>{u.scene_before.description}</p>
                <p className="blocked">{top}</p>
              </div>
            )
          })}
        </section>
      )}

      <section>
        <h3>Scenes ({(res.scenes ?? []).length})</h3>
        <table>
          <thead>
            <tr>
              <th>start</th>
              <th>length</th>
              <th>activity</th>
              <th>description</th>
              <th>contexts</th>
            </tr>
          </thead>
          <tbody>
            {(res.scenes ?? []).map((s) => (
              <tr key={s.index} onClick={() => player.current?.seek(s.start)}>
                <td>{fmt(s.start)}</td>
                <td>{fmt(s.end - s.start)}</td>
                <td>{s.dominant_activity}</td>
                <td>{s.description}</td>
                <td className="muted">{(s.contexts ?? []).join(', ')}</td>
              </tr>
            ))}
          </tbody>
        </table>
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

      <TryBrand id={id} onResult={showTrial} />

      <section className="downloads">
        <a href={`/api/episodes/${id}/vmap.xml${trial ? `?trial=${trial.name}` : ''}`} target="_blank">VMAP manifest</a>
        <a href={`/api/episodes/${id}`} target="_blank">debug JSON</a>
      </section>
    </main>
  )
}
