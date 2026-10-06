package web

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"github.com/tyler180/jobtracker/internal/jobs"
)

// cappedOutput bounds decompressed PDF text and stops extraction on overflow.
type cappedOutput struct{ bytes.Buffer }

func (b *cappedOutput) Write(p []byte) (int, error) {
	if b.Len()+len(p) > 64000 {
		return 0, errors.New("PDF text too large")
	}
	return b.Buffer.Write(p)
}

func (s *Server) pdfPreview(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		fail(w, 403, "Cross-site requests are not allowed")
		return
	}
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/pdf" || r.Header.Get("Content-Encoding") != "" {
		fail(w, 415, "Upload a PDF without content encoding")
		return
	}
	select {
	case s.slots <- struct{}{}:
		defer func() { <-s.slots }()
	default:
		fail(w, 429, "Too many imports; try again shortly")
		return
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 5<<20))
	if err != nil {
		fail(w, 413, "Choose a PDF up to 5 MB")
		return
	}
	if !bytes.HasPrefix(data, []byte("%PDF-")) {
		fail(w, 400, "Choose a valid PDF file")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "pdftotext", "-layout", "-enc", "UTF-8", "-", "-")
	cmd.Stdin = bytes.NewReader(data)
	var output cappedOutput
	cmd.Stdout = &output
	if err = cmd.Run(); err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			fail(w, 503, "PDF importing is unavailable on this server. Paste the description instead.")
			return
		}
		fail(w, 400, "Could not read this PDF. Use an unencrypted text PDF with at most 64,000 bytes of text, or paste the description.")
		return
	}
	text := strings.TrimSpace(strings.ReplaceAll(output.String(), "\f", "\n"))
	if text == "" {
		fail(w, 400, "This PDF has no readable text. Paste the description from a scanned PDF instead.")
		return
	}
	j, err := jobs.ManualPreview("", text)
	if err != nil {
		fail(w, 400, err.Error())
		return
	}
	j.Application = jobs.InferPay(j.DescriptionText)
	reply(w, 200, j)
}
