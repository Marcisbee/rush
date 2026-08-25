import { expect, fireEvent, screen, test, vi } from "@rush/browser"
import { readStatus } from "./mock-service.js"

vi.mock("./mock-service.js", () => ({readStatus: () => "mocked"}))

test("hoists module mocks before imports", () => {
  expect(readStatus()).toBe("mocked")
})

test("runs the shared browser API", () => {
  expect((globalThis as {test?: unknown}).test).toBeUndefined()
  document.body.innerHTML = `<button type="button">Save</button>`
  const saved = vi.fn()
  const button = screen.getByRole("button", {name: "Save"})
  button.addEventListener("click", saved)

  fireEvent.click(button)

  expect(saved.mock.calls).toHaveLength(1)
})

test("runs fake timers without a native wait", () => {
  vi.useFakeTimers()
  const completed = vi.fn()
  setTimeout(completed, 10_000)

  vi.advanceTimersByTime(10_000)

  expect(completed.mock.calls).toHaveLength(1)
})
