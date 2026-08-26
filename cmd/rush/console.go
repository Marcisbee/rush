package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/Marcisbee/rush/internal/rush"
)

const (
	ansiReset  = "\x1b[0m"
	ansiBold   = "\x1b[1m"
	ansiDim    = "\x1b[2m"
	ansiRed    = "\x1b[31m"
	ansiGreen  = "\x1b[32m"
	ansiYellow = "\x1b[33m"
)

type consoleOptions struct {
	color   bool
	verbose bool
}

func consoleOptionsFor(output io.Writer, verbose bool) consoleOptions {
	return consoleOptions{color: supportsColor(output), verbose: verbose}
}

func supportsColor(output io.Writer) bool {
	if _, disabled := os.LookupEnv("NO_COLOR"); disabled || os.Getenv("TERM") == "dumb" {
		return false
	}
	file, ok := output.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func printResponse(output io.Writer, response rush.Response, options consoleOptions) error {
	passed, failed, skipped, todo := 0, 0, 0, 0
	for suiteIndex, suite := range response.Suites {
		if suiteIndex > 0 {
			fmt.Fprintln(output)
		}
		fmt.Fprintln(output, options.paint(ansiBold, suite.File+":"))
		for testIndex, test := range suite.Tests {
			mark, color := options.testMark(test.Status)
			switch test.Status {
			case "passed":
				passed++
			case "failed":
				failed++
			case "todo":
				todo++
			default:
				skipped++
			}
			duration := ""
			if (test.Status != "skipped" && test.Status != "todo") || test.Duration > 0 {
				duration = " " + options.paint(ansiDim, "["+formatMilliseconds(test.Duration)+"]")
			}
			fmt.Fprintf(output, "%s %s%s\n", options.paint(color, mark), test.Name, duration)
			if test.Error != "" {
				writeFailure(output, test.Error, options)
				if testIndex < len(suite.Tests)-1 {
					fmt.Fprintln(output)
				}
			}
		}
		if options.verbose {
			timing := suite.Timing
			details := fmt.Sprintf(
				"  build %s | runner %s | application %s | network %s | wait %s | page %s",
				formatMilliseconds(timing.BuildMS), formatMilliseconds(timing.RunnerMS),
				formatMilliseconds(timing.ApplicationMS), formatMilliseconds(timing.NetworkMS),
				formatMilliseconds(timing.WaitMS), formatMilliseconds(timing.TotalMS),
			)
			fmt.Fprintln(output, options.paint(ansiDim, details))
		}
	}

	if len(response.Suites) > 0 {
		fmt.Fprintln(output)
	}
	fmt.Fprintf(output, " %s\n", options.paint(ansiGreen, fmt.Sprintf("%d pass", passed)))
	fmt.Fprintf(output, " %s\n", options.paintFailureCount(failed))
	if skipped > 0 {
		fmt.Fprintf(output, " %s\n", options.paint(ansiYellow, fmt.Sprintf("%d skip", skipped)))
	}
	if todo > 0 {
		fmt.Fprintf(output, " %s\n", options.paint(ansiYellow, fmt.Sprintf("%d todo", todo)))
	}
	testCount := passed + failed + skipped + todo
	totalMS := response.WallMS
	if response.Cold {
		totalMS += response.StartupMS
	}
	fmt.Fprintf(output, "Ran %d %s across %d %s. %s\n",
		testCount, plural(testCount, "test", "tests"),
		len(response.Suites), plural(len(response.Suites), "file", "files"),
		options.paint(ansiDim, "["+formatMilliseconds(totalMS)+"]"),
	)
	if options.verbose {
		details := fmt.Sprintf("request %s", formatMilliseconds(response.WallMS))
		if response.Cold {
			details = fmt.Sprintf("startup %s | %s", formatMilliseconds(response.StartupMS), details)
		}
		fmt.Fprintln(output, options.paint(ansiDim, details))
	}
	if failed > 0 {
		return errTestsFailed
	}
	return nil
}

func printWatchChange(output io.Writer, path string, options consoleOptions) {
	mark := "(watch)"
	if options.color {
		mark = "↻"
	}
	fmt.Fprintf(output, "\n%s %s changed\n\n", options.paint(ansiYellow, mark), path)
}

func (options consoleOptions) testMark(status string) (string, string) {
	if options.color {
		switch status {
		case "passed":
			return "✓", ansiGreen
		case "failed":
			return "✗", ansiRed
		case "todo":
			return "-", ansiYellow
		default:
			return "-", ansiYellow
		}
	}
	switch status {
	case "passed":
		return "(pass)", ""
	case "failed":
		return "(fail)", ""
	case "todo":
		return "(todo)", ""
	default:
		return "(skip)", ""
	}
}

func (options consoleOptions) paintFailureCount(failed int) string {
	text := fmt.Sprintf("%d fail", failed)
	if failed == 0 {
		return options.paint(ansiGreen, text)
	}
	return options.paint(ansiRed, text)
}

func (options consoleOptions) paint(color, text string) string {
	if !options.color || color == "" {
		return text
	}
	return color + text + ansiReset
}

func writeFailure(output io.Writer, value string, options consoleOptions) {
	lines := formatFailure(value)
	for index, line := range lines {
		if line == "" {
			fmt.Fprintln(output)
			continue
		}
		line = "  " + line
		if index == 0 {
			line = options.paint(ansiRed, line)
		}
		fmt.Fprintln(output, line)
	}
}

func formatFailure(value string) []string {
	lines := strings.Split(strings.TrimRight(value, "\n"), "\n")
	for len(lines) > 1 && isUselessWebKitFrame(lines[len(lines)-1]) {
		lines = lines[:len(lines)-1]
	}
	accessible := false
	formatted := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "Here are the accessible roles:" {
			accessible = true
			line = "Accessible roles:"
			trimmed = line
		}
		if accessible && strings.Trim(trimmed, "-") == "" && trimmed != "" {
			continue
		}
		if accessible && strings.HasPrefix(trimmed, "Ignored nodes:") {
			accessible = false
			if len(formatted) > 0 && formatted[len(formatted)-1] != "" {
				formatted = append(formatted, "")
			}
		}
		if accessible && trimmed == "" {
			continue
		}
		if accessible && strings.HasPrefix(line, "  ") && (strings.HasPrefix(trimmed, "Name ") || strings.HasPrefix(trimmed, "<")) {
			line = "  " + line
		}
		if line == "" && (len(formatted) == 0 || formatted[len(formatted)-1] == "") {
			continue
		}
		formatted = append(formatted, line)
	}
	for len(formatted) > 0 && formatted[len(formatted)-1] == "" {
		formatted = formatted[:len(formatted)-1]
	}
	return formatted
}

func isUselessWebKitFrame(line string) bool {
	line = strings.TrimSpace(line)
	return line == "@" || strings.HasSuffix(line, "@") && !strings.Contains(line, "://")
}

func formatMilliseconds(value float64) string {
	if value >= 1000 {
		return fmt.Sprintf("%.2fs", value/1000)
	}
	return fmt.Sprintf("%.2fms", value)
}

func plural(count int, singular, plural string) string {
	if count == 1 {
		return singular
	}
	return plural
}
