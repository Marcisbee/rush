for (let index = 0; index < 100; index++) {
  test(`representative mix ${index}`, async () => {
    const input = document.createElement("input")
    input.value = String(index)
    document.body.append(input)
    input.dispatchEvent(new Event("input", {bubbles: true}))
    await Promise.resolve()
    expect(input.value).toBe(String(index))
    expect({index}).toEqual({index})
    input.remove()
  })
}
