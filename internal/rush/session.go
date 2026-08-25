package rush

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

const defaultSessionPoolSize = 4

type SessionLease struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type SessionEvaluation struct {
	Value  json.RawMessage `json:"value"`
	URL    string          `json:"url"`
	Timing Timing          `json:"timing"`
}

type SessionNavigation struct {
	URL    string `json:"url"`
	Timing Timing `json:"timing"`
}

type sessionCommand struct {
	ID     string `json:"id"`
	Action string `json:"action"`
	URL    string `json:"url,omitempty"`
	Source string `json:"source,omitempty"`
}

type sessionReply struct {
	ID     string          `json:"id"`
	URL    string          `json:"url,omitempty"`
	Value  json.RawMessage `json:"value,omitempty"`
	Timing Timing          `json:"timing"`
	Error  string          `json:"error,omitempty"`
}

type sessionWorker interface {
	call(sessionCommand) (sessionReply, error)
	close() error
}

type sessionWorkerFactory func() (sessionWorker, error)

type sessionSlot struct {
	worker sessionWorker
	lease  string
}

// SessionPool keeps a bounded set of browser workers warm. Each worker is a
// separate process and therefore has an independent WebKit data store even
// when all clients navigate to the same application origin.
type SessionPool struct {
	mu      sync.Mutex
	slots   []sessionSlot
	leases  map[string]int
	nextID  atomic.Uint64
	factory sessionWorkerFactory
}

func NewSessionPool(headed bool, size int) *SessionPool {
	if size < 1 {
		size = defaultSessionPoolSize
	}
	return newSessionPool(size, func() (sessionWorker, error) { return startSessionProcess(headed) })
}

func newSessionPool(size int, factory sessionWorkerFactory) *SessionPool {
	return &SessionPool{slots: make([]sessionSlot, size), leases: make(map[string]int), factory: factory}
}

func (p *SessionPool) Create(names []string) ([]SessionLease, error) {
	if len(names) == 0 {
		return nil, errors.New("a session requires at least one client")
	}
	if len(names) > len(p.slots) {
		return nil, fmt.Errorf("session requested %d clients; the native pool supports %d", len(names), len(p.slots))
	}
	seen := make(map[string]bool, len(names))
	for _, name := range names {
		if name == "" {
			return nil, errors.New("session client names cannot be empty")
		}
		if seen[name] {
			return nil, fmt.Errorf("duplicate session client name %q", name)
		}
		seen[name] = true
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	available := make([]int, 0, len(names))
	for index := range p.slots {
		if p.slots[index].lease == "" {
			available = append(available, index)
		}
	}
	if len(available) < len(names) {
		return nil, fmt.Errorf("session pool has %d free clients; %d requested", len(available), len(names))
	}

	created := make([]SessionLease, 0, len(names))
	for offset, name := range names {
		index := available[offset]
		if p.slots[index].worker == nil {
			worker, err := p.factory()
			if err != nil {
				p.releaseLocked(created)
				return nil, fmt.Errorf("start session client %q: %w", name, err)
			}
			p.slots[index].worker = worker
		}
		id := fmt.Sprintf("session-%d", p.nextID.Add(1))
		p.slots[index].lease = id
		p.leases[id] = index
		created = append(created, SessionLease{ID: id, Name: name})
	}
	return created, nil
}

func (p *SessionPool) Goto(id, url string) (SessionNavigation, error) {
	worker, err := p.worker(id)
	if err != nil {
		return SessionNavigation{}, err
	}
	reply, err := worker.call(sessionCommand{ID: id, Action: "goto", URL: url})
	return SessionNavigation{URL: reply.URL, Timing: reply.Timing}, err
}

func (p *SessionPool) Evaluate(id, source string) (SessionEvaluation, error) {
	worker, err := p.worker(id)
	if err != nil {
		return SessionEvaluation{}, err
	}
	reply, err := worker.call(sessionCommand{ID: id, Action: "evaluate", Source: source})
	if err != nil {
		return SessionEvaluation{}, err
	}
	return SessionEvaluation{Value: reply.Value, URL: reply.URL, Timing: reply.Timing}, nil
}

func (p *SessionPool) Dispose(id string) error {
	p.mu.Lock()
	index, ok := p.leases[id]
	if !ok {
		p.mu.Unlock()
		return fmt.Errorf("unknown or disposed session client %q", id)
	}
	worker := p.slots[index].worker
	p.mu.Unlock()

	_, resetErr := worker.call(sessionCommand{ID: id, Action: "reset"})
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.leases, id)
	p.slots[index].lease = ""
	if resetErr != nil {
		_ = worker.close()
		p.slots[index].worker = nil
		return fmt.Errorf("reset session client: %w", resetErr)
	}
	return nil
}

