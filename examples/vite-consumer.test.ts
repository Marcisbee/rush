import message from "./consumer-message.md";
import { registerSW } from "virtual:pwa-register";
import { expect, test } from "rush-webtest";

test("uses consumer-configured loaders and virtual-module aliases", () => {
  expect(message.trim()).toBe("consumer loader value");
  expect(registerSW()).toBe("consumer alias value");
});
