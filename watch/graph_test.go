package watch

import (
	"context"
	"reflect"
	"testing"
)

func TestAffectedTraversesReverseImports(t *testing.T) {
	graph := New()
	graph.Suite("tests/a.test.ts")
	graph.Suite("tests/b.test.ts")
	graph.Add("tests/a.test.ts", "src/component.ts")
	graph.Add("tests/b.test.ts", "src/other.ts")
	graph.Add("src/component.ts", "src/shared.ts")

	if got, want := graph.Affected("src/shared.ts"), []string{"tests/a.test.ts"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Affected() = %#v, want %#v", got, want)
	}
	if got, want := graph.Affected("src/shared.ts", "src/other.ts"), []string{"tests/a.test.ts", "tests/b.test.ts"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Affected() = %#v, want %#v", got, want)
	}
}

func TestLoopSkipsUnrelatedChangesAndInvalidatesAll(t *testing.T) {
	graph := New()
	graph.Suite("a.test.ts")
	graph.Suite("b.test.ts")
	graph.Add("a.test.ts", "a.ts")
	changes := make(chan Change, 2)
	changes <- Change{Paths: []string{"unrelated.md"}}
	changes <- Change{Paths: []string{"rush.config.ts"}, Invalidate: true}
	close(changes)
	var runs [][]string
	if err := Loop(context.Background(), graph, changes, func(_ context.Context, suites []string) error {
		runs = append(runs, suites)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if want := [][]string{{"a.test.ts", "b.test.ts"}}; !reflect.DeepEqual(runs, want) {
		t.Fatalf("runs = %#v, want %#v", runs, want)
	}
}

func TestUpdateRemovesStaleImportEdges(t *testing.T) {
	graph := New()
	graph.Suite("card.test.ts")
	graph.Update("card.test.ts", "old.ts")
	graph.Update("card.test.ts", "new.ts")
	if got := graph.Affected("old.ts"); len(got) != 0 {
		t.Fatalf("stale affected suites = %#v", got)
	}
	if got, want := graph.Affected("new.ts"), []string{"card.test.ts"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Affected() = %#v, want %#v", got, want)
	}
}
