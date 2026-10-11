// Package instructions manages the AgentBrief block inside AGENTS.md, keeps
// import shims in CLAUDE.md / GEMINI.md, and lints agent-instruction files.
package instructions

import (
	"errors"
	"fmt"
	"strings"
)

// Managed block markers.
const (
	StartMarker = "<!-- agentbrief:start -->"
	EndMarker   = "<!-- agentbrief:end -->"
)

// Block locates the managed block inside a document. Offsets are byte offsets
// into the original text: Start is the first byte of the start marker and End is
// the byte after the end marker (excluding its line terminator).
type Block struct {
	Start, End int
	// StartLine is the 1-based line of the start marker.
	StartLine int
}

// ErrNoBlock is returned by FindBlock when the text has no markers at all.
var ErrNoBlock = errors.New("no managed block")

// FindBlock finds the managed block. It returns ErrNoBlock when there are no
// markers, and a descriptive error for malformed markers (missing end marker,
// missing start marker, duplicates, wrong order). Markers inside fenced code
// blocks that precede the block are ignored. CRLF is handled.
func FindBlock(text string) (Block, error) {
	var (
		fence            string
		offset           int
		lineNo           int
		starts, ends     int
		startOff, endOff = -1, -1
		startLine        int
		startPos         int
		endPos           = -1
	)
	rest := text
	for len(rest) > 0 {
		lineNo++
		nl := strings.IndexByte(rest, '\n')
		var raw string
		adv := 0
		if nl < 0 {
			raw, adv = rest, len(rest)
		} else {
			raw, adv = rest[:nl], nl+1
		}
		line := strings.TrimRight(raw, "\r")
		t := strings.TrimSpace(line)
		inside := startOff >= 0 && endOff < 0
		switch {
		case !inside && fence != "":
			if strings.HasPrefix(t, fence) && strings.Trim(t, fence[:1]) == "" {
				fence = ""
			}
		case !inside && (strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~")):
			c := t[:1]
			fence = strings.Repeat(c, len(t)-len(strings.TrimLeft(t, c)))
		case t == StartMarker:
			starts++
			if startOff < 0 {
				startOff = offset
				startLine = lineNo
				startPos = offset + strings.Index(line, StartMarker)
			}
		case t == EndMarker:
			ends++
			if endOff < 0 && startOff >= 0 {
				endOff = offset
				endPos = offset + strings.Index(line, EndMarker) + len(EndMarker)
			}
		}
		offset += adv
		rest = rest[adv:]
	}
	switch {
	case starts == 0 && ends == 0:
		return Block{}, ErrNoBlock
	case starts > 1:
		return Block{}, fmt.Errorf("duplicate %s markers (found %d)", StartMarker, starts)
	case ends > 1:
		return Block{}, fmt.Errorf("duplicate %s markers (found %d)", EndMarker, ends)
	case starts == 0:
		return Block{}, fmt.Errorf("%s found without a matching %s", EndMarker, StartMarker)
	case ends == 0:
		return Block{}, fmt.Errorf("%s at line %d has no matching %s", StartMarker, startLine, EndMarker)
	}
	if endPos < 0 {
		return Block{}, fmt.Errorf("%s appears before %s", EndMarker, StartMarker)
	}
	return Block{Start: startPos, End: endPos, StartLine: startLine}, nil
}

// detectEOL returns "\r\n" if the text predominantly uses CRLF, else "\n".
func detectEOL(text string) string {
	crlf := strings.Count(text, "\r\n")
	lf := strings.Count(text, "\n") - crlf
	if crlf > 0 && crlf >= lf {
		return "\r\n"
	}
	return "\n"
}

func toEOL(s, eol string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	if eol == "\n" {
		return s
	}
	return strings.ReplaceAll(s, "\n", eol)
}

func normalizeEOL(s string) string { return strings.ReplaceAll(s, "\r\n", "\n") }

// BlockText returns the managed block found in text (markers included, LF
// normalised), or an error as for FindBlock.
func BlockText(text string) (string, error) {
	b, err := FindBlock(text)
	if err != nil {
		return "", err
	}
	return normalizeEOL(text[b.Start:b.End]), nil
}

// ReplaceBlock replaces the managed block in text with block (LF-separated,
// converted to the document's line-ending style).
func ReplaceBlock(text, block string) (string, error) {
	b, err := FindBlock(text)
	if err != nil {
		return "", err
	}
	return text[:b.Start] + toEOL(block, detectEOL(text)) + text[b.End:], nil
}
