package rush

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

type sourceReplacement struct {
	start int
	end   int
	value string
}

type mockCall struct {
	start     int
	end       int
	arguments string
}

type hoistedDeclaration struct {
	start  int
	end    int
	source string
}

// transformHoistedMocks mirrors the public API's hoist transform at the native
// builder seam. Static imports are delayed until top-level vi.mock registrations
// exist. The builder wraps the complete output in the registration promise that
// the WebKit harness awaits.
func transformHoistedMocks(source string) (string, error) {
	return transformHoistedMocksWithIDs(source, nil)
}

func transformHoistedMocksWithIDs(source string, resolvedIDs map[string]string) (string, error) {
	mocks, err := findMockCalls(source)
	if err != nil || len(mocks) == 0 {
		return source, err
	}
	hoisted, err := findHoistedDeclarations(source)
	if err != nil {
		return "", err
	}

	replacements := make([]sourceReplacement, 0, len(mocks)+len(hoisted)+4)
	for _, mock := range mocks {
		replacements = append(replacements, sourceReplacement{start: mock.start, end: mock.end})
	}
	for _, declaration := range hoisted {
		replacements = append(replacements, sourceReplacement{start: declaration.start, end: declaration.end})
	}
	imports, err := findStaticImports(source)
	if err != nil {
		return "", err
	}
	for _, item := range imports {
		statement := source[item.start:item.end]
		sourceID, sourceErr := staticImportSource(statement)
		if sourceErr != nil {
			return "", sourceErr
		}
		runtimeID := sourceID
		if resolved := resolvedIDs[sourceID]; resolved != "" {
			runtimeID = resolved
		}
		rewritten, rewriteErr := rewriteStaticImportWithID(statement, runtimeID)
		if rewriteErr != nil {
			return "", rewriteErr
		}
		replacements = append(replacements, sourceReplacement{start: item.start, end: item.end, value: rewritten})
	}

	sort.Slice(replacements, func(left, right int) bool { return replacements[left].start > replacements[right].start })
	body := source
	for _, replacement := range replacements {
		body = body[:replacement.start] + replacement.value + body[replacement.end:]
	}
	registrations := make([]string, 0, len(mocks))
	for _, mock := range mocks {
		arguments, rewriteErr := rewriteStaticImportActualCalls(mock.arguments)
		if rewriteErr != nil {
			return "", rewriteErr
		}
		sourceID, sourceErr := mockModuleID(arguments)
		if sourceErr != nil {
			return "", sourceErr
		}
		if resolved := resolvedIDs[sourceID]; resolved != "" {
			arguments, err = replaceFirstStringArgument(arguments, resolved)
			if err != nil {
				return "", err
			}
		}
		registrations = append(registrations, "__rushRegisterMock__("+arguments+");")
	}
	runtimeImports := "__rushRegisterMock__, __rushImport__"
	hoistedSources := make([]string, 0, len(hoisted))
	if len(hoisted) > 0 {
		runtimeImports += ", vi as __rushVi"
		for _, declaration := range hoisted {
			hoistedSources = append(hoistedSources, rewriteHoistedVI(declaration.source))
		}
	}
	header := "import { " + runtimeImports + " } from \"rush-webtest/internal\";\n"
	if len(hoistedSources) > 0 {
		header += strings.Join(hoistedSources, "\n") + "\n"
	}
	return header + strings.Join(registrations, "\n") + "\n" + body, nil
}

