import {h, render} from "preact"

function Counter({value}: {value: number}) {
  return <button aria-label="counter">{value}</button>
}

for (let index = 0; index < 1_000; index++) {
  test(`Preact component ${index}`, () => {
    const root = document.createElement("div")
    document.body.append(root)
    render(<Counter value={index}/>, root)
    expect(root.querySelector("button")?.textContent).toBe(String(index))
    render(null, root)
    root.remove()
  })
}
