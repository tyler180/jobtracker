package web

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/tyler180/jobtracker/internal/jobs"
)

func textPDF(text string) []byte {
	var b bytes.Buffer
	b.WriteString("%PDF-1.4\n")
	stream := "BT /F1 12 Tf 50 700 Td (" + text + ") Tj ET"
	objects := []string{"<< /Type /Catalog /Pages 2 0 R >>", "<< /Type /Pages /Kids [3 0 R] /Count 1 >>", "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>", "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>", fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream), stream)}
	offsets := []int{0}
	for i, o := range objects {
		offsets = append(offsets, b.Len())
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 6\n0000000000 65535 f \n")
	for _, offset := range offsets[1:] {
		fmt.Fprintf(&b, "%010d 00000 n \n", offset)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size 6 /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", xref)
	return b.Bytes()
}

func TestPDFPreview(t *testing.T) {
	if _, err := exec.LookPath("pdftotext"); err != nil {
		t.Skip("pdftotext is required")
	}
	store, _ := jobs.Open(t.TempDir())
	handler := New(store, &fake{})
	for _, tc := range []struct {
		body        []byte
		media, site string
		status      int
	}{
		{textPDF("Cloud Engineer"), "application/pdf", "", 200},
		{textPDF(""), "application/pdf", "", 400},
		{[]byte("%PDF-invalid"), "application/pdf", "", 400},
		{[]byte("not PDF"), "application/pdf", "", 400},
		{bytes.Repeat([]byte("x"), (5<<20)+1), "application/pdf", "", 413},
		{textPDF("Engineer"), "text/plain", "", 415},
		{textPDF("Engineer"), "application/pdf", "cross-site", 403},
	} {
		req := httptest.NewRequest("POST", "/api/jobs/pdf", bytes.NewReader(tc.body))
		req.Header.Set("Content-Type", tc.media)
		req.Header.Set("Sec-Fetch-Site", tc.site)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != tc.status {
			t.Fatalf("got %d want %d: %s", w.Code, tc.status, w.Body)
		}
		if w.Code == 200 {
			var j jobs.Job
			json.Unmarshal(w.Body.Bytes(), &j)
			if !strings.Contains(j.DescriptionText, "Cloud Engineer") {
				t.Fatal(j)
			}
		}
	}
	entries, _ := store.List()
	if len(entries) != 0 {
		t.Fatal("preview saved a posting")
	}
}

func TestPDFProvidedFixture(t *testing.T) {
	path := os.Getenv("JOBTRACKER_TEST_PDF")
	if path == "" {
		t.Skip("optional user PDF fixture")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	store, _ := jobs.Open(t.TempDir())
	handler := New(store, &fake{})
	req := httptest.NewRequest("POST", "/api/jobs/pdf", bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/pdf")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	var j jobs.Job
	json.Unmarshal(w.Body.Bytes(), &j)
	for _, text := range []string{"Cloud Engineer", "130,000", "180,000", "Jack Harris"} {
		if !strings.Contains(j.DescriptionText, text) {
			t.Fatalf("missing %s", text)
		}
	}
}
