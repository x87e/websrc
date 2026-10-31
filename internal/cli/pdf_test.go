/*
              _                                 
__      _____| |__  ___ _ __ ___   _____  _____ 
\ \ /\ / / _ \ '_ \/ __| '__/ __| / _ \ \/ / _ \
 \ V  V /  __/ |_) \__ \ | | (__ |  __/>  <  __/
  \_/\_/ \___|_.__/|___/_|  \___(_)___/_/\_\___|
                                                 
*/

package cli

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"websrc/internal/testpdf"
)

func runPDFCommand(args []string, executable string) (int, string, string) {
	var out, diagnostic bytes.Buffer
	code := Run(args, strings.NewReader(""), &out, &diagnostic, executable, "test")
	return code, out.String(), diagnostic.String()
}

func TestPDFLocalAndHTTPAllFormats(t *testing.T) {
	data := testpdf.Sample()
	dir := t.TempDir()
	input := filepath.Join(dir, "report with spaces.pdf")
	if err := os.WriteFile(input, data, 0644); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, "/download", http.StatusFound)
			return
		}
		// The endpoint has no .pdf extension and uses a generic MIME type.
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Write(data)
	}))
	defer server.Close()
	for _, source := range [][]string{{input}, {"--input", input}, {server.URL + "/redirect"}} {
		for _, format := range []string{"raw", "md", "mdx", "txt", "txtx", "xml"} {
			args := append(append([]string{}, source...), "-"+format, "--stdout")
			code, out, diagnostic := runPDFCommand(args, "websrc.exe")
			if code != 0 || diagnostic != "" {
				t.Fatalf("%v: %d %s", args, code, diagnostic)
			}
			if format == "raw" {
				if !bytes.Equal([]byte(out), data) {
					t.Fatal("PDF raw data changed")
				}
			} else if !strings.Contains(out, "PDF extraction example") {
				t.Fatal(args, out)
			}
		}
	}
}

func TestPDFNamingAndExistingFiles(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "report.pdf")
	original := testpdf.Sample()
	if err := os.WriteFile(input, original, 0644); err != nil {
		t.Fatal(err)
	}
	executable := filepath.Join(dir, "bin", "websrc.exe")
	code, _, diagnostic := runPDFCommand([]string{input, "-md"}, executable)
	if code != 0 {
		t.Fatal(diagnostic)
	}
	if _, err := os.Stat(filepath.Join(dir, "bin", "report.md")); err != nil {
		t.Fatal(err)
	}
	code, _, diagnostic = runPDFCommand([]string{input}, executable)
	if code != 0 {
		t.Fatal(diagnostic)
	}
	copy, err := os.ReadFile(filepath.Join(dir, "bin", "report.pdf"))
	if err != nil || !bytes.Equal(copy, original) {
		t.Fatal(err)
	}
	code, _, diagnostic = runPDFCommand([]string{input, "--output", input}, executable)
	if code != 1 || !strings.Contains(diagnostic, "already exists") {
		t.Fatal(code, diagnostic)
	}
}

func TestPDFPasswordsAndFailures(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "encrypted.pdf")
	data := testpdf.Encrypted("secret", testpdf.Page{Text: []testpdf.Text{{Value: "Confidential text", Size: 12, X: 72, Y: 700}}})
	if err := os.WriteFile(input, data, 0644); err != nil {
		t.Fatal(err)
	}
	code, out, diagnostic := runPDFCommand([]string{input, "-txt", "--stdout"}, "websrc.exe")
	if code != 1 || out != "" || !strings.Contains(diagnostic, "password") {
		t.Fatal(code, out, diagnostic)
	}
	t.Setenv("WEBSRC_PDF_PASSWORD", "secret")
	for _, passwordArgs := range [][]string{{"--password-env", "WEBSRC_PDF_PASSWORD"}, {"--password", "secret"}} {
		args := append([]string{input, "-txt", "--stdout"}, passwordArgs...)
		code, out, diagnostic = runPDFCommand(args, "websrc.exe")
		if code != 0 || out != "Confidential text\n" || diagnostic != "" {
			t.Fatal(code, out, diagnostic)
		}
	}
	if _, err := parseOptions([]string{input, "--password-env", "WEBSRC_PDF_PASSWORD", "--password", "secret"}); err == nil {
		t.Fatal("accepted two password sources")
	}
	missing := filepath.Join(dir, "missing.pdf")
	code, _, diagnostic = runPDFCommand([]string{missing, "-txt", "--stdout"}, "websrc.exe")
	if code != 1 || !strings.Contains(diagnostic, "open input") {
		t.Fatal(code, diagnostic)
	}
	invalid := filepath.Join(dir, "bad.pdf")
	writeTestFile(t, invalid, "broken PDF")
	code, out, diagnostic = runPDFCommand([]string{invalid, "-md", "--stdout"}, "websrc.exe")
	if code != 1 || out != "" || !strings.Contains(diagnostic, "extract PDF") {
		t.Fatal(code, out, diagnostic)
	}
}

func TestPDFInputLimitAndArgumentFile(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "report.pdf")
	if err := os.WriteFile(input, testpdf.Sample(), 0644); err != nil {
		t.Fatal(err)
	}
	code, out, diagnostic := runPDFCommand([]string{input, "-txt", "--stdout", "--max-bytes", "10"}, "websrc.exe")
	if code != 1 || out != "" || !strings.Contains(diagnostic, "limit") {
		t.Fatal(code, out, diagnostic)
	}
	argsFile := filepath.Join(dir, "arguments.txt")
	writeTestFile(t, argsFile, `"`+input+`" -md --stdout`)
	code, out, diagnostic = runPDFCommand([]string{argsFile}, "websrc.exe")
	if code != 0 || !strings.HasPrefix(out, "# PDF extraction example") {
		t.Fatal(code, out, diagnostic)
	}
}
