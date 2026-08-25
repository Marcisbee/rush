package harness

import (
	"context"
	"encoding/json"
	"testing"
)

type fakeBrowser struct{ evaluations int }

func (*fakeBrowser) LoadHTML(context.Context, string) error { return nil }
func (b *fakeBrowser) Evaluate(_ context.Context, _ string) (json.RawMessage, error) {
	b.evaluations++
	if b.evaluations == 1 {
		return json.RawMessage(`{"dom":true,"selection":true,"beforeInput":true,"shadowDOM":true,"iframe":true}`), nil
	}
	return json.RawMessage(`{"passed":true,"milliseconds":0.01}`), nil
}

func TestSharedHarness(t *testing.T) {
	browser := &fakeBrowser{}
	conformance, err := RunConformance(context.Background(), browser)
	if err != nil || !conformance.Passed() {
		t.Fatalf("conformance = %+v, %v", conformance, err)
	}
	performance, err := RunPerformance(context.Background(), browser, 3)
	if err != nil {
		t.Fatal(err)
	}
	if performance.Assertions.Repeats != 3 || performance.DOM.Repeats != 3 {
		t.Fatalf("performance = %+v", performance)
	}
}
