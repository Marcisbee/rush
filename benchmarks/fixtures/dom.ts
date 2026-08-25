import { expect, test } from "@rush/browser"

for (let index = 0; index < 1_000; index++) {
  test(`DOM ${index}`, () => {
    const element = document.createElement("div")
    element.dataset.rush = String(index)
    document.body.append(element)
    expect(document.querySelector(`[data-rush="${index}"]`)).toBe(element)
    element.textContent = "updated"
    expect(element.textContent).toBe("updated")
    element.remove()
  })
}