func (p *SessionPool) worker(id string) (sessionWorker, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	index, ok := p.leases[id]
	if !ok {
		return nil, fmt.Errorf("unknown or disposed session client %q", id)
	}
	return p.slots[index].worker, nil
}

func (p *SessionPool) releaseLocked(leases []SessionLease) {
	for _, lease := range leases {
		index := p.leases[lease.ID]
		delete(p.leases, lease.ID)
		p.slots[index].lease = ""
	}
}

func (p *SessionPool) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for index := range p.slots {
		if p.slots[index].worker != nil {
			_ = p.slots[index].worker.close()
			p.slots[index].worker = nil
		}
	}
	clear(p.leases)
}

type processSessionWorker struct {
	mu      sync.Mutex
	command *exec.Cmd
	input   io.WriteCloser
	output  io.ReadCloser
	encoder *json.Encoder
	decoder *json.Decoder
	profile string
}

func startSessionProcess(headed bool) (*processSessionWorker, error) {
	executable, err := os.Executable()
	if err != nil {
		return nil, err
	}
	profile, err := os.MkdirTemp("", "rush-session-profile-")
	if err != nil {
		return nil, err
	}
	args := []string{"__session-worker"}
	if headed {
		args = append(args, "--headed")
	}
	command := exec.Command(executable, args...)
	command.Env = append(os.Environ(),
		"XDG_DATA_HOME="+filepath.Join(profile, "data"),
		"XDG_CACHE_HOME="+filepath.Join(profile, "cache"),
	)
	input, err := command.StdinPipe()
	if err != nil {
		_ = os.RemoveAll(profile)
		return nil, err
	}
	output, err := command.StdoutPipe()
	if err != nil {
		_ = input.Close()
		_ = os.RemoveAll(profile)
		return nil, err
	}
	command.Stderr = os.Stderr
	if err := command.Start(); err != nil {
		_ = input.Close()
		_ = output.Close()
		_ = os.RemoveAll(profile)
		return nil, err
	}
	return &processSessionWorker{
		command: command, input: input, output: output,
		encoder: json.NewEncoder(input), decoder: json.NewDecoder(output), profile: profile,
	}, nil
}

func (w *processSessionWorker) call(command sessionCommand) (sessionReply, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	started := time.Now()
	if err := w.encoder.Encode(command); err != nil {
		return sessionReply{}, err
	}
	var reply sessionReply
	if err := w.decoder.Decode(&reply); err != nil {
		return sessionReply{}, err
	}
	if reply.ID != command.ID {
		return sessionReply{}, fmt.Errorf("session worker replied to %q instead of %q", reply.ID, command.ID)
	}
	if reply.Error != "" {
		return reply, errors.New(reply.Error)
	}
	wall := milliseconds(time.Since(started))
	reply.Timing.RunnerMS += max(0, wall-reply.Timing.TotalMS)
	return reply, nil
}

func (w *processSessionWorker) close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	_ = w.encoder.Encode(sessionCommand{Action: "close"})
	_ = w.input.Close()
	err := w.command.Wait()
	_ = w.output.Close()
	_ = os.RemoveAll(w.profile)
	return err
}
