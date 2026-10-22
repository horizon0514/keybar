package clip

import (
	"strings"
	"testing"
)

func TestWatch(t *testing.T) {
	for _, noSeq := range []bool{false, true} {
		f := &Fake{Text: "copied before start", NoSeq: noSeq}
		w := &Watch{B: f}
		if w.Next() != "" {
			t.Error("first look offered")
		}
		reads := f.Reads
		if w.Next() != "" {
			t.Error("unchanged offered")
		}
		if !noSeq && f.Reads != reads {
			t.Error("read without a change")
		}
		f.Set("  hf_dummyDummyDummyDummyDummyDummy12\n", false)
		if got := w.Next(); got != "hf_dummyDummyDummyDummyDummyDummy12" {
			t.Errorf("%q", got)
		}
		if w.Next() != "" {
			t.Error("offered twice")
		}
		f.Set("concealed-dummy", true)
		if w.Next() != "" {
			t.Error("concealed offered")
		}
		f.Set(strings.Repeat("x", Max+1), false)
		if w.Next() != "" {
			t.Error("long offered")
		}
		// the same text copied again after something else: offered again
		f.Set("hf_dummyDummyDummyDummyDummyDummy12", false)
		if w.Next() == "" {
			t.Error("copied again")
		}
	}
}
