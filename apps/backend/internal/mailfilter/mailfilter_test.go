package mailfilter

import (
	"bytes"
	"encoding/xml"
	"testing"
)

func TestTheGeneratedFilterForwardsToThePlaceholder(t *testing.T) {
	// The parser writes the placeholder; if the two drift apart, no address is ever put in.
	if !bytes.Contains(generated, []byte(`value="`+placeholder+`"`)) {
		t.Fatalf("gmail-filters.xml does not forward to %s", placeholder)
	}
}

func TestForPutsInTheAddressEscaped(t *testing.T) {
	filter := For(`finchie@example.com"&`)
	if bytes.Contains(filter, []byte(placeholder)) {
		t.Fatal("placeholder left in the filter")
	}
	if !bytes.Contains(filter, []byte(`value="finchie@example.com&#34;&amp;"`)) {
		t.Fatalf("address not put in escaped:\n%s", filter)
	}
	if err := xml.Unmarshal(filter, new(struct{})); err != nil {
		t.Fatalf("not well-formed XML: %v", err)
	}
}
