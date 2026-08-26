import { expect, test } from "rush-webtest"

for (let index = 0; index < 1_000; index++) {
  test(`assertion ${index}`, () => expect(index + 1).toBe(index + 1))
}
