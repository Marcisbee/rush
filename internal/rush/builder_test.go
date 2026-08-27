package rush

import (
	"encoding/base64"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestBuilderReusesContextAndSeesEdits(t *testing.T) {
	directory, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	fixtureDirectory, err := os.MkdirTemp(directory, ".rush-builder-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(fixtureDirectory) })
	file := filepath.Join(fixtureDirectory, "suite.ts")
	if err := os.WriteFile(file, []byte("import { expect, test } from 'rush-webtest'; test('one', () => expect(1).toBe(1))"), 0600); err != nil {
		t.Fatal(err)
	}
	builder := NewBuilder()
	defer builder.Close()
	first, _, err := builder.Build(directory, file)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("import { expect, test } from 'rush-webtest'; test('two', () => expect(2).toBe(2))"), 0600); err != nil {
		t.Fatal(err)
	}
	second, _, err := builder.Build(directory, file)
	if err != nil {
		t.Fatal(err)
	}
	if first == second || !strings.Contains(second, "two") {
		t.Fatalf("incremental rebuild did not include edit")
	}
}

func TestBuilderBatchesSuitesAndCachesUnchangedDependencyGraph(t *testing.T) {
	rushRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("RUSH_BROWSER_MODULE", filepath.Join(rushRoot, "dist", "index.js"))
	directory := t.TempDir()
	dependency := filepath.Join(directory, "value.ts")
	first := filepath.Join(directory, "first.test.ts")
	second := filepath.Join(directory, "second.test.ts")
	if err := os.WriteFile(dependency, []byte("export const value = 'before'"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(first, []byte("import { value } from './value'; import { test } from 'rush-webtest'; test(value, () => {})"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second, []byte("import { test } from 'rush-webtest'; test('second', () => {})"), 0600); err != nil {
		t.Fatal(err)
	}

	builder := NewBuilder()
	defer builder.Close()
	initial, initialMS, err := builder.BuildBatch(directory, []string{first, second})
	if err != nil {
		t.Fatal(err)
	}
	if len(initial) != 2 || initialMS <= 0 || !strings.Contains(initial[0].Source, "before") || !strings.Contains(initial[1].Source, "second") {
		t.Fatalf("unexpected initial batch: count=%d build=%f", len(initial), initialMS)
	}
	watchFiles := builder.WatchFiles()
	for _, path := range []string{dependency, first, second} {
		if !slices.Contains(watchFiles, path) {
			t.Fatalf("watch files %#v do not include %s", watchFiles, path)
		}
	}
	cached, cachedMS, err := builder.BuildBatch(directory, []string{first, second})
	if err != nil {
		t.Fatal(err)
	}
	if cachedMS != 0 || cached[0].Hash != initial[0].Hash || cached[1].Hash != initial[1].Hash {
		t.Fatalf("unchanged graph was not served from cache: build=%f", cachedMS)
	}

	if err := os.WriteFile(dependency, []byte("export const value = 'after dependency edit'"), 0600); err != nil {
		t.Fatal(err)
	}
	rebuilt, rebuiltMS, err := builder.BuildBatch(directory, []string{first, second})
	if err != nil {
		t.Fatal(err)
	}
	if rebuiltMS <= 0 || rebuilt[0].Hash == initial[0].Hash || !strings.Contains(rebuilt[0].Source, "after dependency edit") {
		t.Fatalf("dependency edit did not invalidate the batch: build=%f", rebuiltMS)
	}
}

func TestBuilderAppliesAsyncMocksToDependenciesImportedBySubject(t *testing.T) {
	t.Setenv("RUSH_BROWSER_MODULE", "")
	directory := t.TempDir()
	linkedParent := t.TempDir()
	linkedDirectory := filepath.Join(linkedParent, "project")
	if err := os.Symlink(directory, linkedDirectory); err != nil {
		t.Fatal(err)
	}
	service := filepath.Join(directory, "service.ts")
	subjectDirectory := filepath.Join(directory, "subject")
	if err := os.Mkdir(subjectDirectory, 0700); err != nil {
		t.Fatal(err)
	}
	subject := filepath.Join(subjectDirectory, "index.ts")
	suite := filepath.Join(directory, "subject.test.ts")
	if err := os.WriteFile(service, []byte(`export const read = () => "real";`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(subject, []byte(`import { read } from "../service"; export const load = () => read();`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(suite, []byte(`
import { vi } from "rush-webtest";
import { load } from "./subject/index";
vi.mock("./service", async (importOriginal) => {
  const actual = await importOriginal();
  return { ...actual, read: () => "mocked:" + actual.read() };
});
globalThis.__rushDependencyMockResult = load();
`), 0600); err != nil {
		t.Fatal(err)
	}

	builder := NewBuilder()
	defer builder.Close()
	bundle, _, err := builder.Build(linkedDirectory, filepath.Join(linkedDirectory, filepath.Base(suite)))
	if err != nil {
		t.Fatal(err)
	}
	runtime := `
const mocks = new Map();
globalThis.__rushBrowserRuntime = {
  vi: {},
  __rushRegisterMock__(id, factory) { mocks.set(id, factory); },
  async __rushImport__(id, importer) {
    const factory = mocks.get(id);
    return factory ? factory(importer) : importer();
  },
};
`
	script := filepath.Join(directory, "execute.mjs")
	if err := os.WriteFile(script, []byte(runtime+bundle+`
await globalThis.__rushRegistration;
if (globalThis.__rushDependencyMockResult !== "mocked:real") {
  throw new Error("dependency mock result: " + globalThis.__rushDependencyMockResult);
}
`), 0600); err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command("node", script).CombinedOutput()
	if err != nil {
		t.Fatalf("execute bundle: %v\n%s", err, output)
	}
}

func TestBuilderInitializesHoistedStateForAliasedDependencyMocks(t *testing.T) {
	t.Setenv("RUSH_BROWSER_MODULE", "")
	directory := t.TempDir()
	service := filepath.Join(directory, "service.ts")
	subject := filepath.Join(directory, "subject.ts")
	suite := filepath.Join(directory, "subject.test.ts")
	if err := os.WriteFile(service, []byte(`
export function read() {
  if (import.meta.hot) return "hot";
  return "real";
}
`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(subject, []byte(`
import { read } from "virtual:service";
export const load = () => read();
`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(suite, []byte(`
import { vi } from "rush-webtest";
import { load } from "./subject";
const state = vi.hoisted(() => ({ value: "mocked", read: vi.fn(() => "mocked") }));
vi.mock("virtual:service", () => state);
globalThis.__rushHoistedMockResult = load();
`), 0600); err != nil {
		t.Fatal(err)
	}

	builder := NewBuilder(BuilderOptions{Aliases: map[string]string{"virtual:service": service}})
	defer builder.Close()
	bundle, _, err := builder.Build(directory, suite)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(bundle, "import.meta.hot") {
		t.Fatal("bundle retained Vite HMR syntax that script execution cannot parse")
	}
	runtime := `
const mocks = new Map();
globalThis.__rushBrowserRuntime = {
  vi: {
    fn(implementation) { return implementation; },
    hoisted(factory) { return factory(); },
  },
  __rushRegisterMock__(id, factory) { mocks.set(id, factory); },
  async __rushImport__(id, importer) {
    const factory = mocks.get(id);
    return factory ? factory(importer) : importer();
  },
};
`
	script := filepath.Join(directory, "execute.mjs")
	if err := os.WriteFile(script, []byte(runtime+bundle+`
await globalThis.__rushRegistration;
if (globalThis.__rushHoistedMockResult !== "mocked") {
  throw new Error("hoisted mock result: " + globalThis.__rushHoistedMockResult);
}
`), 0600); err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command("node", script).CombinedOutput()
	if err != nil {
		t.Fatalf("execute bundle: %v\n%s", err, output)
	}
}

func TestBuilderLoadsStaticImportActualInsideMockFactory(t *testing.T) {
	t.Setenv("RUSH_BROWSER_MODULE", "")
	directory := t.TempDir()
	service := filepath.Join(directory, "avatarCrop.ts")
	suite := filepath.Join(directory, "avatarCrop.test.ts")
	if err := os.WriteFile(service, []byte(`
export const crop = () => "real";
export const format = "png";
`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(suite, []byte(`
import { vi } from "rush-webtest";
import { crop, format } from "./avatarCrop";
vi.mock("./avatarCrop", async () => {
  const actual = await vi.importActual<typeof import("./avatarCrop")>("./avatarCrop");
  return { ...actual, crop: () => "mocked" };
});
globalThis.__rushImportActualResult = [crop(), format];
`), 0600); err != nil {
		t.Fatal(err)
	}

	builder := NewBuilder()
	defer builder.Close()
	bundle, _, err := builder.Build(directory, suite)
	if err != nil {
		t.Fatal(err)
	}
	runtime := `
const mocks = new Map();
globalThis.__rushBrowserRuntime = {
  vi: {
    importActual(importer) { return importer(); },
  },
  __rushRegisterMock__(id, factory) { mocks.set(id, factory); },
  async __rushImport__(id, importer) {
    const factory = mocks.get(id);
    return factory ? factory(importer) : importer();
  },
};
`
	script := filepath.Join(directory, "execute.mjs")
	if err := os.WriteFile(script, []byte(runtime+bundle+`
await globalThis.__rushRegistration;
if (JSON.stringify(globalThis.__rushImportActualResult) !== '["mocked","png"]') {
  throw new Error("importActual result: " + JSON.stringify(globalThis.__rushImportActualResult));
}
`), 0600); err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command("node", script).CombinedOutput()
	if err != nil {
		t.Fatalf("execute bundle: %v\n%s", err, output)
	}
}

func TestBuilderReportsTypeScriptSyntaxErrors(t *testing.T) {
	directory, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	fixtureDirectory, err := os.MkdirTemp(directory, ".rush-builder-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(fixtureDirectory) })
	bad := filepath.Join(fixtureDirectory, "bad.ts")
	if err := os.WriteFile(bad, []byte("const value: = 1"), 0600); err != nil {
		t.Fatal(err)
	}
	builder := NewBuilder()
	defer builder.Close()
	if _, _, err := builder.Build(directory, bad); err == nil {
		t.Fatal("expected syntax error")
	}
}

func TestDetectJSXImportSource(t *testing.T) {
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "package.json"), []byte(`{"dependencies":{"react":"latest"},"devDependencies":{"preact":"latest"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if got := detectJSXImportSource(directory); got != "react" {
		t.Fatalf("detected %q, want react", got)
	}
	if err := os.WriteFile(filepath.Join(directory, "package.json"), []byte(`{"devDependencies":{"preact":"latest"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if got := detectJSXImportSource(directory); got != "preact" {
		t.Fatalf("detected %q, want preact", got)
	}
}

func TestDetectJSXImportSourceOverride(t *testing.T) {
	t.Setenv("RUSH_JSX_IMPORT_SOURCE", "solid-js")
	if got := detectJSXImportSource(t.TempDir()); got != "solid-js" {
		t.Fatalf("detected %q, want solid-js", got)
	}
}

func TestBuilderResolvesBrowserModuleForExternalSuite(t *testing.T) {
	rushRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("RUSH_BROWSER_MODULE", filepath.Join(rushRoot, "dist", "index.js"))
	externalRoot := t.TempDir()
	suite := filepath.Join(externalRoot, "external.test.ts")
	if err := os.WriteFile(suite, []byte("import { expect, test } from 'rush-webtest'; test('external', () => expect(1).toBe(1))"), 0600); err != nil {
		t.Fatal(err)
	}
	builder := NewBuilder()
	defer builder.Close()
	bundle, _, err := builder.Build(externalRoot, suite)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(bundle, "external") {
		t.Fatal("external suite was not included in bundle")
	}
}

func TestBuilderKeepsSharedBrowserRuntimeOutOfSuiteBundles(t *testing.T) {
	t.Setenv("RUSH_BROWSER_MODULE", "")
	directory := t.TempDir()
	suite := filepath.Join(directory, "small.test.ts")
	if err := os.WriteFile(suite, []byte("import { expect, test } from 'rush-webtest'; test('small', () => expect(1).toBe(1))"), 0600); err != nil {
		t.Fatal(err)
	}
	builder := NewBuilder()
	defer builder.Close()
	bundle, _, err := builder.Build(directory, suite)
	if err != nil {
		t.Fatal(err)
	}
	if len(bundle) > 50_000 {
		t.Fatalf("suite bundle still contains shared browser runtime: %d bytes", len(bundle))
	}
	if strings.Contains(bundle, "TestingLibraryElementError") {
		t.Fatal("suite bundle contains Testing Library implementation")
	}
}

func TestBuilderOmitsEmbeddedSourcesFromInlineSourceMaps(t *testing.T) {
	t.Setenv("RUSH_BROWSER_MODULE", "")
	directory := t.TempDir()
	suite := filepath.Join(directory, "source-map.test.ts")
	if err := os.WriteFile(suite, []byte("import { test } from 'rush-webtest'; test('mapped', () => {})"), 0600); err != nil {
		t.Fatal(err)
	}
	builder := NewBuilder()
	defer builder.Close()
	bundle, _, err := builder.Build(directory, suite)
	if err != nil {
		t.Fatal(err)
	}
	const marker = "//# sourceMappingURL=data:application/json;base64,"
	markerIndex := strings.LastIndex(bundle, marker)
	if markerIndex < 0 {
		t.Fatal("bundle does not contain an inline source map")
	}
	encoded := strings.TrimPrefix(bundle[markerIndex:], marker)
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encoded))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(decoded), `"sourcesContent"`) {
		t.Fatal("inline source map duplicates consumer source content")
	}
}

func TestBuilderBundlesViteAssetsAndPackageStyles(t *testing.T) {
	t.Setenv("RUSH_BROWSER_MODULE", "")
	directory := t.TempDir()
	packageDirectory := filepath.Join(directory, "node_modules", "style-package")
	if err := os.MkdirAll(packageDirectory, 0700); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		filepath.Join(directory, "icon.svg"):            `<svg xmlns="http://www.w3.org/2000/svg"><path d="M0 0h1v1z"/></svg>`,
		filepath.Join(directory, "local.css"):           `@import "style-package"; .local { background: url("./icon.svg"); }`,
		filepath.Join(packageDirectory, "package.json"): `{"exports":{".":{"style":"./style.css","default":"./index.js"}}}`,
		filepath.Join(packageDirectory, "index.js"):     `throw new Error("style condition was not selected")`,
		filepath.Join(packageDirectory, "style.css"):    `.from-package { color: rgb(1, 2, 3); }`,
		filepath.Join(directory, "suite.ts"): `
			import icon from "./icon.svg";
			import iconURL from "./icon.svg?url";
			import "./local.css";
			import { expect, test } from "rush-webtest";
			test("assets", () => expect(iconURL).toBe(icon));
		`,
	}
	for path, contents := range files {
		if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
			t.Fatal(err)
		}
	}

	builder := NewBuilder()
	defer builder.Close()
	bundle, _, err := builder.Build(directory, filepath.Join(directory, "suite.ts"))
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"data:image/svg+xml,", ".local", ".from-package", "data-rush-bundle-style"} {
		if !strings.Contains(bundle, expected) {
			t.Fatalf("bundle does not include %q", expected)
		}
	}
}

func TestBuilderUsesConsumerAliasesAndLoaders(t *testing.T) {
	t.Setenv("RUSH_BROWSER_MODULE", "")
	directory := t.TempDir()
	files := map[string]string{
		filepath.Join(directory, "message.txt"): "consumer loader value",
		filepath.Join(directory, "pwa-stub.ts"): `export const registerSW = () => "consumer alias value"`,
		filepath.Join(directory, "suite.ts"): `
			import message from "./message.txt";
			import { registerSW } from "virtual:pwa-register";
			import { expect, test } from "rush-webtest";
			test("configuration", () => expect([message, registerSW()]).toEqual(["consumer loader value", "consumer alias value"]));
		`,
	}
	for path, contents := range files {
		if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
			t.Fatal(err)
		}
	}

	builder := NewBuilder(BuilderOptions{
		Aliases: map[string]string{"virtual:pwa-register": "./pwa-stub.ts"},
		Loaders: map[string]string{".txt": "text"},
	})
	defer builder.Close()
	bundle, _, err := builder.Build(directory, filepath.Join(directory, "suite.ts"))
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"consumer loader value", "consumer alias value"} {
		if !strings.Contains(bundle, expected) {
			t.Fatalf("bundle does not include %q", expected)
		}
	}
}

func TestBuilderDefinesViteEnvironment(t *testing.T) {
	t.Setenv("RUSH_BROWSER_MODULE", "")
	t.Setenv("RUSH_NODE_ENV", "test")
	t.Setenv("VITE_PUBLIC_ORIGIN", "https://example.test")
	directory := t.TempDir()
	suite := filepath.Join(directory, "suite.ts")
	if err := os.WriteFile(suite, []byte(`
		import { expect, test } from "rush-webtest";
		test("environment", () => expect([
			import.meta.env.MODE,
			import.meta.env.DEV,
			import.meta.env.PROD,
			import.meta.env.SSR,
			import.meta.env.BASE_URL,
			import.meta.env.VITE_PUBLIC_ORIGIN,
		]).toEqual(["test", true, false, false, "/", "https://example.test"]));
	`), 0600); err != nil {
		t.Fatal(err)
	}

	builder := NewBuilder()
	defer builder.Close()
	bundle, _, err := builder.Build(directory, suite)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(bundle, "import_meta.env") || !strings.Contains(bundle, "https://example.test") {
		t.Fatalf("Vite environment was not compiled into the suite")
	}
}
