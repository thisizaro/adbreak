import { useState } from 'react'

type Job = { id: string; episode: string; status: string; stage: string; log: string[] | null; error?: string }

// Same three steps as production: get an upload URL, PUT the bytes there, start a job.
export default function Upload({ done }: { done: (episode: string) => void }) {
  const [job, setJob] = useState<Job | null>(null)
  const [msg, setMsg] = useState('')

  async function send(file: File) {
    try {
      setJob(null)
      if (file.size > 700 * 1024 * 1024) throw new Error(`file is ${Math.round(file.size / 1048576)} MB, limit is 700 MB`)
      setMsg('reading the video…')
      const secs = await localDuration(file)
      if (secs > 3600) throw new Error(`video is ${Math.round(secs / 60)} minutes long, limit is 60 minutes`)
      setMsg('requesting upload URL…')
      const ur = await fetch('/api/uploads', { method: 'POST', body: JSON.stringify({ size: file.size }) })
      const up = await ur.json()
      if (!ur.ok) throw new Error(up.error)
      setMsg(`uploading ${(file.size / 1e6).toFixed(0)} MB…`)
      // Locally this is our own endpoint; on Cloud Run it is a GCS resumable session URL.
      const put = await fetch(up.upload_url, { method: 'PUT', body: file, headers: { 'Content-Type': 'video/mp4' } })
      if (!put.ok) throw new Error(`upload failed: HTTP ${put.status}`)
      setMsg('checking the video…')
      const fin = await fetch(`/api/uploads/${up.id}/finalize`, { method: 'POST' })
      const finBody = await fin.json()
      if (!fin.ok) throw new Error(finBody.error)
      setMsg('starting job…')
      const jr = await fetch('/api/jobs', { method: 'POST', body: JSON.stringify({ episode: up.id }) })
      let j: Job = await jr.json()
      if (!jr.ok) throw new Error((j as unknown as { error: string }).error)
      setMsg('')
      while (j.status === 'queued' || j.status === 'running') {
        setJob(j)
        await new Promise((r) => setTimeout(r, 1500))
        const pr = await fetch(`/api/jobs/${j.id}`)
        if (pr.status === 404) throw new Error('job lost: the server restarted while processing. Please upload again.')
        j = await pr.json()
      }
      setJob(j)
      if (j.status === 'done') done(j.episode)
      else if (j.error) throw new Error(j.error)
    } catch (e) {
      setMsg(`error: ${(e as Error).message}`)
    }
  }

  return (
    <section className="upload">
      <h3>Analyse a new video</h3>
      <p className="muted">MP4 up to 60 minutes and 700 MB. H.264 video plays in every browser. Processing a 25 minute episode takes a few minutes; you can leave this page open.</p>
      <input type="file" accept="video/mp4" onChange={(e) => e.target.files?.[0] && send(e.target.files[0])} />
      {msg && <p className="muted">{msg}</p>}
      {job && (
        <div className="joblog">
          <b>
            {job.status} {job.stage && `· ${job.stage}`}
          </b>
          {(job.log ?? []).map((l, i) => (
            <div key={i}>{l}</div>
          ))}
          {job.error && <div className="err">{job.error}</div>}
        </div>
      )}
    </section>
  )
}

// Reads the duration in the browser so an over-long file is refused before uploading.
// Returns 0 when the browser cannot read it; the server checks again either way.
function localDuration(file: File): Promise<number> {
  return new Promise((resolve) => {
    const v = document.createElement('video')
    const url = URL.createObjectURL(file)
    const done = (d: number) => {
      URL.revokeObjectURL(url)
      resolve(d)
    }
    v.preload = 'metadata'
    v.onloadedmetadata = () => done(isFinite(v.duration) ? v.duration : 0)
    v.onerror = () => done(0)
    setTimeout(() => done(0), 8000)
    v.src = url
  })
}
