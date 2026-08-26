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
	if strings.Contains(transformed, "globalThis.__rushRegistration") {
		t.Fatalf("source transform unexpectedly owns bundle registration:\n%s", transformed)
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

func TestTransformHoistedMocksInitializesHoistedStateBeforeMocksAndImports(t *testing.T) {
	source := `import { test, vi } from "rush-webtest";
import { read } from "./service.js";
const { state } = vi.hoisted(() => ({ state: { read: vi.fn(() => "mocked") } }));
vi.mock("./service.js", () => state);
test("mocked", () => read());`

	transformed, err := transformHoistedMocks(source)
	if err != nil {
		t.Fatal(err)
	}
	hoisted := strings.Index(transformed, `const { state } = __rushVi.hoisted`)
	registration := strings.Index(transformed, `__rushRegisterMock__("./service.js", () => state)`)
	delayedImport := strings.Index(transformed, `await __rushImport__("./service.js"`)
	if hoisted == -1 || registration == -1 || delayedImport == -1 || hoisted > registration || registration > delayedImport {
		t.Fatalf("hoisted state, mock, and import are out of order:\n%s", transformed)
	}
	if strings.Contains(transformed, `const { state } = vi.hoisted`) {
		t.Fatalf("hoisted state still depends on the delayed vi import:\n%s", transformed)
	}
}
