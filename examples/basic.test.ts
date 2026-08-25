describe("Rush browser runtime", () => {
  beforeEach(() => {
    document.body.innerHTML = `<main><button>Save</button></main>`
  })

  test("executes TypeScript in the page", () => {
    const button: HTMLButtonElement | null = document.querySelector("button")
    expect(button?.textContent).toBe("Save")
  })

  test("supports asynchronous tests", async () => {
    await new Promise(resolve => setTimeout(resolve, 5))
    expect(document.querySelector("main")).toBeTruthy()
  })
})
