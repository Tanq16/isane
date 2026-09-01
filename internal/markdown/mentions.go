package markdown

import (
	"regexp"
	"strings"
)

const (
	maxHandleLen    = 32
	channelKeyword  = "channel"
	minFenceMarkers = 3
)

var (
	mentionRe    = regexp.MustCompile(`(?i)(^|\W)@([a-z0-9][a-z0-9_-]*)`)
	linkTargetRe = regexp.MustCompile(`\]\([^)\n]*\)`)
)

func Mentions(body string) ([]string, bool) {
	masked := maskUnscannable(body)
	var handles []string
	seen := make(map[string]bool)
	channel := false
	for _, match := range mentionRe.FindAllStringSubmatch(masked, -1) {
		handle := strings.ToLower(match[2])
		if len(handle) > maxHandleLen {
			continue
		}
		if handle == channelKeyword {
			channel = true
			continue
		}
		if seen[handle] {
			continue
		}
		seen[handle] = true
		handles = append(handles, handle)
	}
	return handles, channel
}

func maskUnscannable(body string) string {
	masked := maskFences(body)
	masked = maskCodeSpans(masked)
	return linkTargetRe.ReplaceAllStringFunc(masked, func(s string) string {
		return strings.Repeat(" ", len(s))
	})
}

func maskFences(body string) string {
	out := []byte(body)
	open := ""
	for pos := 0; pos <= len(body); {
		end := len(body)
		if i := strings.IndexByte(body[pos:], '\n'); i >= 0 {
			end = pos + i
		}
		marker := fenceMarker(body[pos:end])
		if open == "" {
			if marker != "" {
				open = marker
				blankRange(out, pos, end)
			}
		} else {
			blankRange(out, pos, end)
			if marker != "" && marker[0] == open[0] && len(marker) >= len(open) {
				open = ""
			}
		}
		if end == len(body) {
			break
		}
		pos = end + 1
	}
	return string(out)
}

func fenceMarker(line string) string {
	start := 0
	for start < len(line) && start < 3 && line[start] == ' ' {
		start++
	}
	if start >= len(line) || (line[start] != '`' && line[start] != '~') {
		return ""
	}
	end := start
	for end < len(line) && line[end] == line[start] {
		end++
	}
	if end-start < minFenceMarkers {
		return ""
	}
	return line[start:end]
}

func maskCodeSpans(body string) string {
	out := []byte(body)
	for i := 0; i < len(out); {
		if out[i] != '`' {
			i++
			continue
		}
		start := i
		for i < len(out) && out[i] == '`' {
			i++
		}
		closeEnd := findTickRun(out, i, i-start)
		if closeEnd < 0 {
			continue
		}
		blankRange(out, start, closeEnd)
		i = closeEnd
	}
	return string(out)
}

func findTickRun(b []byte, from, width int) int {
	for i := from; i < len(b); {
		if b[i] != '`' {
			i++
			continue
		}
		start := i
		for i < len(b) && b[i] == '`' {
			i++
		}
		if i-start >= width {
			return i
		}
	}
	return -1
}

func blankRange(b []byte, from, to int) {
	for i := from; i < to; i++ {
		b[i] = ' '
	}
}
