package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// ParseError provides rich diagnostic information when configuration parsing fails.
type ParseError struct {
	Line    int    `json:"line"`
	Column  int    `json:"column"`
	Message string `json:"message"`
	Snippet string `json:"snippet"`
}

func (e *ParseError) Error() string {
	if e.Line > 0 {
		if e.Snippet != "" {
			return fmt.Sprintf("config parse error at line %d, col %d: %s (snippet: %q)", e.Line, e.Column, e.Message, e.Snippet)
		}
		return fmt.Sprintf("config parse error at line %d, col %d: %s", e.Line, e.Column, e.Message)
	}
	return fmt.Sprintf("config parse error: %s", e.Message)
}

// Parse parses configuration data in either YAML or JSON format into a GatewayConfig.
func Parse(data []byte) (*GatewayConfig, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return nil, &ParseError{Line: 1, Column: 1, Message: "configuration data is empty"}
	}

	// Detect format: JSON starts with '{' or '['
	if trimmed[0] == '{' || trimmed[0] == '[' {
		return parseJSON(trimmed)
	}

	return parseYAML(data)
}

// ParseFile reads a configuration file from disk and parses it into a GatewayConfig.
func ParseFile(filePath string) (*GatewayConfig, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read configuration file %q: %w", filePath, err)
	}
	cfg, err := Parse(data)
	if err != nil {
		return nil, fmt.Errorf("failed to parse configuration file %q: %w", filePath, err)
	}
	return cfg, nil
}

// parseJSON unmarshals a JSON configuration payload into GatewayConfig, normalizing duration strings.
func parseJSON(data []byte) (*GatewayConfig, error) {
	var raw map[string]any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&raw); err != nil {
		return nil, &ParseError{Line: 1, Column: 1, Message: err.Error()}
	}

	if err := normalizeDurations(raw); err != nil {
		return nil, err
	}

	normalizedBytes, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("json remolding failed: %w", err)
	}

	var cfg GatewayConfig
	dec := json.NewDecoder(bytes.NewReader(normalizedBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&cfg); err != nil {
		return nil, &ParseError{Line: 1, Column: 1, Message: err.Error()}
	}

	return &cfg, nil
}

// durationKeys identifies configuration keys that represent time.Duration values.
var durationKeys = map[string]bool{
	"read_timeout":        true,
	"write_timeout":       true,
	"idle_timeout":        true,
	"read_header_timeout": true,
	"timeout":             true,
	"interval":            true,
	"reset_timeout":       true,
	"idle_sleep_timeout":  true,
	"snapshot_interval":   true,
	"delay":               true,
}

// normalizeDurations walks an unmarshaled AST and converts string durations ("5s", "100ms") to int64 nanoseconds.
func normalizeDurations(val any) error {
	switch node := val.(type) {
	case map[string]any:
		for k, v := range node {
			if durationKeys[k] {
				switch durVal := v.(type) {
				case string:
					d, err := time.ParseDuration(durVal)
					if err != nil {
						return &ParseError{
							Line:    0,
							Column:  0,
							Message: fmt.Sprintf("invalid duration format %q for field %q: %v", durVal, k, err),
							Snippet: durVal,
						}
					}
					node[k] = int64(d)
				case json.Number:
					num, err := durVal.Int64()
					if err != nil {
						return &ParseError{
							Line:    0,
							Column:  0,
							Message: fmt.Sprintf("invalid numeric duration for field %q: %v", k, err),
						}
					}
					node[k] = num
				case float64:
					node[k] = int64(durVal)
				case int64:
					// already int64 nanoseconds
				default:
					return &ParseError{
						Line:    0,
						Column:  0,
						Message: fmt.Sprintf("unexpected type %T for duration field %q", v, k),
					}
				}
			} else {
				if err := normalizeDurations(v); err != nil {
					return err
				}
			}
		}
	case []any:
		for _, item := range node {
			if err := normalizeDurations(item); err != nil {
				return err
			}
		}
	}
	return nil
}

