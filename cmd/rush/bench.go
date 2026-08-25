package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/Marcisbee/rush/internal/rush"
)

type benchmarkResult struct {
	Name         string             `json:"name"`
	Metric       string             `json:"metric"`
	TargetMS     float64            `json:"target_ms"`
	SamplesMS    []float64          `json:"samples_ms"`
	MedianMS     float64            `json:"median_ms"`
	Passed       bool               `json:"passed"`
	TestCount    int                `json:"test_count,omitempty"`
	PhaseMedians map[string]float64 `json:"phase_medians_ms,omitempty"`
	Measurement  string             `json:"measurement"`
}

func runBenchmarks(args []string, output io.Writer) error {
	set := flag.NewFlagSet("bench", flag.ContinueOnError)
	repeat := set.Int("repeat", 5, "measured repetitions per warm scenario")
	coldRepeat := set.Int("cold-repeat", 3, "measured cold starts")
	jsonOutput := set.Bool("json", false, "write JSON")
	if err := set.Parse(args); err != nil {
		return err
	}
	if *repeat < 1 || *coldRepeat < 1 {
		return errors.New("repeat counts must be positive")
	}
	root, err := repoRoot()
	if err != nil {
		return err
	}

	results := make([]benchmarkResult, 0, 6)
	coldSamples := make([]float64, 0, *coldRepeat)
	for i := 0; i < *coldRepeat; i++ {
		if err := rush.Stop(false); err != nil {
			return err
		}
		response, err := benchmarkRun(root, "benchmarks/fixtures/smoke.ts")
		if err != nil {
			return err
		}
		coldSamples = append(coldSamples, response.StartupMS)
	}
	coldMedian := median(coldSamples)
	results = append(results, benchmarkResult{Name: "cold startup", Metric: "daemon-to-page-ready", TargetMS: 2000, SamplesMS: coldSamples, MedianMS: coldMedian, Passed: coldMedian < 2000, Measurement: "native process start through WebKitGTK bridge readiness; excludes build and user test time"})

	scenarios := []struct {
		name, file, metric string
		target             float64
		count              int
	}{
		{"warm assertions", "benchmarks/fixtures/assertions.ts", "page-total", 250, 1000},
		{"warm DOM", "benchmarks/fixtures/dom.ts", "page-total", 1000, 1000},
		{"warm Preact components", "benchmarks/fixtures/components.tsx", "page-total", 5000, 1000},
		{"100-test warm", "benchmarks/fixtures/hundred.ts", "page-total", 1000, 100},
	}
	for _, scenario := range scenarios {
		if _, err := benchmarkRun(root, scenario.file); err != nil { // warm browser and esbuild graph
			return err
		}
		samples := make([]float64, 0, *repeat)
		phases := newPhaseSamples()
		for i := 0; i < *repeat; i++ {
			response, err := benchmarkRun(root, scenario.file)
			if err != nil {
				return err
			}
			if failedTests(response) > 0 || len(response.Suites) != 1 || len(response.Suites[0].Tests) != scenario.count {
				return fmt.Errorf("%s did not complete its declared %d passing tests", scenario.name, scenario.count)
			}
			timing := response.Suites[0].Timing
			samples = append(samples, timing.TotalMS)
			phases.add(timing)
		}
		value := median(samples)
		results = append(results, benchmarkResult{Name: scenario.name, Metric: scenario.metric, TargetMS: scenario.target, SamplesMS: samples, MedianMS: value, Passed: value < scenario.target, TestCount: scenario.count, PhaseMedians: phases.medians(), Measurement: "WebKit performance.now() around registration and test execution; build excluded from the target metric"})
	}

	rebuild, err := benchmarkRebuild(root, *repeat)
	if err != nil {
		return err
	}
	results = append(results, rebuild)

	allPassed := true
	for _, result := range results {
		allPassed = allPassed && result.Passed
	}
	if *jsonOutput {
		encoder := json.NewEncoder(output)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(map[string]any{"results": results, "passed": allPassed}); err != nil {
			return err
		}
	} else {
		for _, result := range results {
			verdict := "PASS"
			if !result.Passed {
				verdict = "FAIL"
			}
			fmt.Fprintf(output, "%s %-23s median %8.2fms  target <%8.2fms  samples %v\n", verdict, result.Name, result.MedianMS, result.TargetMS, result.SamplesMS)
			if len(result.PhaseMedians) > 0 {
				fmt.Fprintf(output, "     build %.2fms | runner %.2fms | application %.2fms | network %.2fms | intentional wait %.2fms\n",
					result.PhaseMedians["build"], result.PhaseMedians["runner"], result.PhaseMedians["application"], result.PhaseMedians["network"], result.PhaseMedians["intentional_wait"])
			}
		}
	}
	if !allPassed {
		return errors.New("one or more measured performance targets were missed")
	}
	return nil
}

