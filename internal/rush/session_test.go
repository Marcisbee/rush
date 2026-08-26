package rush

import (
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"
)

type fakeSessionWorker struct {
	mu      sync.Mutex
	calls   []sessionCommand
	closed  bool
	storage map[string]string
}

func TestSessionPoolWarmsRequestedWorkersConcurrently(t *testing.T) {
	var mu sync.Mutex
	started := 0
	allStarted := make(chan struct{})
	release := make(chan struct{})
	pool := newSessionPool(4, func() (sessionWorker, error) {
		mu.Lock()
		started++
		if started == 3 {
			close(allStarted)
		}
		mu.Unlock()
		<-release
		return &fakeSessionWorker{storage: make(map[string]string)}, nil
	}, 3)
	defer pool.Close()

	type createResult struct {
		leases []SessionLease
		err    error
	}
	created := make(chan createResult, 1)
	go func() {
		leases, err := pool.Create([]string{"alice", "bob", "carol"})
		created <- createResult{leases: leases, err: err}
	}()
	select {
	case <-allStarted:
		close(release)
	case <-time.After(time.Second):
		close(release)
		t.Fatal("session workers did not start concurrently")
	}
	result := <-created
	if result.err != nil {
		t.Fatal(result.err)
	}
	if len(result.leases) != 3 {
		t.Fatalf("leases = %d, want 3", len(result.leases))
	}
}

func (w *fakeSessionWorker) call(command sessionCommand) (sessionReply, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.calls = append(w.calls, command)
	if w.closed {
		return sessionReply{}, errors.New("closed")
	}
	if command.Action == "reset" {
		clear(w.storage)
	}
	value, _ := json.Marshal(w.storage)
	return sessionReply{ID: command.ID, URL: command.URL, Value: value}, nil
}

func (w *fakeSessionWorker) close() error {
	w.closed = true
	return nil
}

func TestSessionPoolAllocatesIsolatedNamedClientsAndReusesResetWorkers(t *testing.T) {
	var workers []*fakeSessionWorker
	pool := newSessionPool(2, func() (sessionWorker, error) {
		worker := &fakeSessionWorker{storage: make(map[string]string)}
		workers = append(workers, worker)
		return worker, nil
	})
	defer pool.Close()

	leases, err := pool.Create([]string{"alice", "bob"})
	if err != nil {
		t.Fatal(err)
	}
	if leases[0].Name != "alice" || leases[1].Name != "bob" || leases[0].ID == leases[1].ID {
		t.Fatalf("unexpected leases: %#v", leases)
	}
	workers[0].storage["identity"] = "alice"
	workers[1].storage["identity"] = "bob"
	if workers[0].storage["identity"] == workers[1].storage["identity"] {
		t.Fatal("client state was not isolated")
	}
	if err := pool.Dispose(leases[0].ID); err != nil {
		t.Fatal(err)
	}
	if len(workers[0].storage) != 0 {
		t.Fatal("disposing a client did not reset its state")
	}

	reused, err := pool.Create([]string{"carol"})
	if err != nil {
		t.Fatal(err)
	}
	if len(workers) != 2 {
		t.Fatalf("expected the warm worker pool to be reused, started %d workers", len(workers))
	}
	if reused[0].Name != "carol" || reused[0].ID == leases[0].ID {
		t.Fatalf("unexpected reused lease: %#v", reused[0])
	}
}

func TestSessionPoolRejectsInvalidOrExcessClients(t *testing.T) {
	pool := newSessionPool(1, func() (sessionWorker, error) {
		return &fakeSessionWorker{storage: make(map[string]string)}, nil
	})
	defer pool.Close()
	for _, names := range [][]string{{}, {"same", "same"}, {"one", "two"}} {
		if _, err := pool.Create(names); err == nil {
			t.Fatalf("expected %v to be rejected", names)
		}
	}
}
