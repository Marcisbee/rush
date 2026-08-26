package rush

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

type fakeBrowserRealm struct {
	batches [][]BuiltSuite
	err     error
	stops   int
}

func (f *fakeBrowserRealm) Ready() <-chan struct{} {
	ready := make(chan struct{})
	close(ready)
	return ready
}
func (f *fakeBrowserRealm) RunLoop() {}
func (f *fakeBrowserRealm) Stop()    { f.stops++ }
func (f *fakeBrowserRealm) Close()   {}
func (f *fakeBrowserRealm) RunBatch(_ context.Context, _ string, bundles []BuiltSuite) (browserBatchResult, error) {
	f.batches = append(f.batches, append([]BuiltSuite(nil), bundles...))
	if f.err != nil {
		return browserBatchResult{}, f.err
	}
	suites := make([]SuiteResult, len(bundles))
	for index, bundle := range bundles {
		suites[index].File = bundle.File
	}
	return browserBatchResult{Suites: suites}, nil
}

func TestConfiguredBrowserPoolSizeIsBounded(t *testing.T) {
	if size, err := configuredBrowserPoolSize(false, ""); err != nil || size < 1 || size > defaultBrowserPoolSize {
		t.Fatalf("default size = %d, error = %v", size, err)
	}
	if size, err := configuredBrowserPoolSize(true, ""); err != nil || size != 1 {
		t.Fatalf("headed size = %d, error = %v", size, err)
	}
	if size, err := configuredBrowserPoolSize(false, "4"); err != nil || size != 4 {
		t.Fatalf("configured size = %d, error = %v", size, err)
	}
	if size, err := configuredBrowserPoolSize(false, "", 1); err != nil || size != 1 {
		t.Fatalf("single-suite size = %d, error = %v", size, err)
	}
	if size, err := configuredBrowserPoolSize(false, "4", 1); err != nil || size != 1 {
		t.Fatalf("capped configured size = %d, error = %v", size, err)
	}
	for _, invalid := range []string{"0", "5", "many"} {
		if _, err := configuredBrowserPoolSize(false, invalid); err == nil {
			t.Fatalf("configuration %q was accepted", invalid)
		}
	}
}

func TestRunAcrossRealmsKeepsStableAssignmentsAndResultOrder(t *testing.T) {
	realms := []browserRealm{&fakeBrowserRealm{}, &fakeBrowserRealm{}, &fakeBrowserRealm{}}
	bundles := make([]BuiltSuite, 7)
	for index := range bundles {
		bundles[index].File = string(rune('a' + index))
	}
	result, err := runAcrossRealms(context.Background(), "run-1", bundles, realms)
	if err != nil {
		t.Fatal(err)
	}
	files := make([]string, len(result.Suites))
	for index, suite := range result.Suites {
		files[index] = suite.File
	}
	if !reflect.DeepEqual(files, []string{"a", "b", "c", "d", "e", "f", "g"}) {
		t.Fatalf("result order = %v", files)
	}
	wantAssignments := [][]string{{"a", "d", "g"}, {"b", "e"}, {"c", "f"}}
	for index, realm := range realms {
		fake := realm.(*fakeBrowserRealm)
		var got []string
		for _, bundle := range fake.batches[0] {
			got = append(got, bundle.File)
		}
		if !reflect.DeepEqual(got, wantAssignments[index]) {
			t.Fatalf("realm %d assignment = %v, want %v", index+1, got, wantAssignments[index])
		}
	}
}

func TestRunAcrossRealmsSurfacesWorkerFailure(t *testing.T) {
	realms := []browserRealm{&fakeBrowserRealm{}, &fakeBrowserRealm{err: errors.New("crashed")}}
	_, err := runAcrossRealms(context.Background(), "run-1", []BuiltSuite{{File: "a"}, {File: "b"}}, realms)
	if err == nil || !strings.Contains(err.Error(), "browser realm 2: crashed") {
		t.Fatalf("error = %v", err)
	}
}

func TestServerStopIsIdempotent(t *testing.T) {
	realm := &fakeBrowserRealm{}
	server := &Server{browser: &BrowserPool{realms: []browserRealm{realm}}}
	server.stop()
	server.stop()
	if realm.stops != 1 {
		t.Fatalf("browser stops = %d, want 1", realm.stops)
	}
}
