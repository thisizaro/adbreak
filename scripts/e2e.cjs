// Browser check of the ad cut: node scripts/e2e.cjs (needs 'npm i playwright' and a running server on :8080).
const { chromium } = require('playwright')
;(async () => {
  const b = await chromium.launch({ args: ['--autoplay-policy=no-user-gesture-required'] })
  const p = await b.newPage({ viewport: { width: 1280, height: 900 } })
  p.on('console', (m) => m.type() === 'error' && console.log('console error:', m.text()))
  await p.goto('http://localhost:8080/')
  await p.waitForSelector('.card')
  await p.screenshot({ path: '../shot_library.png' })
  console.log('h264 support:', await p.evaluate(() => document.createElement('video').canPlayType('video/mp4; codecs="avc1.42E01E, mp4a.40.2"')))
  await p.click('.card')
  await p.waitForSelector('.break')
  await p.screenshot({ path: '../shot_episode.png', fullPage: true })
  await p.click('.break button')
  await p.waitForTimeout(9000)
  const st = await p.evaluate(() => {
    const [c, a] = document.querySelectorAll('.screen video')
    return { contentT: c.currentTime, contentPaused: c.paused, adOn: a.classList.contains('on'), adT: a.currentTime, badge: document.querySelector('.adbadge')?.textContent }
  })
  console.log('after jump + 9s:', JSON.stringify(st))
  await p.screenshot({ path: '../shot_ad.png' })
  await p.evaluate(() => { const a = document.querySelectorAll('.screen video')[1]; a.currentTime = a.duration - 0.3 })
  await p.waitForTimeout(3000)
  const st2 = await p.evaluate(() => {
    const [c, a] = document.querySelectorAll('.screen video')
    return { contentT: c.currentTime, contentPaused: c.paused, adOn: a.classList.contains('on') }
  })
  console.log('after ad end:', JSON.stringify(st2))
  await b.close()
})()
