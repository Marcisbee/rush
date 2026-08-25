package reporter

import (
	"encoding/xml"
	"io"
	"strconv"
	"time"

	"github.com/Marcisbee/rush/result"
)

type JUnit struct{}

type xmlSuites struct {
	XMLName  xml.Name   `xml:"testsuites"`
	Tests    int        `xml:"tests,attr"`
	Failures int        `xml:"failures,attr"`
	Skipped  int        `xml:"skipped,attr"`
	Time     string     `xml:"time,attr"`
	Suites   []xmlSuite `xml:"testsuite"`
}

type xmlSuite struct {
	Name     string    `xml:"name,attr"`
	Tests    int       `xml:"tests,attr"`
	Failures int       `xml:"failures,attr"`
	Skipped  int       `xml:"skipped,attr"`
	Time     string    `xml:"time,attr"`
	Cases    []xmlCase `xml:"testcase"`
}

type xmlCase struct {
	Name      string      `xml:"name,attr"`
	ClassName string      `xml:"classname,attr"`
	Time      string      `xml:"time,attr"`
	Failure   *xmlFailure `xml:"failure,omitempty"`
	Skipped   *struct{}   `xml:"skipped,omitempty"`
}

type xmlFailure struct {
	Message string `xml:"message,attr"`
	Body    string `xml:",chardata"`
}

func (JUnit) Write(writer io.Writer, summary result.Summary) error {
	byName := map[string]int{}
	total := xmlSuites{Tests: len(summary.Tests), Time: seconds(summary.Timing.User)}
	for _, test := range summary.Tests {
		index, ok := byName[test.Suite]
		if !ok {
			total.Suites = append(total.Suites, xmlSuite{Name: test.Suite})
			index = len(total.Suites) - 1
			byName[test.Suite] = index
		}
		suite := &total.Suites[index]
		entry := xmlCase{Name: test.Name, ClassName: test.Suite, Time: seconds(test.Duration)}
		suite.Tests++
		suite.Time = seconds(parseSeconds(suite.Time) + test.Duration)
		switch test.Status {
		case result.Failed:
			suite.Failures++
			total.Failures++
			entry.Failure = &xmlFailure{Message: test.Error, Body: test.Error}
		case result.Skipped, result.Todo:
			suite.Skipped++
			total.Skipped++
			entry.Skipped = &struct{}{}
		}
		suite.Cases = append(suite.Cases, entry)
	}
	if _, err := io.WriteString(writer, xml.Header); err != nil {
		return err
	}
	encoder := xml.NewEncoder(writer)
	encoder.Indent("", "  ")
	if err := encoder.Encode(total); err != nil {
		return err
	}
	_, err := io.WriteString(writer, "\n")
	return err
}

func seconds(value time.Duration) string {
	return strconv.FormatFloat(value.Seconds(), 'f', 6, 64)
}

func parseSeconds(value string) time.Duration {
	seconds, _ := strconv.ParseFloat(value, 64)
	return time.Duration(seconds * float64(time.Second))
}
