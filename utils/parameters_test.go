package utils

import (
	"net/url"
	"testing"
)

func TestFormatEmpty(t *testing.T) {
	if got := NewParameters().Format(); got != "" {
		t.Errorf("Format() = %q, want empty string", got)
	}
}

func TestFormatSingleParameter(t *testing.T) {
	p := NewParameters()
	p.Cursor("abc")

	if got, want := p.Format(), "?cursor=abc"; got != want {
		t.Errorf("Format() = %q, want %q", got, want)
	}
}

// TestFormatJoinsWithSingleAmpersand guards the query separator. Joining with
// "&&" produces an empty parameter between each pair, so every parameter after
// the first is silently lost by a standards-compliant server.
func TestFormatJoinsWithSingleAmpersand(t *testing.T) {
	p := NewParameters()
	p.Cursor("CURSOR")
	p.Count(100)
	p.SetAscOrder()

	got := p.Format()
	want := "?cursor=CURSOR&count=100&order=asc"
	if got != want {
		t.Fatalf("Format() = %q, want %q", got, want)
	}

	// The result must parse back to exactly the parameters that went in.
	values, err := url.ParseQuery(got[1:])
	if err != nil {
		t.Fatalf("ParseQuery(%q) failed: %v", got[1:], err)
	}
	if len(values) != 3 {
		t.Errorf("parsed %d parameters, want 3: %v", len(values), values)
	}
	for key, want := range map[string]string{
		"cursor": "CURSOR",
		"count":  "100",
		"order":  "asc",
	} {
		if got := values.Get(key); got != want {
			t.Errorf("parameter %q = %q, want %q", key, got, want)
		}
	}
	if _, empty := values[""]; empty {
		t.Error("query contains an empty parameter name")
	}
}

func TestFormatAllParametersRoundTrip(t *testing.T) {
	p := NewParameters()
	p.Count(50)
	p.Cursor("next")
	p.Policy("deadbeef")
	p.EpochNo(500)
	p.From(1)
	p.To(2)
	p.SetDescOrder()
	p.WithCbor()
	p.ResolveDatums()
	p.FromHeight(1234)

	got := p.Format()
	values, err := url.ParseQuery(got[1:])
	if err != nil {
		t.Fatalf("ParseQuery(%q) failed: %v", got[1:], err)
	}

	want := map[string]string{
		"count":          "50",
		"cursor":         "next",
		"policy":         "deadbeef",
		"epoch_no":       "500",
		"from":           "1",
		"to":             "2",
		"order":          "desc",
		"with_cbor":      "true",
		"resolve_datums": "true",
		"from_height":    "1234",
	}
	if len(values) != len(want) {
		t.Errorf("parsed %d parameters, want %d: %v", len(values), len(want), values)
	}
	for key, wantValue := range want {
		if got := values.Get(key); got != wantValue {
			t.Errorf("parameter %q = %q, want %q", key, got, wantValue)
		}
	}
}
