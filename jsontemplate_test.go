package berghain

import (
	"bytes"
	"testing"

	"github.com/dropmorepackets/haproxy-go/pkg/buffer"
)

func Test_slotLocator(t *testing.T) {
	loc := slotLocator{doc: `{"c":0,"r":"0000ID","s":"0000"}`}

	countdown := loc.next("0")
	random := loc.next("0000ID")
	sum := loc.next("0000")

	doc := []byte(loc.doc)
	countdown(doc)[0] = '7'
	copy(random(doc), "1234id")
	copy(sum(doc), "ffff")

	want := `{"c":7,"r":"1234id","s":"ffff"}`
	if string(doc) != want {
		t.Errorf("rendered doc = %q, want %q", doc, want)
	}
}

func Test_slotLocator_missingPlaceholder(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Errorf("expected panic for missing placeholder")
		}
	}()

	loc := slotLocator{doc: `{"a":"x"}`}
	loc.next("x")
	// "x" only occurs before the cursor now; sequential search must panic.
	loc.next("x")
}

func Test_powChallengeTemplate_zeroAlloc(t *testing.T) {
	body := buffer.NewSliceBuffer(1024)
	supportID := []byte("bh@123e4567-e89b-12d3-a456-426614174000")

	allocs := testing.AllocsPerRun(100, func() {
		body.Reset()

		tpl := &validatorPOWChallenge
		doc := tpl.Render(body)
		tpl.Countdown(doc)[0] = '3'
		copy(tpl.Random(doc)[len(validatorPOWTimestamp):], supportID)
		copy(tpl.SupportID(doc), supportID)
	})
	if allocs != 0 {
		t.Errorf("render allocates: %v allocs per run, want 0", allocs)
	}

	if !bytes.Contains(body.ReadBytes(), supportID) {
		t.Errorf("rendered document misses the support ID: %q", body.ReadBytes())
	}
}
