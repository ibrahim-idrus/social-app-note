package store

import (
	"regexp"
	"strings"
	"unicode"
)

type MatchRange struct {
	Start int `json:"start"`
	End   int `json:"end"`
}
type NoteSearchSection struct {
	Heading     string       `json:"heading"`
	HeadingPath []string     `json:"heading_path"`
	Anchor      string       `json:"anchor"`
	Excerpt     string       `json:"excerpt"`
	Matches     []MatchRange `json:"matches"`
}

type markdownSection struct {
	heading string
	path    []string
	body    string
}

var headingPattern = regexp.MustCompile(`^(#{1,6})\s+(.+?)\s*#*\s*$`)
var linkPattern = regexp.MustCompile(`\[([^]]+)\]\([^)]*\)`)

func searchSections(markdown, query string) []NoteSearchSection {
	sections := splitSections(markdown)
	result := make([]NoteSearchSection, 0, 3)
	for _, section := range sections {
		plain := markdownPlainText(section.body)
		if len(matchRanges(plain, query)) == 0 {
			continue
		}
		excerpt, matches := excerptAround(plain, query, 180)
		result = append(result, NoteSearchSection{section.heading, section.path, slugHeading(section.heading), excerpt, matches})
		if len(result) == 3 {
			break
		}
	}
	return result
}

func splitSections(markdown string) []markdownSection {
	sections := []markdownSection{{heading: "Introduction", path: []string{}}}
	stack := []string{}
	fence := ""
	for _, line := range strings.Split(markdown, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			marker := trimmed[:3]
			if fence == "" {
				fence = marker
			} else if marker == fence {
				fence = ""
			}
			continue
		}
		if fence == "" {
			if match := headingPattern.FindStringSubmatch(line); match != nil {
				level, heading := len(match[1]), strings.TrimSpace(match[2])
				if len(stack) >= level {
					stack = stack[:level-1]
				}
				for len(stack) < level-1 {
					stack = append(stack, "")
				}
				stack = append(stack, heading)
				path := append([]string(nil), stack...)
				sections = append(sections, markdownSection{heading: heading, path: path})
				continue
			}
		}
		sections[len(sections)-1].body += line + "\n"
	}
	return sections
}

func matchRanges(text, query string) []MatchRange {
	if query == "" {
		return nil
	}
	runes, needle := []rune(text), []rune(query)
	lower := []rune(strings.ToLower(text))
	target := []rune(strings.ToLower(query))
	result := []MatchRange{}
	for i := 0; i+len(target) <= len(lower); {
		if string(lower[i:i+len(target)]) == string(target) {
			result = append(result, MatchRange{i, i + len(needle)})
			i += len(target)
		} else {
			i++
		}
	}
	_ = runes
	return result
}

func excerptAround(text, query string, limit int) (string, []MatchRange) {
	runes := []rune(strings.TrimSpace(text))
	all := matchRanges(string(runes), query)
	if len(runes) <= limit {
		return string(runes), all
	}
	center := all[0].Start
	start := center - limit/2
	if start < 0 {
		start = 0
	}
	end := start + limit
	if end > len(runes) {
		end = len(runes)
		start = end - limit
	}
	prefix, suffix := "", ""
	if start > 0 {
		prefix = "…"
	}
	if end < len(runes) {
		suffix = "…"
	}
	excerpt := prefix + string(runes[start:end]) + suffix
	offset := start - len([]rune(prefix))
	matches := []MatchRange{}
	for _, m := range all {
		if m.Start >= start && m.End <= end {
			matches = append(matches, MatchRange{m.Start - offset, m.End - offset})
		}
	}
	return excerpt, matches
}

func markdownPlainText(value string) string {
	value = linkPattern.ReplaceAllString(value, "$1")
	value = strings.NewReplacer("**", "", "__", "", "`", "", "*", "", "_", "").Replace(value)
	return strings.Join(strings.Fields(value), " ")
}

func slugHeading(value string) string {
	var out []rune
	dash := false
	for _, r := range strings.ToLower(value) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			out = append(out, r)
			dash = false
		} else if len(out) > 0 && !dash {
			out = append(out, '-')
			dash = true
		}
	}
	return strings.Trim(string(out), "-")
}
