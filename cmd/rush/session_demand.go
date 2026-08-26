package main

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
)

const (
	defaultDetectedSessionClients = 2
	maxDetectedSessionClients     = 4
)

var (
	clientsCountPattern = regexp.MustCompile(`\bclients\s*:\s*([0-9]+)`)
	clientsArrayPattern = regexp.MustCompile(`(?s)\bclients\s*:\s*\[([^]]*)\]`)
	clientsKeyPattern   = regexp.MustCompile(`\bclients\s*:`)
)

func detectSessionDemands(files []string) ([]int, error) {
	demands := make([]int, len(files))
	for index, file := range files {
		source, err := os.ReadFile(file)
		if err != nil {
			return nil, fmt.Errorf("inspect session demand in %s: %w", file, err)
		}
		demands[index] = detectSessionDemand(string(source))
	}
	return demands, nil
}

func detectSessionDemand(source string) int {
	demand := 0
	for _, offset := range sessionCallOffsets(source) {
		remainder := strings.TrimSpace(source[offset:])
		clients := defaultDetectedSessionClients
		if strings.HasPrefix(remainder, "{") {
			options := leadingObject(remainder)
			switch {
			case clientsCountPattern.MatchString(options):
				match := clientsCountPattern.FindStringSubmatch(options)
				clients, _ = strconv.Atoi(match[1])
			case clientsArrayPattern.MatchString(options):
				match := clientsArrayPattern.FindStringSubmatch(options)
				clients = countStaticArrayItems(match[1])
			case clientsKeyPattern.MatchString(options):
				clients = maxDetectedSessionClients
			}
		}
		demand = max(demand, min(clients, maxDetectedSessionClients))
	}
	return demand
}

func sessionCallOffsets(source string) []int {
	var offsets []int
	for index := 0; index < len(source); {
		switch {
		case source[index] == '\'' || source[index] == '"' || source[index] == '`':
			index = skipQuoted(source, index)
		case index+1 < len(source) && source[index:index+2] == "//":
			if end := strings.IndexByte(source[index+2:], '\n'); end >= 0 {
				index += end + 3
			} else {
				return offsets
			}
		case index+1 < len(source) && source[index:index+2] == "/*":
			if end := strings.Index(source[index+2:], "*/"); end >= 0 {
				index += end + 4
			} else {
				return offsets
			}
		case source[index] == '.':
			cursor := skipSpace(source, index+1)
			if !strings.HasPrefix(source[cursor:], "session") {
				index++
				continue
			}
			cursor += len("session")
			if cursor < len(source) && (source[cursor] == '_' || source[cursor] == '$' || source[cursor] >= '0' && source[cursor] <= '9' || source[cursor] >= 'A' && source[cursor] <= 'Z' || source[cursor] >= 'a' && source[cursor] <= 'z') {
				index++
				continue
			}
			cursor = skipSpace(source, cursor)
			if cursor < len(source) && source[cursor] == '(' {
				offsets = append(offsets, cursor+1)
			}
			index = cursor + 1
		default:
			index++
		}
	}
	return offsets
}

func skipQuoted(source string, start int) int {
	quote := source[start]
	for index := start + 1; index < len(source); index++ {
		if source[index] == '\\' {
			index++
			continue
		}
		if source[index] == quote {
			return index + 1
		}
	}
	return len(source)
}

func skipSpace(source string, start int) int {
	for start < len(source) {
		switch source[start] {
		case ' ', '\t', '\r', '\n':
			start++
		default:
			return start
		}
	}
	return start
}

func leadingObject(source string) string {
	depth := 0
	quote := byte(0)
	escaped := false
	for index := 0; index < len(source); index++ {
		character := source[index]
		if quote != 0 {
			if escaped {
				escaped = false
				continue
			}
			if character == '\\' {
				escaped = true
				continue
			}
			if character == quote {
				quote = 0
			}
			continue
		}
		switch character {
		case '\'', '"', '`':
			quote = character
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return source[:index+1]
			}
		}
	}
	return source
}

func countStaticArrayItems(source string) int {
	trimmed := strings.TrimSpace(source)
	trimmed = strings.TrimSpace(strings.TrimSuffix(trimmed, ","))
	if trimmed == "" {
		return 0
	}
	source = trimmed
	count := 1
	quote := byte(0)
	escaped := false
	depth := 0
	for index := 0; index < len(source); index++ {
		character := source[index]
		if quote != 0 {
			if escaped {
				escaped = false
				continue
			}
			if character == '\\' {
				escaped = true
				continue
			}
			if character == quote {
				quote = 0
			}
			continue
		}
		switch character {
		case '\'', '"', '`':
			quote = character
		case '[', '{', '(':
			depth++
		case ']', '}', ')':
			depth--
		case ',':
			if depth == 0 {
				count++
			}
		}
	}
	return count
}