// parseYAML parses a YAML document into a GatewayConfig using an indentation-based line lexer.
func parseYAML(data []byte) (*GatewayConfig, error) {
	lines := strings.Split(string(data), "\n")
	ast, err := parseYAMLLines(lines)
	if err != nil {
		return nil, err
	}

	if err := normalizeDurations(ast); err != nil {
		return nil, err
	}

	jsonBytes, err := json.Marshal(ast)
	if err != nil {
		return nil, &ParseError{Message: fmt.Sprintf("yaml-to-json translation failed: %v", err)}
	}

	var cfg GatewayConfig
	dec := json.NewDecoder(bytes.NewReader(jsonBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&cfg); err != nil {
		return nil, &ParseError{Message: err.Error()}
	}

	return &cfg, nil
}

// yamlLine holds tokenized metadata for a single line of YAML.
type yamlLine struct {
	lineNum int
	indent  int
	raw     string
	content string
}

// parseYAMLLines parses indented YAML lines into a generic Go map/slice tree.
func parseYAMLLines(rawLines []string) (any, error) {
	var parsed []yamlLine
	for i, r := range rawLines {
		lineNum := i + 1

		// Check for forbidden tab characters in indentation
		trimmedLeft := strings.TrimLeft(r, " ")
		leadingSpaces := len(r) - len(trimmedLeft)
		if strings.HasPrefix(trimmedLeft, "\t") {
			return nil, &ParseError{
				Line:    lineNum,
				Column:  leadingSpaces + 1,
				Message: "tabs are not allowed for indentation in YAML; use spaces",
				Snippet: r,
			}
		}

		// Strip inline comments starting with '#'
		cleanContent := stripYAMLComment(trimmedLeft)
		if cleanContent == "" {
			continue // Skip blank lines and full comment lines
		}

		parsed = append(parsed, yamlLine{
			lineNum: lineNum,
			indent:  leadingSpaces,
			raw:     r,
			content: cleanContent,
		})
	}

	if len(parsed) == 0 {
		return nil, &ParseError{Line: 1, Column: 1, Message: "configuration data contains only comments or blank lines"}
	}

	idx := 0
	res, _, err := parseYAMLBlock(parsed, idx, 0)
	return res, err
}

func parseYAMLBlock(lines []yamlLine, startIdx int, currentIndent int) (any, int, error) {
	if startIdx >= len(lines) {
		return nil, startIdx, nil
	}

	firstLine := lines[startIdx]
	if strings.HasPrefix(firstLine.content, "- ") || firstLine.content == "-" {
		// Sequence / List block
		return parseYAMLSequence(lines, startIdx, currentIndent)
	}

	// Mapping / Object block
	return parseYAMLMapping(lines, startIdx, currentIndent)
}

func parseYAMLMapping(lines []yamlLine, startIdx int, minIndent int) (map[string]any, int, error) {
	result := make(map[string]any)
	i := startIdx

	for i < len(lines) {
		line := lines[i]
		if line.indent < minIndent {
			// dedent: return to parent block
			break
		}

		// Split on the first unquoted colon
		colonIdx := findYAMLColon(line.content)
		if colonIdx == -1 {
			return nil, i, &ParseError{
				Line:    line.lineNum,
				Column:  line.indent + 1,
				Message: "expected mapping key-value pair separated by ':'",
				Snippet: line.raw,
			}
		}

		key := strings.TrimSpace(line.content[:colonIdx])
		key = unquoteYAML(key)
		valStr := strings.TrimSpace(line.content[colonIdx+1:])

		if valStr == "" {
			// Nested block or sequence following key
			if i+1 < len(lines) && lines[i+1].indent > line.indent {
				childVal, nextIdx, err := parseYAMLBlock(lines, i+1, lines[i+1].indent)
				if err != nil {
					return nil, i, err
				}
				result[key] = childVal
				i = nextIdx
				continue
			} else {
				// Empty mapping value
				result[key] = nil
				i++
				continue
			}
		} else {
			// Scalar value or inline list
			parsedVal := parseYAMLScalar(valStr)
			result[key] = parsedVal
			i++
		}
	}

	return result, i, nil
}

func parseYAMLSequence(lines []yamlLine, startIdx int, minIndent int) ([]any, int, error) {
	var result []any
	i := startIdx

	for i < len(lines) {
		line := lines[i]
		if line.indent < minIndent {
			break
		}

		if !strings.HasPrefix(line.content, "- ") && line.content != "-" {
			// Not a sequence item at this level
			break
		}

		itemContent := strings.TrimSpace(strings.TrimPrefix(line.content, "-"))
		if itemContent == "" {
			// Item value is defined on next indented lines
			if i+1 < len(lines) && lines[i+1].indent > line.indent {
				childVal, nextIdx, err := parseYAMLBlock(lines, i+1, lines[i+1].indent)
				if err != nil {
					return nil, i, err
				}
				result = append(result, childVal)
				i = nextIdx
				continue
			} else {
				result = append(result, nil)
				i++
				continue
			}
		}

		// Check if the item content is a key: value map directly on the hyphen line (e.g. "- id: route1")
		colonIdx := findYAMLColon(itemContent)
		if colonIdx != -1 {
			// First key of a mapping item
			subLines := []yamlLine{
				{
					lineNum: line.lineNum,
					indent:  line.indent + 2,
					raw:     itemContent,
					content: itemContent,
				},
			}

			// Gather following lines belonging to this list item's map
			next := i + 1
			for next < len(lines) {
				if lines[next].indent <= line.indent {
					break
				}
				subLines = append(subLines, lines[next])
				next++
			}

			itemMap, _, err := parseYAMLMapping(subLines, 0, line.indent+2)
			if err != nil {
				return nil, i, err
			}
			result = append(result, itemMap)
			i = next
			continue
		}

		// Scalar list item (e.g. "- GET", "- 127.0.0.1/32")
		result = append(result, parseYAMLScalar(itemContent))
		i++
	}

	return result, i, nil
}

// findYAMLColon finds the first colon that is outside quotes.
func findYAMLColon(s string) int {
	inDouble := false
	inSingle := false

	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '"' && !inSingle {
			inDouble = !inDouble
		} else if c == '\'' && !inDouble {
			inSingle = !inSingle
		} else if c == ':' && !inDouble && !inSingle {
			// Colon must be followed by a space, tab, or end of string to be a YAML mapping separator
			if i+1 == len(s) || s[i+1] == ' ' || s[i+1] == '\t' {
				return i
			}
		}
	}
	return -1
}

