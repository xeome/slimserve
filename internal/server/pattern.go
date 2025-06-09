package server

import (
	"bufio"
	"fmt"
	"io"
	"regexp"
	"strings"
)

// Pattern represents a parsed gitignore pattern.
type Pattern struct {
	Regex  *regexp.Regexp
	Negate bool
}

// Parse parses a .slimserveignore file into a list of patterns.
func Parse(r io.Reader) ([]*Pattern, error) {
	var (
		lineNumber int
		builder    strings.Builder
		patterns   = make([]*Pattern, 0, 20)
		scanner    = bufio.NewScanner(r)
	)

	for scanner.Scan() {
		lineNumber++
		line := strings.TrimSpace(scanner.Text())

		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		negate := false
		if strings.HasPrefix(line, "!") {
			negate = true
			line = line[1:]
		}

		line = strings.TrimPrefix(line, `\`)

		builder.Reset()

		line = strings.ReplaceAll(line, "**/", ".*/")
		line = strings.ReplaceAll(line, "/**", "/.*")
		line = strings.ReplaceAll(line, "*", "[^/]*")
		line = strings.ReplaceAll(line, "?", "[^/]")

		if strings.HasSuffix(line, "/") {
			builder.WriteString(line)
			builder.WriteString(".*")
		} else {
			builder.WriteString(line)
		}

		expr := builder.String()
		if !strings.HasPrefix(expr, ".*/") {
			if strings.HasPrefix(expr, "/") {
				expr = "^" + expr[1:]
			} else {
				expr = "(.*/|^)" + expr
			}
		}

		regex, err := regexp.Compile(expr)
		if err != nil {
			return nil, fmt.Errorf("invalid regex for pattern %q on line %d: %w", expr, lineNumber, err)
		}

		patterns = append(patterns, &Pattern{
			Regex:  regex,
			Negate: negate,
		})
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed to scan file: %w", err)
	}

	return patterns, nil
}
