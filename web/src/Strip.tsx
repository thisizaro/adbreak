import { fmt } from './api'

type Props = {
  duration: number
  breaks: number[]
  suppressed: number[]
  scenes?: number[]
  onPick?: (t: number) => void
  size?: 'mini' | 'full'
}

// The ad schedule for one episode: runtime as a bar, scene bands, red ticks for
// placed breaks and amber ticks for breaks suppressed for brand safety.
export default function Strip({ duration, breaks, suppressed, scenes = [], onPick, size = 'mini' }: Props) {
  const pct = (t: number) => `${(t / duration) * 100}%`
  const bands = [0, ...scenes, duration]
  return (
    <div className={`strip ${size}`} role="img" aria-label={`${breaks.length} ad breaks, ${suppressed.length} suppressed`}>
      {bands.slice(0, -1).map((start, i) => (
        <span key={start} className={i % 2 ? 'band alt' : 'band'} style={{ left: pct(start), width: pct(bands[i + 1] - start) }} />
      ))}
      {suppressed.map((t) => (
        <span key={`s${t}`} className="tick held" style={{ left: pct(t) }} title={`Suppressed for brand safety at ${fmt(t)}`} />
      ))}
      {breaks.map((t) =>
        onPick ? (
          <button key={`b${t}`} className="tick onair marker" style={{ left: pct(t) }} onClick={() => onPick(t)} aria-label={`Watch the ad break at ${fmt(t)}`} title={`Ad break at ${fmt(t)}: watch from 6 s before`}>
            <span className="tc">{fmt(t)}</span>
          </button>
        ) : (
          <span key={`b${t}`} className="tick onair" style={{ left: pct(t) }} />
        ),
      )}
    </div>
  )
}
