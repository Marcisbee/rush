test("executes JavaScript in WebKitGTK", () => {
  const element = document.createElement("output")
  element.textContent = "JavaScript"
  document.body.append(element)
  expect(document.querySelector("output").textContent).toBe("JavaScript")
})
