import { forwardRef, useEffect, useImperativeHandle, useRef, useState } from 'react'
import { fmt, type AdSlot } from './api'
import Strip from './Strip'

export type PlayerHandle = { seek: (t: number) => void }

type Props = { src: string; slots: AdSlot[]; duration: number; sceneStarts: number[]; suppressed: number[] }

// Plays the episode and, when playback crosses a VMAP break, pauses content,
// plays the ad creative, then resumes content at the break point.
const Player = forwardRef<PlayerHandle, Props>(function Player({ src, slots, duration, sceneStarts, suppressed }, ref) {
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

  // Preload the next ad creative so the cut is instant instead of waiting on the network.
  useEffect(() => {
    const a = ad.current
    if (active || !a) return
    const next = slots.find((s) => !played.current.has(s.breakId) && s.offset > now) ?? slots[0]
    if (next && a.getAttribute('src') !== next.media) {
      a.preload = 'auto'
      a.src = next.media
    }
  }, [slots, now, active])

  useEffect(() => {
    const a = ad.current
    if (!active || !a) return
    if (a.getAttribute('src') !== active.media) a.src = active.media
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
            <span className="onair-dot" aria-hidden="true" />
            Ad break: {active.title}, {adLeft}s left. The episode resumes at {fmt(active.offset)}.
            <button
              className="skip"
              onClick={() => {
                const a = ad.current
                if (a && isFinite(a.duration)) a.currentTime = a.duration - 0.05
              }}
            >
              Skip (demo)
            </button>
          </div>
        )}
      </div>
      <div className="track">
        <div className="progress" style={{ width: `${(now / duration) * 100}%` }} />
        <Strip
          size="full"
          duration={duration}
          scenes={sceneStarts}
          suppressed={suppressed}
          breaks={slots.map((s) => s.offset)}
          onPick={(t) => {
            for (const x of slots) if (x.offset >= t) played.current.delete(x.breakId)
            lastT.current = Math.max(0, t - 6)
            if (content.current) {
              content.current.currentTime = lastT.current
              content.current.play().catch(() => {})
            }
          }}
        />
      </div>
    </div>
  )
})

export default Player
