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
      <form>
        <label>Email <input aria-label="Email"></label>
        <button type="submit">Save</button>
        <button type="button" disabled>Unavailable</button>
      </form>
      <nav aria-label="Targets">
        <button type="button" data-target="left">Left</button><button type="button" data-target="middle">Middle</button><button type="button" data-target="right">Right</button>
      </nav>
      <output data-clicks aria-label="Click results"></output>
      <output>loading</output>
      <script>
        const input = document.querySelector("input")
        input.addEventListener("input", event => input.dataset.trusted = String(event.isTrusted))
        input.addEventListener("keydown", event => {
          if (event.key === "Enter") input.dataset.keyTrusted = String(event.isTrusted)
        })
        const form = document.querySelector("form")
        form.addEventListener("submit", event => {
          event.preventDefault()
          form.dataset.trusted = String(event.isTrusted)
          fetch("/api/save", {method: "POST", body: input.value})
        })
        const clicks = document.querySelector("[data-clicks]")
        document.querySelectorAll("[data-target]").forEach(button => button.addEventListener("click", event => {
          clicks.textContent += event.currentTarget.dataset.target + ":" + event.isTrusted + ","
        }))
        fetch("/api/user").then(response => response.json()).then(user => {
          document.querySelectorAll("output")[1].textContent = user.name
        })
      </script>
    `,
  }))
  network.route("**/api/user", route => route.fulfill({
    headers: {"Content-Type": "application/json"},
    body: JSON.stringify({name: "Ada"}),
  }))
  network.route("**/api/save", route => route.fulfill({status: 204}))

  const apiRequest = network.waitForRequest(`${appOrigin}/api/user`)
  await goto(`${appOrigin}/account`)
  expect((await apiRequest).method).toBe("GET")
  const user = await page.findByText("Ada")

  const input = page.getByRole("textbox", {name: "Email"})
  expect(user.element()).toBeInTheDocument()
  expect(user.element()).toHaveTextContent("Ada")
  expect(input.element()).toHaveAttribute("aria-label", "Email")
  expect(input.element()).toBeVisible()
  expect(page.getByRole("button", {name: "Unavailable"}).element()).toBeDisabled()
  input.fill("synthetic")
  expect(input.element()).toHaveValue("synthetic")
  expect(input.getAttribute("data-trusted")).toBe("false")
  input.fill("")
  await native.type(input, "ada@example.test")
  await waitFor(() => expect(input.getAttribute("data-trusted")).toBe("true"))
  expect((input.element() as HTMLInputElement).value).toBe("ada@example.test")
  await native.press("Enter", input)
  await waitFor(() => expect(input.getAttribute("data-key-trusted")).toBe("true"))

  const expectedTargets = ["left", "middle", "right", "middle", "left", "right", "left", "middle"]
  for (const target of expectedTargets) {
    await native.click(page.getByRole("button", {name: target[0].toUpperCase() + target.slice(1)}))
  }
  expect(page.getByRole("status", {name: "Click results"}).textContent()).toBe(expectedTargets.map(target => `${target}:true,`).join(""))

  const saved = network.waitForRequest(`${appOrigin}/api/save`)
  await native.click(page.getByRole("button", {name: "Save"}))
  expect((await saved).body).toBe("ada@example.test")
  expect(network.requests().map(request => request.url)).toEqual([
    `${appOrigin}/account`,
    `${appOrigin}/api/user`,
    `${appOrigin}/api/save`,
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
