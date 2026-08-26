import icon from "./fixtures/rush-icon.svg";
import iconURL from "./fixtures/rush-icon.svg?url";
import "./fixtures/vite-assets.css";
import { expect, test } from "rush-webtest";

test("loads Vite-style assets and suite CSS", () => {
  document.body.innerHTML = `<div class="rush-vite-asset">asset</div>`;
  const element = document.querySelector(".rush-vite-asset");

  expect(icon.startsWith("data:image/svg+xml,")).toBe(true);
  expect(iconURL.startsWith("data:image/svg+xml,")).toBe(true);
  expect(element).toBeInTheDocument();
  expect(getComputedStyle(element!).color).toBe("rgb(12, 34, 56)");
  expect(getComputedStyle(element!).backgroundImage).toContain("data:image/svg+xml");
});
