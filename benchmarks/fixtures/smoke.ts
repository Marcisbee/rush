import { expect, test } from "rush-webtest"

test("WebKitGTK smoke", () => {
  const element = document.createElement("button")
  element.textContent = "ready"
  document.body.append(element)
  expect(document.querySelector("button")?.textContent).toBe("ready")
})
