import { beforeEach, describe, expect, screen, test } from "rush-webtest"

describe("Rush browser runtime", () => {
  beforeEach(() => {
    document.body.innerHTML = `<main><button>Save</button></main>`
  })

  test("executes TypeScript in the page", () => {
    const button = screen.getByRole("button", {name: "Save"})
    expect(button.textContent).toBe("Save")
  })

  test("supports asynchronous tests", async () => {
    await new Promise(resolve => setTimeout(resolve, 5))
    expect(document.querySelector("main")).toBeTruthy()
  })
})
