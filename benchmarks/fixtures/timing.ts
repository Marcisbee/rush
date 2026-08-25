test("attributes browser resource and timer time separately", async () => {
  const response = await fetch("/__rush/timing")
  expect(await response.text()).toBe("rush")
  await new Promise(resolve => setTimeout(resolve, 10))
})