func rewriteStaticImportActualCalls(source string) (string, error) {
	const method = "vi.importActual"
	var replacements []sourceReplacement
	for index := 0; index < len(source); {
		switch {
		case source[index] == '\'' || source[index] == '"' || source[index] == '`':
			index = skipSourceString(source, index, source[index])
		case strings.HasPrefix(source[index:], "//"):
			index = skipLineComment(source, index)
		case strings.HasPrefix(source[index:], "/*"):
			index = skipBlockComment(source, index)
		case strings.HasPrefix(source[index:], method) && !isIdentifierByte(byteBefore(source, index)) && !isIdentifierByte(byteAt(source, index+len(method))):
			cursor := index + len(method)
			for cursor < len(source) && isSourceSpace(source[cursor]) {
				cursor++
			}
			if cursor < len(source) && source[cursor] == '<' {
				var err error
				cursor, err = findClosingTypeArguments(source, cursor)
				if err != nil {
					return "", err
				}
				for cursor < len(source) && isSourceSpace(source[cursor]) {
					cursor++
				}
			}
			if cursor >= len(source) || source[cursor] != '(' {
				index += len(method)
				continue
			}
			close, err := findClosingParenthesis(source, cursor)
			if err != nil {
				return "", err
			}
			argument, ok := staticStringArgument(source[cursor+1 : close])
			if ok {
				replacements = append(replacements, sourceReplacement{
					start: cursor + 1,
					end:   close,
					value: "() => import(" + argument + ")",
				})
			}
			index = close + 1
		default:
			index++
		}
	}
	if len(replacements) == 0 {
		return source, nil
	}
	sort.Slice(replacements, func(left, right int) bool { return replacements[left].start > replacements[right].start })
	result := source
	for _, replacement := range replacements {
		result = result[:replacement.start] + replacement.value + result[replacement.end:]
	}
	return result, nil
}

func findClosingTypeArguments(source string, open int) (int, error) {
	depth := 1
	for index := open + 1; index < len(source); index++ {
		switch {
		case source[index] == '\'' || source[index] == '"' || source[index] == '`':
			index = skipSourceString(source, index, source[index]) - 1
		case strings.HasPrefix(source[index:], "//"):
			index = skipLineComment(source, index) - 1
		case strings.HasPrefix(source[index:], "/*"):
			index = skipBlockComment(source, index) - 1
		case source[index] == '<':
			depth++
		case source[index] == '>' && byteBefore(source, index) != '=':
			depth--
			if depth == 0 {
				return index + 1, nil
			}
		}
	}
	return 0, errors.New("unterminated vi.importActual type arguments")
}

func staticStringArgument(value string) (string, bool) {
	trimmed := strings.TrimSpace(value)
	if strings.HasSuffix(trimmed, ",") {
		trimmed = strings.TrimSpace(strings.TrimSuffix(trimmed, ","))
	}
	if len(trimmed) < 2 || (trimmed[0] != '\'' && trimmed[0] != '"') {
		return "", false
	}
	end := skipSourceString(trimmed, 0, trimmed[0])
	if end != len(trimmed) {
		return "", false
	}
	return trimmed, true
}

func transformDependencyImports(source string, resolvedIDs map[string]string) (string, error) {
	imports, err := findStaticImports(source)
	if err != nil {
		return "", err
	}
	replacements := make([]sourceReplacement, 0, len(imports))
	for _, item := range imports {
		statement := source[item.start:item.end]
		sourceID, sourceErr := staticImportSource(statement)
		if sourceErr != nil {
			return "", sourceErr
		}
		resolvedID := resolvedIDs[sourceID]
		if resolvedID == "" {
			continue
		}
		rewritten, rewriteErr := rewriteStaticImportWithID(statement, resolvedID)
		if rewriteErr != nil {
			return "", rewriteErr
		}
		replacements = append(replacements, sourceReplacement{start: item.start, end: item.end, value: rewritten})
	}
	if len(replacements) == 0 {
		return source, nil
	}
	sort.Slice(replacements, func(left, right int) bool { return replacements[left].start > replacements[right].start })
	body := source
	for _, replacement := range replacements {
		body = body[:replacement.start] + replacement.value + body[replacement.end:]
	}
	return "import { __rushImport__ } from \"rush-webtest/internal\";\n" + body, nil
}

func findMockCalls(source string) ([]mockCall, error) {
	var calls []mockCall
	depth := 0
	for index := 0; index < len(source); {
		switch {
		case source[index] == '\'' || source[index] == '"' || source[index] == '`':
			index = skipSourceString(source, index, source[index])
		case strings.HasPrefix(source[index:], "//"):
			index = skipLineComment(source, index)
		case strings.HasPrefix(source[index:], "/*"):
			index = skipBlockComment(source, index)
		case source[index] == '{':
			depth++
			index++
		case source[index] == '}':
			depth--
			index++
		case depth == 0 && strings.HasPrefix(source[index:], "vi.mock") && !isIdentifierByte(byteBefore(source, index)) && !isIdentifierByte(byteAt(source, index+7)):
			open := index + 7
			for open < len(source) && isSourceSpace(source[open]) {
				open++
			}
			if open >= len(source) || source[open] != '(' {
				index += 7
				continue
			}
			close, err := findClosingParenthesis(source, open)
			if err != nil {
				return nil, err
			}
			end := close + 1
			for end < len(source) && isSourceSpace(source[end]) && source[end] != '\n' && source[end] != '\r' {
				end++
			}
			if end < len(source) && source[end] == ';' {
				end++
			}
			calls = append(calls, mockCall{start: index, end: end, arguments: source[open+1 : close]})
			index = end
		default:
			index++
		}
	}
	return calls, nil
}