func benchmarkRun(root, file string) (rush.Response, error) {
	return rush.Send(rush.Request{Action: "run", CWD: root, Files: []string{file}, Timeout: (30 * time.Second).Milliseconds()}, false)
}

func benchmarkRebuild(root string, repeat int) (benchmarkResult, error) {
	temp, err := os.MkdirTemp(root, ".rush-rebuild-")
	if err != nil {
		return benchmarkResult{}, err
	}
	defer os.RemoveAll(temp)
	entry := filepath.Join(temp, "rebuild.ts")
	write := func(iteration int) error {
		content := fmt.Sprintf("import { expect, test } from '@rush/browser';\nfor (let i = 0; i < 100; i++) test(`rebuild ${i}`, () => expect(i + %d).toBe(i + %d));\n", iteration, iteration)
		return os.WriteFile(entry, []byte(content), 0600)
	}
	if err := write(0); err != nil {
		return benchmarkResult{}, err
	}
	if _, err := rush.Send(rush.Request{Action: "run", CWD: root, Files: []string{entry}}, false); err != nil {
		return benchmarkResult{}, err
	}
	samples := make([]float64, 0, repeat)
	phases := newPhaseSamples()
	for i := 1; i <= repeat; i++ {
		if err := write(i); err != nil {
			return benchmarkResult{}, err
		}
		response, err := rush.Send(rush.Request{Action: "run", CWD: root, Files: []string{entry}}, false)
		if err != nil {
			return benchmarkResult{}, err
		}
		if failedTests(response) > 0 || len(response.Suites) != 1 || len(response.Suites[0].Tests) != 100 {
			return benchmarkResult{}, errors.New("incremental rebuild fixture did not complete 100 passing tests")
		}
		suite := response.Suites[0]
		samples = append(samples, suite.Timing.BuildMS+suite.Timing.TotalMS)
		phases.add(suite.Timing)
	}
	value := median(samples)
	return benchmarkResult{Name: "incremental rebuild", Metric: "build-plus-affected-page-total", TargetMS: 500, SamplesMS: samples, MedianMS: value, Passed: value < 500, TestCount: 100, PhaseMedians: phases.medians(), Measurement: "esbuild incremental context rebuild after a source edit plus affected tests in the warm page"}, nil
}

type phaseSamples struct {
	build, runner, application, network, wait []float64
}

func newPhaseSamples() *phaseSamples { return &phaseSamples{} }

func (p *phaseSamples) add(timing rush.Timing) {
	p.build = append(p.build, timing.BuildMS)
	p.runner = append(p.runner, timing.RunnerMS)
	p.application = append(p.application, timing.ApplicationMS)
	p.network = append(p.network, timing.NetworkMS)
	p.wait = append(p.wait, timing.WaitMS)
}

func (p *phaseSamples) medians() map[string]float64 {
	return map[string]float64{
		"build":            median(p.build),
		"runner":           median(p.runner),
		"application":      median(p.application),
		"network":          median(p.network),
		"intentional_wait": median(p.wait),
	}
}
