import { expect, native, test, waitFor } from "@rush/browser"

const appOrigin = "http://127.0.0.1:45678"

test.app("preserves the application path, query, and fragment", async ({goto, network, window}) => {
  network.route(`${appOrigin}/login?next=%2Faccount`, route => route.fulfill({
    headers: {"Content-Type": "text/html; charset=utf-8"},
    body: `<p>login</p>`,
  }))
  network.route(`${appOrigin}/`, route => route.fulfill({
    headers: {"Content-Type": "text/html; charset=utf-8"},
    body: `<p>home</p>`,
  }))

  await goto(`${appOrigin}/login?next=%2Faccount#form`)
  expect(window.location.pathname).toBe("/login")
  expect(window.location.search).toBe("?next=%2Faccount")
  expect(window.location.hash).toBe("#form")

  await goto(`${appOrigin}/`)
  expect(window.location.pathname).toBe("/")
})

test.app("navigates, intercepts requests, and uses trusted input", async ({goto, network, page}) => {
  network.route(`${appOrigin}/account`, route => route.fulfill({
    headers: {"Content-Type": "text/html; charset=utf-8"},
    body: `
      <label>Name <input aria-label="Name"></label>
      <button type="button">Save</button>
      <output>loading</output>
      <script>
        const input = document.querySelector("input")
        input.addEventListener("input", event => input.dataset.trusted = String(event.isTrusted))
        input.addEventListener("keydown", event => {
          if (event.key === "Enter") input.dataset.keyTrusted = String(event.isTrusted)
        })
        const button = document.querySelector("button")
        button.addEventListener("click", event => button.dataset.trusted = String(event.isTrusted))
        fetch("/api/user").then(response => response.json()).then(user => {
          document.querySelector("output").textContent = user.name
        })
      </script>
    `,
  }))
  network.route("**/api/user", route => route.fulfill({
    headers: {"Content-Type": "application/json"},
    body: JSON.stringify({name: "Ada"}),
  }))

  const apiRequest = network.waitForRequest(`${appOrigin}/api/user`)
  await goto(`${appOrigin}/account`)
  expect((await apiRequest).method).toBe("GET")
  await page.findByText("Ada")

  const input = page.getByRole("textbox", {name: "Name"})
  input.fill("synthetic")
  expect(input.getAttribute("data-trusted")).toBe("false")
  input.fill("")
  await native.type(input, "Ada")
  await waitFor(() => expect(input.getAttribute("data-trusted")).toBe("true"))
  expect((input.element() as HTMLInputElement).value).toBe("Ada")
  await native.press("Enter", input)
  await waitFor(() => expect(input.getAttribute("data-key-trusted")).toBe("true"))
  const button = page.getByRole("button", {name: "Save"})
  await native.click(button)
  await waitFor(() => expect(button.getAttribute("data-trusted")).toBe("true"))
  expect(network.requests().map(request => request.url)).toEqual([
    `${appOrigin}/account`,
    `${appOrigin}/api/user`,
  ])
})

test.app("starts with isolated origin storage", async ({goto, network, window}) => {
  network.route(`${appOrigin}/storage`, route => route.fulfill({
    headers: {"Content-Type": "text/html; charset=utf-8"},
    body: `<p>storage</p>`,
  }))
  await goto(`${appOrigin}/storage`)
  expect(window.localStorage.getItem("previous-test")).toBeNull()
  window.localStorage.setItem("previous-test", "must be cleared")
})

test.app("does not inherit storage from the previous app test", async ({goto, network, window}) => {
  network.route(`${appOrigin}/storage`, route => route.fulfill({
    headers: {"Content-Type": "text/html; charset=utf-8"},
    body: `<p>storage</p>`,
  }))
  await goto(`${appOrigin}/storage`)
  expect(window.localStorage.getItem("previous-test")).toBeNull()
})
