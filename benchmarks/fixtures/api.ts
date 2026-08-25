import { beforeEach, describe, expect, test } from "@rush/browser"

let hookRuns = 0

beforeEach(() => {
  hookRuns++
})

describe("registration API", () => {
  test.each([
    [1, 2, 3],
    [2, 3, 5],
  ])("adds %i and %i", (left, right, sum) => {
    expect(left + right).toBe(sum)
  })

  test("runs hooks", () => {
    expect(hookRuns).toBe(3)
  })

  test.skip("can skip", () => {
    throw new Error("must not run")
  })

  test.todo("records todo work")
})
