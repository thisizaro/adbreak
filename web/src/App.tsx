import { useEffect, useRef, useState } from 'react'
import Strip from './Strip'
import Player, { type PlayerHandle } from './Player'
import Upload from './Upload'
import TryBrand from './TryBrand'
import { fmt, title, getEpisode, listEpisodes, loadVMAP, type AdSlot, type Result, type Summary } from './api'

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
      <header className="masthead">
        <a href="/" onClick={(e) => (e.preventDefault(), go('/'))} className="logo">
          <span className="onair-dot" aria-hidden="true" />
          adbreak
        </a>
        <span className="tag">Context-aware ad breaks for Bengali drama</span>
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
      <section className="intro">
        <h1>Where an ad can interrupt the story, and which brand belongs there.</h1>
        <p className="lede">
          Each episode below was segmented into scenes, checked for speech at every cut, and given only the breaks the pacing rules allow. A brand is placed only
          if nothing around the break is on its list of contexts to avoid.
        </p>
        <ol className="steps">
          <li>
            <b>Open an episode</b>
            <span>The strip under each title is its ad schedule: red for breaks, amber for breaks held back for brand safety.</span>
          </li>
          <li>
            <b>Click a red mark on the timeline</b>
            <span>Playback starts 6 seconds before the break, cuts to the ad and resumes where it left off.</span>
          </li>
          <li>
            <b>Read why</b>
            <span>Every break explains why that moment, why that brand, and which brands were blocked and on what evidence.</span>
          </li>
          <li>
            <b>Try your own</b>
            <span>Add a brand the system has never seen, or upload an episode and watch it being analysed.</span>
          </li>
        </ol>
      </section>

      <section>
        <h2>Episodes</h2>
        <div className="rundown">
          {eps.map((e) => (
            <button key={e.id} className="card" onClick={() => open(e.id)}>
              <span className="ep-title">{title(e.id)}</span>
              <span className="ep-len">{fmt(e.duration)}</span>
              <Strip duration={e.duration} breaks={e.break_times ?? []} suppressed={e.suppressed_times ?? []} />
              <span className="ep-facts">
                <span className="fact onair">{e.funnel.placed} placed</span>
                {e.funnel.suppressed_for_brand_safety > 0 && <span className="fact held">{e.funnel.suppressed_for_brand_safety} held back</span>}
                <span className="fact">{e.funnel.scenes} scenes</span>
                <span className="fact">{e.funnel.caught_by_ai_audio_check} mid-dialogue cuts caught</span>
              </span>
            </button>
          ))}
        </div>
        <p className="note">Computed by pipeline v{eps[0]?.pipeline_version}. Brands are the synthetic catalogue from the hackathon kit.</p>
      </section>

      <Upload done={open} />

      {pacing && (
        <section>
          <h2>Pacing rules</h2>
          <p className="note">Limits, not targets. Enforced in code; margins and gaps scale with episode length.</p>
          <dl className="pacing">
            <div><dt>Breaks per hour, at most</dt><dd>{pacing.max_breaks_per_hour}</dd></div>
            <div><dt>Ad load, at most</dt><dd>{pacing.max_ad_load_pct}%</dd></div>
            <div><dt>No break in the first</dt><dd>{Math.round(pacing.head_margin_sec / 60)} min or {pacing.head_pct}%</dd></div>
            <div><dt>No break in the last</dt><dd>{Math.round(pacing.tail_margin_sec / 60)} min or {pacing.tail_pct}%</dd></div>
            <div><dt>Gap between breaks, at least</dt><dd>{Math.round(pacing.min_gap_sec / 60)} min or {Math.round(pacing.gap_fraction * 100)}% of runtime</dd></div>
            <div><dt>Distance from any speech</dt><dd>{pacing.speech_margin_sec * 1000} ms</dd></div>
          </dl>
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
      <div className="ep-head">
        <h1>{title(id)}</h1>
        <p className="lede">
          {fmt(res.media.duration)} runtime, {f.scenes} scenes. {f.placed} ad {f.placed === 1 ? 'break' : 'breaks'} placed
          {f.suppressed_for_brand_safety > 0 ? `, ${f.suppressed_for_brand_safety} held back for brand safety` : ''}.
        </p>
      </div>
      {trial && (
        <p className="trialbar">
          Showing re-match with a runtime brand ({trial.name}). <button onClick={showBase}>Back to original catalogue</button>
        </p>
      )}
      <Player
        ref={player}
        src={`/media/episodes/${id}.mp4`}
        slots={slots}
        duration={res.media.duration}
        sceneStarts={(res.scenes ?? []).slice(1).map((s) => s.start)}
        suppressed={(res.unplaced ?? []).map((u) => u.t)}
      />
      <p className="legend">
        <i className="lg-break" /> ad break, click to watch <i className="lg-held" /> held back for brand safety <i className="lg-scene" /> scenes
      </p>

      <section>
        <h2>How the breaks were found</h2>
        {res.effective_pacing && (
          <p className="note">
            For this episode: no break in the first {Math.round(res.effective_pacing.head_margin_sec)} s or last {Math.round(res.effective_pacing.tail_margin_sec)} s, at least{' '}
            {Math.round(res.effective_pacing.min_gap_sec)} s between breaks, at most {res.effective_pacing.break_budget}.
          </p>
        )}
        <div className="funnel">
          {[
            ['camera cuts', f.shots, ''],
            ['clear of speech in the transcript', f.pass_hard_filters, ''],
            ['rejected by the AI listening to the audio', f.caught_by_ai_audio_check, 'catch'],
            ['safe breaks considered', f.considered_for_placement, ''],
            ['placed with a brand', f.placed, 'onair'],
            ['held back for brand safety', f.suppressed_for_brand_safety, 'held'],
          ].map(([label, n, tone]) => (
            <div key={label as string} className={tone as string}>
              <b>{n}</b>
              <span>{label}</span>
            </div>
          ))}
        </div>
      </section>

      <section>
        <h2>Ad breaks</h2>
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
              <span className="muted">
                {b.creative?.duration_sec} s creative, break score {b.score.toFixed(2)}, brand fit {b.placement.decision.fit?.toFixed(2)}
              </span>
              <button onClick={() => player.current?.seek(Math.max(0, b.t - 6))}>Watch this break</button>
            </div>
            <dl className="why">
              <div><dt>Why here</dt><dd>{b.rationale}</dd></div>
              {b.boundary && <div><dt>Scene</dt><dd>{b.boundary.note}</dd></div>}
              <div><dt>Why {b.brand_name}</dt><dd>{b.placement.decision.rationale}</dd></div>
            </dl>
            <div className="scenes">
              <div>
                <small>Before the break</small>
                <p>{b.placement.scene_before.description}</p>
                <small className="muted">{b.placement.scene_before.contexts.join(', ')}</small>
              </div>
              <div>
                <small>After the break</small>
                <p>{b.placement.scene_after.description}</p>
                <small className="muted">{b.placement.scene_after.contexts.join(', ')}</small>
              </div>
            </div>
            {b.safety_checks && (
              <div className="safety">
                <small>Second, independent check from 30 s before to 40 s after the break</small>
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
                <small>Brands blocked</small>
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
          <h2>Held back for brand safety</h2>
          <p className="note">Clean pauses where no brand was allowed. A wrongly placed ad is a violation; a skipped break costs one impression.</p>
          {(res.unplaced ?? []).map((u) => {
            const reasons = Object.values(u.decision.blocked)
            const top = reasons.find((r) => r.includes('independent')) ?? reasons[0] ?? 'no brand fit this moment'
            return (
              <div key={u.t} className="break suppressed">
                <div className="row">
                  <b>{fmt(u.t)}</b>
                  <span className="muted">{Object.keys(u.decision.blocked).length} of {Object.keys(u.decision.blocked).length + u.decision.unblocked.length} brands blocked</span>
                  <button onClick={() => player.current?.seek(Math.max(0, u.t - 6))}>Watch this moment</button>
                </div>
                <p>{u.scene_before.description}</p>
                <p className="blocked">{top}</p>
              </div>
            )
          })}
        </section>
      )}

      <section>
        <h2>Scenes</h2>
        <p className="note">Click a row to jump to it.</p>
        <table>
          <thead>
            <tr>
              <th>Starts</th>
              <th>Length</th>
              <th>Activity</th>
              <th>What happens</th>
              <th>Context</th>
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
        <h2>Every cut the AI reviewed</h2>
        <p className="note">Cuts the transcript called silent, with the AI verdict after listening. Greyed rows were rejected.</p>
        <table>
          <thead>
            <tr>
              <th>Time</th>
              <th>Score</th>
              <th>Verdict</th>
              <th>Reason</th>
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
        <a href={`/api/episodes/${id}/vmap.xml${trial ? `?trial=${trial.name}` : ''}`} target="_blank">Download the VMAP manifest</a>
        <a href={`/api/episodes/${id}`} target="_blank">Open the debug JSON</a>
      </section>
    </main>
  )
}
