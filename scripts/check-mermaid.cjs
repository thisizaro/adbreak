// Renders every mermaid block in docs/ARCHITECTURE.md with mermaid v11. Extract blocks to blocks.json first (see LOG).
const { chromium } = require('playwright')
const blocks = require('./blocks.json')
;(async () => {
  const b = await chromium.launch()
  const p = await b.newPage()
  await p.setContent('<html><body></body></html>')
  await p.addScriptTag({ url: 'https://cdn.jsdelivr.net/npm/mermaid@11/dist/mermaid.min.js' })
  const res = await p.evaluate(async (blocks) => {
    mermaid.initialize({ startOnLoad: false })
    const out = []
    for (let i = 0; i < blocks.length; i++) {
      try { await mermaid.render('d' + i, blocks[i]); out.push(`diagram ${i + 1}: OK`) }
      catch (e) { out.push(`diagram ${i + 1}: FAIL ${String(e.message || e).slice(0, 300)}`) }
    }
    return out
  }, blocks)
  console.log(res.join('\n'))
  await b.close()
})()
