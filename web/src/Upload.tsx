import { useState } from 'react'

type Job = { id: string; episode: string; status: string; stage: string; log: string[] | null; error?: string }

// Same three steps as production: get an upload URL, PUT the bytes there, start a job.
export default function Upload({ done }: { done: (episode: string) => void }) {
  const [job, setJob] = useState<Job | null>(null)
  const [msg, setMsg] = useState('')

  async function send(file: File) {
    try {
      setJob(null)
      setMsg('requesting upload URL…')
      const up = await (await fetch('/api/uploads', { method: 'POST' })).json()
      setMsg(`uploading ${(file.size / 1e6).toFixed(0)} MB…`)
      const put = await fetch(up.upload_url, { method: 'PUT', body: file })
      const putBody = await put.json()
      if (!put.ok) throw new Error(putBody.error)
      setMsg('starting job…')
      const jr = await fetch('/api/jobs', { method: 'POST', body: JSON.stringify({ episode: up.id }) })
      let j: Job = await jr.json()
      if (!jr.ok) throw new Error((j as unknown as { error: string }).error)
      setMsg('')
      while (j.status === 'queued' || j.status === 'running') {
        setJob(j)
        await new Promise((r) => setTimeout(r, 1500))
        j = await (await fetch(`/api/jobs/${j.id}`)).json()
      }
      setJob(j)
      if (j.status === 'done') done(j.episode)
    } catch (e) {
      setMsg(`error: ${(e as Error).message}`)
    }
  }

  return (
    <section className="upload">
      <h3>Analyse a new video</h3>
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
