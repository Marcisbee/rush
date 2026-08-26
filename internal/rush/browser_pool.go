package rush

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strconv"
	"sync"
)

const (
	defaultBrowserPoolSize = 3
	maxBrowserPoolSize     = 4
)

type batchBrowser interface {
	RunBatch(context.Context, string, []BuiltSuite) (browserBatchResult, error)
}

type browserRealm interface {
	batchBrowser
	Ready() <-chan struct{}
	RunLoop()
	Stop()
	Close()
}

// BrowserPool keeps a fixed number of independent WebViews warm. Bundles are
// assigned to the same realm by their request index so each realm can reuse its
// compiled factory while file-level module and mock isolation stays intact.
type BrowserPool struct {
	realms []browserRealm
	ready  chan struct{}
}

func configuredBrowserPoolSize(headed bool, value string, suiteCount ...int) (int, error) {
	capToSuites := func(size int) int {
		if len(suiteCount) > 0 && suiteCount[0] > 0 {
			return min(size, suiteCount[0])
		}
		return size
	}
	if headed && value == "" {
		return 1, nil
	}
	if value != "" {
		size, err := strconv.Atoi(value)
		if err != nil || size < 1 || size > maxBrowserPoolSize {
			return 0, fmt.Errorf("RUSH_WEBVIEW_POOL_SIZE must be between 1 and %d", maxBrowserPoolSize)
		}
		return capToSuites(size), nil
	}
	size := min(defaultBrowserPoolSize, runtime.GOMAXPROCS(0))
	size = max(1, size)
	return capToSuites(size), nil
}

func NewBrowserPool(headed bool, size int, sessionDemands ...int) (*BrowserPool, error) {
	if size < 1 || size > maxBrowserPoolSize {
		return nil, fmt.Errorf("browser pool size must be between 1 and %d", maxBrowserPoolSize)
	}
	sessionWarmCounts := sessionWarmCountsByRealm(size, sessionDemands)
	realms := make([]browserRealm, 0, size)
	for index := 0; index < size; index++ {
		browser, err := NewBrowser(headed, sessionWarmCounts[index])
		if err != nil {
			for _, realm := range realms {
				realm.Close()
			}
			return nil, fmt.Errorf("create browser realm %d: %w", index+1, err)
		}
		realms = append(realms, browser)
	}
	pool := &BrowserPool{realms: realms, ready: make(chan struct{})}
	go func() {
		for _, realm := range pool.realms {
			<-realm.Ready()
		}
		close(pool.ready)
	}()
	return pool, nil
}

func sessionWarmCountsByRealm(realmCount int, suiteDemands []int) []int {
	counts := make([]int, realmCount)
	for suiteIndex, demand := range suiteDemands {
		realm := suiteIndex % realmCount
		counts[realm] = max(counts[realm], demand)
	}
	return counts
}

func (p *BrowserPool) Ready() <-chan struct{} { return p.ready }
func (p *BrowserPool) Size() int              { return len(p.realms) }
func (p *BrowserPool) RunLoop()               { p.realms[0].RunLoop() }
func (p *BrowserPool) Stop()                  { p.realms[0].Stop() }

func (p *BrowserPool) Close() {
	for _, realm := range p.realms {
		realm.Close()
	}
}

func (p *BrowserPool) RunBatch(ctx context.Context, id string, bundles []BuiltSuite) (browserBatchResult, error) {
	return runAcrossRealms(ctx, id, bundles, p.realms)
}

type realmBatch struct {
	indexes []int
	bundles []BuiltSuite
}

func partitionRealmBatches(bundles []BuiltSuite, realmCount int) []realmBatch {
	count := min(len(bundles), realmCount)
	batches := make([]realmBatch, count)
	for index, bundle := range bundles {
		realm := index % count
		batches[realm].indexes = append(batches[realm].indexes, index)
		batches[realm].bundles = append(batches[realm].bundles, bundle)
	}
	return batches
}

func runAcrossRealms(ctx context.Context, id string, bundles []BuiltSuite, realms []browserRealm) (browserBatchResult, error) {
	if len(bundles) == 0 {
		return browserBatchResult{}, errors.New("no browser bundles to run")
	}
	if len(realms) == 0 {
		return browserBatchResult{}, errors.New("no browser realms are available")
	}
	batches := partitionRealmBatches(bundles, len(realms))
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	results := make([]SuiteResult, len(bundles))
	workerResults := make([]browserBatchResult, len(batches))
	errorsByRealm := make([]error, len(batches))
	var wait sync.WaitGroup
	for realmIndex, batch := range batches {
		wait.Add(1)
		go func() {
			defer wait.Done()
			result, err := realms[realmIndex].RunBatch(ctx, fmt.Sprintf("%s-realm-%d", id, realmIndex+1), batch.bundles)
			if err != nil {
				errorsByRealm[realmIndex] = err
				cancel()
				return
			}
			if len(result.Suites) != len(batch.indexes) {
				errorsByRealm[realmIndex] = fmt.Errorf("realm %d returned %d suites for %d bundles", realmIndex+1, len(result.Suites), len(batch.indexes))
				cancel()
				return
			}
			workerResults[realmIndex] = result
			for batchIndex, suite := range result.Suites {
				results[batch.indexes[batchIndex]] = suite
			}
		}()
	}
	wait.Wait()
	for realmIndex, err := range errorsByRealm {
		if err != nil && !errors.Is(err, context.Canceled) {
			return browserBatchResult{}, fmt.Errorf("browser realm %d: %w", realmIndex+1, err)
		}
	}
	for realmIndex, err := range errorsByRealm {
		if err != nil {
			return browserBatchResult{}, fmt.Errorf("browser realm %d: %w", realmIndex+1, err)
		}
	}

	result := browserBatchResult{Suites: results}
	for _, worker := range workerResults {
		result.BrowserMS = max(result.BrowserMS, worker.BrowserMS)
		result.ReportingMS = max(result.ReportingMS, worker.ReportingMS)
	}
	return result, nil
}
