import { expect, test } from "@rush/browser"

test("receives a reset browser", () => {
  window.dispatchEvent(new Event("rush-leak"))
  expect(document.body.textContent).toBe("")
  expect(localStorage.getItem("rush-leak")).toBeNull()
  expect((globalThis as any).__rushLeaked).toBe(undefined)
  expect((globalThis as any).__rushListenerFired).toBe(undefined)
  expect((globalThis as any).__rushTimerFired).toBe(undefined)
})
