package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/hsin19/finchie/apps/backend/internal/db/dbq"
	parserv2 "github.com/hsin19/finchie/apps/backend/internal/gen/finchie/parser/v2"
)

// openTimeout bounds a call that reads an e-mail and decrypts its PDFs: no OCR, unlike a parse.
const openTimeout = 30 * time.Second

// bodyPolicy sandboxes the e-mail body even when its URL is opened on its own: no scripts, forms,
// or plugins, an origin of its own, and only images from elsewhere, as a mail client shows them.
// Some banks still link images over http; they load upgraded to https. Links may open a new tab,
// outside the sandbox, so the bank's site works there.
const bodyPolicy = "sandbox allow-popups allow-popups-to-escape-sandbox; default-src 'none'; " +
	"img-src data: https:; style-src 'unsafe-inline'; frame-ancestors 'self'; upgrade-insecure-requests"

// noStore keeps what the parser opened out of the browser's cache: nothing decrypted is stored
// (ADR 0012), on the server or in the browser.
const noStore = "no-store"

// statusClientClosedRequest is nginx's status for a request whose client left before the answer.
const statusClientClosedRequest = 499

var (
	// A doctype may follow only a byte-order mark, whitespace, comments, and an XML prolog (which
	// HTML reads as a comment); anything else before it puts the page in quirks mode.
	doctype = regexp.MustCompile(`(?is)^(?:\x{FEFF}|\s|<!--.*?-->|<\?[^>]*>)*<!doctype[^>]*>`)
	headTag = regexp.MustCompile(`(?i)<head(?:\s[^>]*)?>`)
)

// openFailure is why a document could not be opened. Every preview operation declares the same 404,
// 422, and 503 Problem responses, so it is a response of each.
type openFailure struct {
	status  int
	message string
}

func (f *openFailure) write(w http.ResponseWriter) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(f.status)
	if err := json.NewEncoder(w).Encode(Problem{Message: f.message}); err != nil {
		return fmt.Errorf("write problem: %w", err)
	}
	return nil
}

func (f *openFailure) VisitGetDocumentPreviewResponse(w http.ResponseWriter) error {
	return f.write(w)
}

func (f *openFailure) VisitGetDocumentPreviewPdfResponse(w http.ResponseWriter) error {
	return f.write(w)
}

func (f *openFailure) VisitGetDocumentPreviewBodyResponse(w http.ResponseWriter) error {
	return f.write(w)
}

func (f *openFailure) VisitGetDocumentPreviewFileResponse(w http.ResponseWriter) error {
	return f.write(w)
}

// open has the parser open the stored original; nothing it returns is stored (ADR 0012).
func (s *Server) open(ctx context.Context, id int64) (*parserv2.OpenDocumentResponse, *openFailure, error) {
	row, err := dbq.New(s.pool).GetDocumentContent(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, &openFailure{http.StatusNotFound, notFound("document", id).Message}, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("get document %d content: %w", id, err)
	}
	ctx, cancel := context.WithTimeout(ctx, openTimeout)
	defer cancel()
	opened, err := s.parser.OpenDocument(ctx, &parserv2.OpenDocumentRequest{
		DocumentId: strconv.FormatInt(id, 10),
		Content:    row.Content,
	})
	switch status.Code(err) {
	case codes.OK:
		return opened, nil, nil
	case codes.InvalidArgument, codes.Unimplemented:
		message := "the parser cannot open this document: " + status.Convert(err).Message()
		return nil, &openFailure{http.StatusUnprocessableEntity, message}, nil
	case codes.ResourceExhausted:
		// What the parser opened is larger than a gRPC message may be.
		return nil, &openFailure{http.StatusUnprocessableEntity, "the opened document is too large to show; download the original instead"}, nil
	case codes.Unavailable, codes.DeadlineExceeded:
		return nil, &openFailure{http.StatusServiceUnavailable, "the parser is not reachable; try again"}, nil
	case codes.Canceled:
		// The browser went away. Nobody reads this answer, but it must not count as a server error.
		return nil, &openFailure{statusClientClosedRequest, "the request was cancelled"}, nil
	default:
		return nil, nil, fmt.Errorf("open document %d: %w", id, err)
	}
}

func (s *Server) GetDocumentPreview(ctx context.Context, request GetDocumentPreviewRequestObject) (GetDocumentPreviewResponseObject, error) {
	opened, failure, err := s.open(ctx, request.Id)
	if err != nil {
		return nil, err
	}
	if failure != nil {
		return failure, nil
	}

	preview := DocumentPreview{Files: make([]PreviewFile, len(opened.GetFiles()))}
	for i, file := range opened.GetFiles() {
		preview.Files[i] = PreviewFile{
			Index:       i,
			Filename:    file.GetFilename(),
			ContentType: file.GetContentType(),
			SizeBytes:   len(file.GetContent()),
			Locked:      file.GetLocked(),
			Primary:     file.GetPrimary(),
		}
	}
	if email := opened.GetEmail(); email != nil {
		preview.Email = &EmailPreview{
			Sender:    email.GetSender(),
			Recipient: email.GetRecipient(),
			Subject:   email.GetSubject(),
			HasBody:   email.GetHtml() != "" || email.GetText() != "",
		}
		if email.GetDate() != nil {
			date := email.GetDate().AsTime()
			preview.Email.Date = &date
		}
	}
	return GetDocumentPreview200JSONResponse(preview), nil
}

