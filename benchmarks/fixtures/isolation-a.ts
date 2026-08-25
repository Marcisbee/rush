import { test } from "@rush/browser"

test("leaves browser state for the suite reset", () => {
  document.body.innerHTML = "<p>leak</p>"
  localStorage.setItem("rush-leak", "yes")
  ;(globalThis as any).__rushLeaked = true
  window.addEventListener("rush-leak", () => {
    ;(globalThis as any).__rushListenerFired = true
  })
  setTimeout(() => {
    ;(globalThis as any).__rushTimerFired = true
  }, 60_000)
})
