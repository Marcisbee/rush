import {h, render} from "preact"
import { expect, test } from "@rush/browser"

function Counter({value}: {value: number}) {
  return h("button", {"aria-label": "counter"}, value)
}

for (let index = 0; index < 1_000; index++) {
  test(`Preact component ${index}`, () => {
    const root = document.createElement("div")
    document.body.append(root)
    render(h(Counter, {value: index}), root)
    expect(root.querySelector("button")?.textContent).toBe(String(index))
    render(null, root)
    root.remove()
  })
}
