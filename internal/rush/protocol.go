package rush

import "time"

type Request struct {
	Action     string       `json:"action"`
	CWD        string       `json:"cwd,omitempty"`
	Files      []string     `json:"files,omitempty"`
	Bundles    []BuiltSuite `json:"bundles,omitempty"`
	BuildMS    float64      `json:"build_ms,omitempty"`
	WatchFiles []string     `json:"watch_files,omitempty"`
	Timeout    int64        `json:"timeout_ms,omitempty"`
}

type Timing struct {
	BuildMS       float64 `json:"build_ms"`
	CompileMS     float64 `json:"compile_ms,omitempty"`
	ResetMS       float64 `json:"reset_ms,omitempty"`
	RunnerMS      float64 `json:"runner_ms"`
	ApplicationMS float64 `json:"application_ms"`
	NetworkMS     float64 `json:"network_ms"`
	WaitMS        float64 `json:"wait_ms"`
	TotalMS       float64 `json:"total_ms"`
}

type Profile struct {
	BrowserRealms      int     `json:"browser_realms"`
	BundleMS           float64 `json:"bundle_ms"`
	NativeHostMS       float64 `json:"native_host_ms"`
	BridgeMS           float64 `json:"bridge_ms"`
	BrowserExecutionMS float64 `json:"browser_execution_ms"`
	TestExecutionMS    float64 `json:"test_execution_ms"`
	ResetMS            float64 `json:"reset_ms"`
	ReportingMS        float64 `json:"reporting_ms"`
}

type TestResult struct {
	Name       string  `json:"name"`
	Status     string  `json:"status"`
	Duration   float64 `json:"duration_ms"`
	Error      string  `json:"error,omitempty"`
	SkipReason string  `json:"skip_reason,omitempty"`
}

type SuiteResult struct {
	File   string       `json:"file"`
	Tests  []TestResult `json:"tests"`
	Timing Timing       `json:"timing"`
}

type Response struct {
	Error      string        `json:"error,omitempty"`
	Cold       bool          `json:"cold"`
	StartupMS  float64       `json:"startup_ms"`
	WallMS     float64       `json:"wall_ms"`
	Profile    Profile       `json:"profile"`
	Suites     []SuiteResult `json:"suites,omitempty"`
	WatchFiles []string      `json:"watch_files,omitempty"`
}

func milliseconds(d time.Duration) float64 {
	return float64(d) / float64(time.Millisecond)
}
