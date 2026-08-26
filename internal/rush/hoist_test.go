package rush

import (
	"strings"
	"testing"
)

func TestTransformHoistedMocksRegistersBeforeDelayedImports(t *testing.T) {
	source := `import { test, vi } from "rush-webtest";
import { read as readValue } from "./service.js";
vi.mock("./service.js", () => ({ read: () => "mocked" }));
test("mocked", () => readValue());`

	transformed, err := transformHoistedMocks(source)
	if err != nil {
		t.Fatal(err)
	}
	registration := strings.Index(transformed, `__rushRegisterMock__("./service.js"`)
	delayedImport := strings.Index(transformed, `await __rushImport__("./service.js"`)
	if registration == -1 || delayedImport == -1 || registration > delayedImport {
		t.Fatalf("mock was not registered before the delayed import:\n%s", transformed)
	}
	if !strings.Contains(transformed, "globalThis.__rushRegistration = (async () =>") {
		t.Fatalf("registration promise missing:\n%s", transformed)
	}
}

func TestTransformHoistedMocksLeavesOrdinarySuiteUntouched(t *testing.T) {
	source := `import { test } from "rush-webtest"; test("plain", () => {});`
	transformed, err := transformHoistedMocks(source)
	if err != nil {
		t.Fatal(err)
	}
	if transformed != source {
		t.Fatalf("ordinary suite changed:\n%s", transformed)
	}
}