// stripYAMLComment removes comments starting with '#' outside quotes.
func stripYAMLComment(s string) string {
	inDouble := false
	inSingle := false

	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '"' && !inSingle {
			inDouble = !inDouble
		} else if c == '\'' && !inDouble {
			inSingle = !inSingle
		} else if c == '#' && !inDouble && !inSingle {
			// Found comment delimiter
			return strings.TrimSpace(s[:i])
		}
	}
	return strings.TrimSpace(s)
}

// unquoteYAML strips surrounding single or double quotes.
func unquoteYAML(s string) string {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

// parseYAMLScalar converts an unquoted/quoted string scalar into boolean, integer, float, or string.
func parseYAMLScalar(s string) any {
	if s == "" {
		return nil
	}

	// Quoted string
	if (strings.HasPrefix(s, "\"") && strings.HasSuffix(s, "\"")) ||
		(strings.HasPrefix(s, "'") && strings.HasSuffix(s, "'")) {
		return unquoteYAML(s)
	}

	// Inline sequence: [GET, POST]
	if strings.HasPrefix(s, "[") && strings.HasSuffix(s, "]") {
		inner := strings.TrimSpace(s[1 : len(s)-1])
		if inner == "" {
			return []any{}
		}
		parts := strings.Split(inner, ",")
		var arr []any
		for _, p := range parts {
			arr = append(arr, parseYAMLScalar(strings.TrimSpace(p)))
		}
		return arr
	}

	// Booleans
	lower := strings.ToLower(s)
	if lower == "true" || lower == "yes" || lower == "on" {
		return true
	}
	if lower == "false" || lower == "no" || lower == "off" {
		return false
	}
	if lower == "null" || lower == "~" {
		return nil
	}

	// Integer
	if intVal, err := strconv.ParseInt(s, 10, 64); err == nil {
		return intVal
	}

	// Float
	if floatVal, err := strconv.ParseFloat(s, 64); err == nil {
		return floatVal
	}

	// String
	return s
}
