package store

import (
	"reflect"
	"strings"
	"testing"
)

func TestSearchSections(t *testing.T) {
	markdown := "intro needle\n\n# Launch\nparent needle\n\n## Requirements\nfirst needle and needle\n\n```\n# fake needle\n```\n\n# Next\nlast needle"
	got := searchSections(markdown, "needle")
	if len(got) != 3 {
		t.Fatalf("sections = %d, want 3: %#v", len(got), got)
	}
	if got[0].Heading != "Introduction" || len(got[0].HeadingPath) != 0 {
		t.Fatalf("introduction = %#v", got[0])
	}
	if got[1].Heading != "Launch" || !reflect.DeepEqual(got[1].HeadingPath, []string{"Launch"}) {
		t.Fatalf("launch = %#v", got[1])
	}
	if got[2].Heading != "Requirements" || !reflect.DeepEqual(got[2].HeadingPath, []string{"Launch", "Requirements"}) {
		t.Fatalf("requirements = %#v", got[2])
	}
	if got[2].Anchor != "requirements" || len(got[2].Matches) != 3 {
		t.Fatalf("requirements metadata = %#v", got[2])
	}
}

func TestSearchSectionsIgnoresFencedHeadingsAndCapsAtThree(t *testing.T) {
	got := searchSections("# One\nneedle\n~~~\n# fake\n~~~\n# Two\nneedle\n# Three\nneedle\n# Four\nneedle", "needle")
	if len(got) != 3 {
		t.Fatalf("sections = %d, want 3", len(got))
	}
	if got[1].Heading != "Two" {
		t.Fatalf("fenced heading changed sections: %#v", got)
	}
}

func TestMatchRangesUseRuneOffsets(t *testing.T) {
	got := matchRanges("東京 東京", "京")
	want := []MatchRange{{Start: 1, End: 2}, {Start: 4, End: 5}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ranges = %#v, want %#v", got, want)
	}
	if got := matchRanges("abc", "x"); len(got) != 0 {
		t.Fatalf("no match = %#v", got)
	}
}

func TestExcerptStaysBoundedAndRebasesMatches(t *testing.T) {
	text := "long prefix long prefix long prefix needle long suffix long suffix long suffix"
	excerpt, matches := excerptAround(text, "needle", 30)
	if len([]rune(excerpt)) > 32 || !strings.HasPrefix(excerpt, "…") || !strings.HasSuffix(excerpt, "…") {
		t.Fatalf("excerpt = %q", excerpt)
	}
	if len(matches) != 1 {
		t.Fatalf("matches = %#v", matches)
	}
	runes := []rune(excerpt)
	if string(runes[matches[0].Start:matches[0].End]) != "needle" {
		t.Fatalf("rebased match = %#v in %q", matches[0], excerpt)
	}
}

func TestPlainTextPreservesLinkAndCodeText(t *testing.T) {
	got := markdownPlainText("Use [camera](https://example.com) and `code` with **bold**.")
	if got != "Use camera and code with bold." {
		t.Fatalf("plain text = %q", got)
	}
}

func TestSlugHeading(t *testing.T) {
	if got := slugHeading("Hello, 東京 World!"); got != "hello-東京-world" {
		t.Fatalf("slug = %q", got)
	}
}
