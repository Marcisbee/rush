import { defineConfig } from "vitest/config";

export default defineConfig({
  test: {
    environment: "jsdom",
    // Rush cannot reset the registry or replace the adapter that is currently
    // executing a suite, so Vitest is limited to that bootstrap seam.
    include: ["test/bootstrap.test.ts"],
    restoreMocks: true,
  },
});
