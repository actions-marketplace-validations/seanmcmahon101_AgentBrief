package instructions

import (
	"errors"
	"strings"
	"testing"
)

func TestFindBlock(t *testing.T) {
	good := "intro\n" + StartMarker + "\nbody\n" + EndMarker + "\noutro\n"
	b, err := FindBlock(good)
	if err != nil || good[b.Start:b.End] != StartMarker+"\nbody\n"+EndMarker || b.StartLine != 2 {
		t.Fatalf("%v %+v", err, b)
	}
	crlf := strings.ReplaceAll(good, "\n", "\r\n")
	b, err = FindBlock(crlf)
	if err != nil || crlf[b.Start:b.End] != StartMarker+"\r\nbody\r\n"+EndMarker {
		t.Fatalf("crlf: %v %+v", err, b)
	}
	if _, err := FindBlock("nothing"); !errors.Is(err, ErrNoBlock) {
		t.Error(err)
	}
	for name, text := range map[string]string{
		"missing end":   StartMarker + "\nx\n",
		"missing start": "x\n" + EndMarker + "\n",
		"dup start":     StartMarker + "\n" + StartMarker + "\n" + EndMarker + "\n",
		"dup end":       StartMarker + "\n" + EndMarker + "\n" + EndMarker + "\n",
		"end first":     EndMarker + "\n" + StartMarker + "\n",
	} {
		if _, err := FindBlock(text); err == nil || errors.Is(err, ErrNoBlock) {
			t.Errorf("%s: want error, got %v", name, err)
		}
	}
}

func TestFindBlockIgnoresFencedMarkers(t *testing.T) {
	doc := "How it works:\n```md\n" + StartMarker + "\nx\n" + EndMarker + "\n```\n\n" + StartMarker + "\nreal\n" + EndMarker + "\n"
	b, err := FindBlock(doc)
	if err != nil || !strings.Contains(doc[b.Start:b.End], "real") {
		t.Fatalf("%v %q", err, doc[b.Start:b.End])
	}
}

func TestReplaceBlockOnlyBetweenMarkers(t *testing.T) {
	doc := "# Mine\r\nhand written\r\n" + StartMarker + "\r\nold\r\n" + EndMarker + "\r\nafter\r\n"
	out, err := ReplaceBlock(doc, StartMarker+"\nnew1\nnew2\n"+EndMarker)
	if err != nil {
		t.Fatal(err)
	}
	want := "# Mine\r\nhand written\r\n" + StartMarker + "\r\nnew1\r\nnew2\r\n" + EndMarker + "\r\nafter\r\n"
	if out != want {
		t.Errorf("got %q", out)
	}
}
