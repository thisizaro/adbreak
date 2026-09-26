import { useEffect, useState } from 'react'

type Health = { status: string; version: string }

export default function App() {
  const [health, setHealth] = useState<Health | null>(null)
  const [err, setErr] = useState<string | null>(null)

  useEffect(() => {
    fetch('/api/health')
      .then((r) => (r.ok ? r.json() : Promise.reject(new Error(`HTTP ${r.status}`))))
      .then(setHealth)
      .catch((e: Error) => setErr(e.message))
  }, [])

  return (
    <main style={{ padding: 24 }}>
      <h1>adbreak</h1>
      {health && <p>API {health.status}, version {health.version}</p>}
      {err && <p style={{ color: '#f66' }}>API unreachable: {err}</p>}
    </main>
  )
}