func (s *Server) GetDocumentPreviewPdf(ctx context.Context, request GetDocumentPreviewPdfRequestObject) (GetDocumentPreviewPdfResponseObject, error) {
	opened, failure, err := s.open(ctx, request.Id)
	if err != nil {
		return nil, err
	}
	if failure != nil {
		return failure, nil
	}

	for _, file := range opened.GetFiles() {
		if !file.GetPrimary() {
			continue
		}
		served, cache := serveFile(contentTypePDF, file.GetFilename()), noStore
		return GetDocumentPreviewPdf200ApplicationpdfResponse{
			Body:          bytes.NewReader(file.GetContent()),
			ContentLength: int64(len(file.GetContent())),
			Headers: GetDocumentPreviewPdf200ResponseHeaders{
				CacheControl:        &cache,
				ContentDisposition:  &served.disposition,
				XContentTypeOptions: &served.nosniff,
			},
		}, nil
	}
	return GetDocumentPreviewPdf404JSONResponse{ProblemJSONResponse{Message: fmt.Sprintf("document %d holds no PDF", request.Id)}}, nil
}

func (s *Server) GetDocumentPreviewBody(ctx context.Context, request GetDocumentPreviewBodyRequestObject) (GetDocumentPreviewBodyResponseObject, error) {
	opened, failure, err := s.open(ctx, request.Id)
	if err != nil {
		return nil, err
	}
	if failure != nil {
		return failure, nil
	}

	email := opened.GetEmail()
	if email.GetHtml() == "" && email.GetText() == "" {
		return GetDocumentPreviewBody404JSONResponse{ProblemJSONResponse{Message: fmt.Sprintf("document %d has no e-mail body", request.Id)}}, nil
	}
	page := bodyPage(email.GetHtml(), email.GetText())
	policy, referrer, nosniff, cache := bodyPolicy, "no-referrer", "nosniff", noStore
	return GetDocumentPreviewBody200TexthtmlCharsetUtf8Response{
		Body:          strings.NewReader(page),
		ContentLength: int64(len(page)),
		Headers: GetDocumentPreviewBody200ResponseHeaders{
			CacheControl:          &cache,
			ContentSecurityPolicy: &policy,
			ReferrerPolicy:        &referrer,
			XContentTypeOptions:   &nosniff,
		},
	}, nil
}

// bodyPage makes the body a page whose links open in a new tab: a sandboxed frame cannot show the
// bank's site in place. The base element goes into the head, else after the doctype, which must
// stay ahead of every element.
func bodyPage(htmlBody, text string) string {
	const base, bom = `<base target="_blank">`, "\ufeff"
	if htmlBody == "" {
		return `<!doctype html><meta charset="utf-8">` + base +
			`<pre style="white-space: pre-wrap; font: 14px/1.6 system-ui, sans-serif; margin: 16px">` +
			html.EscapeString(text) + `</pre>`
	}
	at := 0
	if strings.HasPrefix(htmlBody, bom) {
		at = len(bom)
	}
	if loc := doctype.FindStringIndex(htmlBody); loc != nil {
		at = loc[1]
	}
	if loc := headTag.FindStringIndex(htmlBody[at:]); loc != nil {
		at += loc[1]
	}
	return htmlBody[:at] + base + htmlBody[at:]
}

func (s *Server) GetDocumentPreviewFile(ctx context.Context, request GetDocumentPreviewFileRequestObject) (GetDocumentPreviewFileResponseObject, error) {
	opened, failure, err := s.open(ctx, request.Id)
	if err != nil {
		return nil, err
	}
	if failure != nil {
		return failure, nil
	}

	files := opened.GetFiles()
	if request.Index < 0 || request.Index >= len(files) {
		return GetDocumentPreviewFile404JSONResponse{ProblemJSONResponse{Message: fmt.Sprintf("document %d has no file %d", request.Id, request.Index)}}, nil
	}
	file := files[request.Index]
	served, cache := serveFile(file.GetContentType(), file.GetFilename()), noStore
	return GetDocumentPreviewFile200AsteriskResponse{
		Body:          bytes.NewReader(file.GetContent()),
		ContentType:   served.contentType,
		ContentLength: int64(len(file.GetContent())),
		Headers: GetDocumentPreviewFile200ResponseHeaders{
			CacheControl:        &cache,
			ContentDisposition:  &served.disposition,
			XContentTypeOptions: &served.nosniff,
		},
	}, nil
}
