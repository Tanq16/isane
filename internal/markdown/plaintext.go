package markdown

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

var (
	fenceLineRe       = regexp.MustCompile("(?m)^[ \t]{0,3}(?:`{3,}|~{3,})[^\n]*$")
	headingRe         = regexp.MustCompile(`(?m)^[ \t]{0,3}#{1,6}[ \t]+`)
	imageRe           = regexp.MustCompile(`!\[([^\]]*)\]\([^)\n]*\)`)
	inlineLinkRe      = regexp.MustCompile(`\[([^\]]*)\]\([^)\n]*\)`)
	refLinkRe         = regexp.MustCompile(`\[([^\]]*)\]\[[^\]\n]*\]`)
	autoLinkRe        = regexp.MustCompile(`<((?:https?|mailto):[^>\s]*)>`)
	emphasisRe        = regexp.MustCompile("[*`~]+")
	underscoreLeadRe  = regexp.MustCompile(`(^|\s)_+`)
	underscoreTrailRe = regexp.MustCompile(`_+($|\s)`)
	whitespaceRe      = regexp.MustCompile(`\s+`)
)

func PlainText(body string) string {
	s := fenceLineRe.ReplaceAllString(body, "")
	s = headingRe.ReplaceAllString(s, "")
	s = imageRe.ReplaceAllString(s, "${1}")
	s = inlineLinkRe.ReplaceAllString(s, "${1}")
	s = refLinkRe.ReplaceAllString(s, "${1}")
	s = autoLinkRe.ReplaceAllString(s, "${1}")
	s = emphasisRe.ReplaceAllString(s, "")
	s = underscoreLeadRe.ReplaceAllString(s, "${1}")
	s = underscoreTrailRe.ReplaceAllString(s, "${1}")
	s = whitespaceRe.ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}

func Truncate(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	kept := 0
	for i := range s {
		if kept == n-1 {
			return s[:i] + "…"
		}
		kept++
	}
	return s
}
