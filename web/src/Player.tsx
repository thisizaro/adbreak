import { forwardRef, useEffect, useImperativeHandle, useRef, useState } from 'react'
import { fmt, type AdSlot } from './api'

export type PlayerHandle = { seek: (t: number) => void }

type Props = { src: string; slots: AdSlot[]; duration: number }

// Plays the episode and, when playback crosses a VMAP break, pauses content,
// plays the ad creative, then resumes content at the break point.
const Player = forwardRef<PlayerHandle, Props>(function Player({ src, slots, duration }, ref) {
  const content = useRef<HTMLVideoElement>(null)
  const ad = useRef<HTMLVideoElement>(null)
  const played = useRef<Set<string>>(new Set())
  const lastT = useRef(0)
  const [active, setActive] = useState<AdSlot | null>(null)
  const [now, setNow] = useState(0)
  const [adLeft, setAdLeft] = useState(0)

  useImperativeHandle(ref, () => ({
    seek(t: number) {
      const v = content.current
      if (!v) return
      // Seeking before a break re-arms it so it fires again.
      for (const s of slots) if (s.offset > t) played.current.delete(s.breakId)
      lastT.current = t
      v.currentTime = t
      v.play().catch(() => {})
    },
  }))

  useEffect(() => {
    const v = content.current
    if (!v) return
    const onTime = () => {
      const t = v.currentTime
      setNow(t)
      const prev = lastT.current
      lastT.current = t
      if (active || t < prev || t - prev > 2) return // ignore seeks
      const due = slots.find((s) => !played.current.has(s.breakId) && prev < s.offset && t >= s.offset)
      if (!due) return
      played.current.add(due.breakId)
      v.pause()
      v.currentTime = due.offset
      setActive(due)
    }
    v.addEventListener('timeupdate', onTime)
    return () => v.removeEventListener('timeupdate', onTime)
  }, [slots, active])

  useEffect(() => {
    const a = ad.current
    if (!active || !a) return
    a.src = active.media
    a.currentTime = 0
    a.play().catch(() => {})
    const tick = () => setAdLeft(Math.max(0, Math.ceil(active.duration - a.currentTime)))
    const done = () => {
      setActive(null)
      content.current?.play().catch(() => {})
    }
    a.addEventListener('timeupdate', tick)
    a.addEventListener('ended', done)
    return () => {
      a.removeEventListener('timeupdate', tick)
      a.removeEventListener('ended', done)
    }
  }, [active])

  return (
    <div className="player">
      <div className="screen">
        <video ref={content} src={src} controls={!active} preload="metadata" />
        <video ref={ad} className={active ? 'ad on' : 'ad'} playsInline />
        {active && (
          <div className="adbadge">
            Ad: {active.title} · {adLeft}s · resumes at {fmt(active.offset)}
          </div>
        )}
      </div>
      <div className="track">
        <div className="progress" style={{ width: `${(now / duration) * 100}%` }} />
        {slots.map((s) => (
          <div key={s.breakId} className="marker" style={{ left: `${(s.offset / duration) * 100}%` }} title={`${s.title} at ${fmt(s.offset)}`} />
        ))}
      </div>
    </div>
  )
})

export default Player