func findHoistedDeclarations(source string) ([]hoistedDeclaration, error) {
	var declarations []hoistedDeclaration
	depth := 0
	for index := 0; index < len(source); {
		switch {
		case source[index] == '\'' || source[index] == '"' || source[index] == '`':
			index = skipSourceString(source, index, source[index])
		case strings.HasPrefix(source[index:], "//"):
			index = skipLineComment(source, index)
		case strings.HasPrefix(source[index:], "/*"):
			index = skipBlockComment(source, index)
		case source[index] == '{':
			depth++
			index++
		case source[index] == '}':
			depth--
			index++
		case depth == 0 && isVariableDeclarationKeyword(source, index):
			lineEnd := strings.IndexAny(source[index:], "\r\n")
			if lineEnd == -1 {
				lineEnd = len(source)
			} else {
				lineEnd += index
			}
			line := source[index:lineEnd]
			relative := strings.Index(line, "vi.hoisted")
			if relative == -1 || isIdentifierByte(byteBefore(line, relative)) || isIdentifierByte(byteAt(line, relative+len("vi.hoisted"))) {
				index++
				continue
			}
			open := index + relative + len("vi.hoisted")
			for open < len(source) && isSourceSpace(source[open]) {
				open++
			}
			if open >= len(source) || source[open] != '(' {
				index++
				continue
			}
			close, err := findClosingParenthesis(source, open)
			if err != nil {
				return nil, err
			}
			end := close + 1
			for end < len(source) && source[end] != '\n' && source[end] != '\r' {
				end++
			}
			trimmedEnd := end
			for trimmedEnd > close+1 && (source[trimmedEnd-1] == ' ' || source[trimmedEnd-1] == '\t') {
				trimmedEnd--
			}
			declarations = append(declarations, hoistedDeclaration{
				start:  index,
				end:    end,
				source: strings.TrimSpace(source[index:trimmedEnd]),
			})
			index = end
		default:
			index++
		}
	}
	return declarations, nil
}

func isVariableDeclarationKeyword(source string, index int) bool {
	for _, keyword := range []string{"const", "let", "var"} {
		if strings.HasPrefix(source[index:], keyword) && !isIdentifierByte(byteBefore(source, index)) && !isIdentifierByte(byteAt(source, index+len(keyword))) {
			return true
		}
	}
	return false
}

func rewriteHoistedVI(source string) string {
	var result strings.Builder
	result.Grow(len(source) + 16)
	for index := 0; index < len(source); {
		switch {
		case source[index] == '\'' || source[index] == '"' || source[index] == '`':
			end := skipSourceString(source, index, source[index])
			result.WriteString(source[index:end])
			index = end
		case strings.HasPrefix(source[index:], "//"):
			end := skipLineComment(source, index)
			result.WriteString(source[index:end])
			index = end
		case strings.HasPrefix(source[index:], "/*"):
			end := skipBlockComment(source, index)
			result.WriteString(source[index:end])
			index = end
		case strings.HasPrefix(source[index:], "vi") && !isIdentifierByte(byteBefore(source, index)) && !isIdentifierByte(byteAt(source, index+2)):
			result.WriteString("__rushVi")
			index += 2
		default:
			result.WriteByte(source[index])
			index++
		}
	}
	return result.String()
}

