package rush

import "time"

type Request struct {
	Action  string   `json:"action"`
	CWD     string   `json:"cwd,omitempty"`
	Files   []string `json:"files,omitempty"`
	Timeout int64    `json:"timeout_ms,omitempty"`
}

type Timing struct {
	BuildMS       float64 `json:"build_ms"`
	RunnerMS      float64 `json:"runner_ms"`
	ApplicationMS float64 `json:"application_ms"`
	NetworkMS     float64 `json:"network_ms"`
	WaitMS        float64 `json:"wait_ms"`
	TotalMS       float64 `json:"total_ms"`
}

type TestResult struct {
	Name     string  `json:"name"`
	Status   string  `json:"status"`
	Duration float64 `json:"duration_ms"`
	Error    string  `json:"error,omitempty"`
}

type SuiteResult struct {
	File   string       `json:"file"`
	Tests  []TestResult `json:"tests"`
	Timing Timing       `json:"timing"`
}

type Response struct {
	Error     string        `json:"error,omitempty"`
	Cold      bool          `json:"cold"`
	StartupMS float64       `json:"startup_ms"`
	WallMS    float64       `json:"wall_ms"`
	Suites    []SuiteResult `json:"suites,omitempty"`
}

func milliseconds(d time.Duration) float64 {
	return float64(d) / float64(time.Millisecond)
}
