package input

import (
	"bytes"
	"testing"
)

func TestReadEventsFeedsTheDetector(t *testing.T) {
	// What a remote sends for: press Exit, auto-repeat, release; with the
	// synchronisation events the kernel adds, and a mouse movement in between.
	var stream bytes.Buffer
	for _, e := range [][]byte{
		encodeEvent(evKey, keyExit, 1), encodeEvent(evSyn, 0, 0),
		encodeEvent(evRel, relX, 5), encodeEvent(evSyn, 0, 0),
		encodeEvent(evKey, keyExit, 2), encodeEvent(evSyn, 0, 0),
		encodeEvent(evKey, keyExit, 0), encodeEvent(evSyn, 0, 0),
	} {
		stream.Write(e)
	}
	type key struct {
		code  uint16
		value int32
	}
	var got []key
	readEvents(&stream, func(code uint16, value int32) { got = append(got, key{code, value}) })
	want := []key{{keyExit, 1}, {keyExit, 2}, {keyExit, 0}}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("event %d = %v, want %v", i, got[i], want[i])
		}
	}
}

func TestEventEncoding(t *testing.T) {
	b := encodeEvent(evRel, relY, -7)
	if len(b) != eventSize {
		t.Fatalf("event is %d bytes, want %d", len(b), eventSize)
	}
	typ, code, value := decodeEvent(b)
	if typ != evRel || code != relY || value != -7 {
		t.Errorf("round trip = %d %d %d", typ, code, value)
	}
	for _, c := range b[:eventSize-8] {
		if c != 0 {
			t.Error("the timestamp must be left zero for the kernel to fill in")
		}
	}
}