func findStaticImports(source string) ([]sourceReplacement, error) {
	var imports []sourceReplacement
	depth := 0
	for index := 0; index < len(source); {
		switch {
		case source[index] == '\'' || source[index] == '"' || source[index] == '`':
			index = skipSourceString(source, index, source[index])
		case strings.HasPrefix(source[index:], "//"):
			index = skipLineComment(source, index)
		case strings.HasPrefix(source[index:], "/*"):
			index = skipBlockComment(source, index)
		case source[index] == '{':
			depth++
			index++
		case source[index] == '}':
			depth--
			index++
		case depth == 0 && strings.HasPrefix(source[index:], "import") && !isIdentifierByte(byteBefore(source, index)) && !isIdentifierByte(byteAt(source, index+6)):
			cursor := index + 6
			for cursor < len(source) && isSourceSpace(source[cursor]) {
				cursor++
			}
			if cursor < len(source) && (source[cursor] == '(' || source[cursor] == '.') {
				index = cursor + 1
				continue
			}
			end, err := findImportEnd(source, index)
			if err != nil {
				return nil, err
			}
			imports = append(imports, sourceReplacement{start: index, end: end})
			index = end
		default:
			index++
		}
	}
	return imports, nil
}

func findImportEnd(source string, start int) (int, error) {
	braces, brackets, parentheses := 0, 0, 0
	for index := start + len("import"); index < len(source); index++ {
		switch source[index] {
		case '\'', '"', '`':
			index = skipSourceString(source, index, source[index]) - 1
		case '/':
			if strings.HasPrefix(source[index:], "//") {
				return skipLineComment(source, index), nil
			}
			if strings.HasPrefix(source[index:], "/*") {
				index = skipBlockComment(source, index) - 1
			}
		case '{':
			braces++
		case '}':
			braces--
		case '[':
			brackets++
		case ']':
			brackets--
		case '(':
			parentheses++
		case ')':
			parentheses--
		case ';':
			if braces == 0 && brackets == 0 && parentheses == 0 {
				return index + 1, nil
			}
		case '\n', '\r':
			if braces == 0 && brackets == 0 && parentheses == 0 {
				return index, nil
			}
		}
	}
	return len(source), nil
}

func rewriteStaticImport(statement string) (string, error) {
	sourceID, err := staticImportSource(statement)
	if err != nil {
		return "", err
	}
	return rewriteStaticImportWithID(statement, sourceID)
}

func rewriteStaticImportWithID(statement, runtimeID string) (string, error) {
	trimmed := strings.TrimSpace(strings.TrimSuffix(statement, ";"))
	if strings.HasPrefix(trimmed, "import type ") {
		return "", nil
	}
	if len(trimmed) > len("import ") && (trimmed[len("import ")] == '\'' || trimmed[len("import ")] == '"') {
		sourceID, err := quotedImportSource(trimmed[len("import "):])
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("await __rushImport__(%q, () => import(%q));", runtimeID, sourceID), nil
	}
	from := strings.LastIndex(trimmed, " from ")
	if from == -1 {
		return "", fmt.Errorf("unsupported static import while hoisting mocks: %s", trimmed)
	}
	clause := strings.TrimSpace(strings.TrimPrefix(trimmed[:from], "import"))
	sourceID, err := quotedImportSource(strings.TrimSpace(trimmed[from+len(" from "):]))
	if err != nil {
		return "", err
	}
	loader := fmt.Sprintf("await __rushImport__(%q, () => import(%q))", runtimeID, sourceID)
	if strings.HasPrefix(clause, "*") {
		parts := strings.Fields(clause)
		if len(parts) != 3 || parts[0] != "*" || parts[1] != "as" {
			return "", fmt.Errorf("unsupported namespace import: %s", clause)
		}
		return "const " + parts[2] + " = " + loader + ";", nil
	}

	comma := findTopLevelComma(clause)
	defaultBinding := ""
	namedClause := ""
	if strings.HasPrefix(clause, "{") {
		namedClause = clause
	} else if comma == -1 {
		defaultBinding = clause
	} else {
		defaultBinding = strings.TrimSpace(clause[:comma])
		namedClause = strings.TrimSpace(clause[comma+1:])
	}
	bindings := make([]string, 0, 4)
	if defaultBinding != "" {
		bindings = append(bindings, "default: "+defaultBinding)
	}
	if namedClause != "" {
		inner := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(namedClause, "{"), "}"))
		for _, raw := range strings.Split(inner, ",") {
			binding := strings.TrimSpace(raw)
			if binding == "" || strings.HasPrefix(binding, "type ") {
				continue
			}
			parts := strings.Split(binding, " as ")
			if len(parts) == 2 {
				binding = strings.TrimSpace(parts[0]) + ": " + strings.TrimSpace(parts[1])
			}
			bindings = append(bindings, binding)
		}
	}
	return "const { " + strings.Join(bindings, ", ") + " } = " + loader + ";", nil
}

