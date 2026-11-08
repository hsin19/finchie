// Package mailfilter serves the Gmail filter that forwards statement e-mails to the mailbox (ADR 0014).
//
// gmail-filters.xml is generated from the parsers' claims by `task parser:generate` and
// forwards to a placeholder: the mailbox address is the deployment's and stays out of the repository
// (ADR 0010).
package mailfilter

import (
	"bytes"
	_ "embed"
	"encoding/xml"
)

//go:embed gmail-filters.xml
var generated []byte

// placeholder is MAILBOX_PLACEHOLDER in apps/parser/src/finchie_parser/mail_filter.py.
const placeholder = "finchie@mailbox.invalid"

// For is the filter forwarding to address.
func For(address string) []byte {
	var escaped bytes.Buffer
	// Never fails writing to a bytes.Buffer.
	_ = xml.EscapeText(&escaped, []byte(address))
	return bytes.ReplaceAll(generated, []byte(placeholder), escaped.Bytes())
}
