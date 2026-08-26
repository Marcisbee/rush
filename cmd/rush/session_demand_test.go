package main

import "testing"

func TestDetectSessionDemand(t *testing.T) {
	for name, testCase := range map[string]struct {
		source string
		want   int
	}{
		"none":    {`test("browser", () => {})`, 0},
		"default": {`test.session("chat", () => {})`, 2},
		"count":   {`test.session({clients: 3})("chat", () => {})`, 3},
		"names":   {`test.session({ clients: ["alice", "bob",] })("chat", () => {})`, 2},
		"dynamic": {`test.session({clients: participants})("chat", () => {})`, 4},
		"maximum": {`test.session({clients: 1})("one", fn); test.session({clients: ["a", "b", "c"]})("three", fn)`, 3},
		"ignored": {`const example = "test.session({clients: 4})"; /* test.session({clients: 3}) */ test("browser", fn)`, 0},
	} {
		t.Run(name, func(t *testing.T) {
			if got := detectSessionDemand(testCase.source); got != testCase.want {
				t.Fatalf("demand = %d, want %d", got, testCase.want)
			}
		})
	}
}

func TestParseSessionDemands(t *testing.T) {
	demands, err := parseSessionDemands("0,2,4")
	if err != nil || len(demands) != 3 || demands[1] != 2 || demands[2] != 4 {
		t.Fatalf("demands = %v, error = %v", demands, err)
	}
	for _, invalid := range []string{"many", "-1", "5"} {
		if _, err := parseSessionDemands(invalid); err == nil {
			t.Fatalf("invalid demand %q was accepted", invalid)
		}
	}
}