func staticImportSource(statement string) (string, error) {
	trimmed := strings.TrimSpace(strings.TrimSuffix(statement, ";"))
	if strings.HasPrefix(trimmed, "import type ") {
		trimmed = strings.TrimPrefix(trimmed, "import type ")
	}
	if strings.HasPrefix(trimmed, "import ") {
		trimmed = strings.TrimPrefix(trimmed, "import ")
	}
	if len(trimmed) > 0 && (trimmed[0] == '\'' || trimmed[0] == '"') {
		return quotedImportSource(trimmed)
	}
	from := strings.LastIndex(trimmed, " from ")
	if from == -1 {
		return "", fmt.Errorf("unsupported static import while hoisting mocks: %s", strings.TrimSpace(statement))
	}
	return quotedImportSource(strings.TrimSpace(trimmed[from+len(" from "):]))
}

func mockModuleID(arguments string) (string, error) {
	return quotedImportSource(strings.TrimSpace(arguments))
}

func replaceFirstStringArgument(arguments, id string) (string, error) {
	start := 0
	for start < len(arguments) && isSourceSpace(arguments[start]) {
		start++
	}
	if start >= len(arguments) || (arguments[start] != '\'' && arguments[start] != '"') {
		return "", errors.New("statically hoisted vi.mock requires a string module id")
	}
	end := skipSourceString(arguments, start, arguments[start])
	if end > len(arguments) {
		return "", errors.New("unterminated mock module id")
	}
	return arguments[:start] + fmt.Sprintf("%q", id) + arguments[end:], nil
}

func quotedImportSource(value string) (string, error) {
	value = strings.TrimSpace(value)
	if len(value) < 2 || (value[0] != '\'' && value[0] != '"') {
		return "", fmt.Errorf("unsupported import source: %s", value)
	}
	quote := value[0]
	end := 1
	for end < len(value) && value[end] != quote {
		if value[end] == '\\' {
			end++
		}
		end++
	}
	if end >= len(value) {
		return "", errors.New("unterminated import source")
	}
	return value[1:end], nil
}

func findTopLevelComma(value string) int {
	depth := 0
	for index := 0; index < len(value); index++ {
		switch value[index] {
		case '{':
			depth++
		case '}':
			depth--
		case ',':
			if depth == 0 {
				return index
			}
		}
	}
	return -1
}

func findClosingParenthesis(source string, open int) (int, error) {
	depth := 1
	for index := open + 1; index < len(source); index++ {
		switch {
		case source[index] == '\'' || source[index] == '"' || source[index] == '`':
			index = skipSourceString(source, index, source[index]) - 1
		case strings.HasPrefix(source[index:], "//"):
			index = skipLineComment(source, index) - 1
		case strings.HasPrefix(source[index:], "/*"):
			index = skipBlockComment(source, index) - 1
		case source[index] == '(':
			depth++
		case source[index] == ')':
			depth--
			if depth == 0 {
				return index, nil
			}
		}
	}
	return 0, errors.New("unterminated vi.mock() call")
}

func skipSourceString(source string, start int, quote byte) int {
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

func skipLineComment(source string, start int) int {
	if end := strings.IndexByte(source[start:], '\n'); end != -1 {
		return start + end
	}
	return len(source)
}

func skipBlockComment(source string, start int) int {
	if end := strings.Index(source[start+2:], "*/"); end != -1 {
		return start + 2 + end + 2
	}
	return len(source)
}

func isIdentifierByte(value byte) bool {
	return value == '$' || value == '_' || value >= '0' && value <= '9' || value >= 'A' && value <= 'Z' || value >= 'a' && value <= 'z'
}

func byteBefore(value string, index int) byte {
	if index == 0 {
		return 0
	}
	return value[index-1]
}

func byteAt(value string, index int) byte {
	if index >= len(value) {
		return 0
	}
	return value[index]
}

func isSourceSpace(value byte) bool {
	return value == ' ' || value == '\t' || value == '\n' || value == '\r'
}
