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
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestOptionsInterspersedAndValidation(t *testing.T) {
	t.Setenv("WEBSRC_TEST_DIR", filepath.Join(t.TempDir(), "space dir"))
	o, err := parseOptions([]string{"https://example.test/page", "-md", "%WEBSRC_TEST_DIR%"})
	if err != nil || o.Format != "md" || o.Dir != os.Getenv("WEBSRC_TEST_DIR") {
		t.Fatalf("%+v %v", o, err)
	}
	for _, args := range [][]string{
		{"https://example.test", "-md", "-txt"}, {"file:///private", "-md"},
		{"https://example.test", "--stdout", "-o", "file.txt"}, {"https://example.test", "--timeout", "0s"},
		{"https://example.test", "--max-bytes", "0"}, {"https://example.test", "--format", "bad"},
		{"https://example.test", "--wat"}, {"https://example.test", "-md=yes"}, {"https://example.test", "--output"},
	} {
		if _, err := parseOptions(args); err == nil {
			t.Errorf("accepted %v", args)
		}
	}
	o, err = parseOptions([]string{"-md", "--stdout", "https://example.test"})
	if err != nil || !o.Stdout {
		t.Fatal(o, err)
	}
}

func TestArgumentFilesWindowsPathsAndCycles(t *testing.T) {
	args, err := splitArgs(`https://example.test -md "C:\Exports\My Pages"`)
	if err != nil || len(args) != 3 || args[2] != `C:\Exports\My Pages` {
		t.Fatal(args, err)
	}
	if _, err := splitArgs(`"unterminated`); err == nil {
		t.Fatal("missing quote should fail")
	}
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "nested.txt"), "\ufeff# comment\nhttps://example.test -md\n--stdout\n")
	writeTestFile(t, filepath.Join(dir, "args.txt"), "@nested.txt")
	expanded, err := expandArgs([]string{filepath.Join(dir, "args.txt")}, dir, 0, map[string]bool{})
	if err != nil || strings.Join(expanded, " ") != "https://example.test -md --stdout" {
		t.Fatal(expanded, err)
	}
	writeTestFile(t, filepath.Join(dir, "nested.txt"), "@args.txt")
	if _, err := expandArgs([]string{"@args.txt"}, dir, 0, map[string]bool{}); err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatal(err)
	}
	expanded, err = expandArgs([]string{"https://example.test/page.txt"}, dir, 0, map[string]bool{})
	if err != nil || expanded[0] != "https://example.test/page.txt" {
		t.Fatal(expanded, err)
	}
}

func TestEndToEndExportsAndRaw(t *testing.T) {
	source := `<h1>Heading</h1><p>Hello <b>world</b> &amp; friends.</p>`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, source)
	}))
	defer server.Close()
	for _, tt := range []struct{ flag, want string }{{"-raw", source}, {"-md", "# Heading\n\nHello **world** \\& friends.\n"}, {"-txt", "Heading\n\nHello world & friends.\n"}, {"-txtx", "<header1>Heading</header1>"}, {"-xml", "<p>Hello world &amp; friends.</p>"}} {
		t.Run(tt.flag, func(t *testing.T) {
			var out, diag bytes.Buffer
			code := Run([]string{server.URL, tt.flag, "--stdout"}, strings.NewReader(""), &out, &diag, "websrc.exe", "test")
			if code != 0 || !strings.Contains(out.String(), tt.want) {
				t.Fatalf("code %d output %q errors %q", code, out.String(), diag.String())
			}
			if diag.Len() != 0 {
				t.Fatal("stdout export produced unexpected diagnostics", diag.String())
			}
		})
	}
}

func TestOutputDirectoryAndOverwrite(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "input.html")
	writeTestFile(t, input, "<h1>First</h1>")
	executable := filepath.Join(dir, "program", "websrc.exe")
	run := func(args ...string) (int, string, string) {
		var out, diag bytes.Buffer
		code := Run(args, strings.NewReader(""), &out, &diag, executable, "test")
		return code, out.String(), diag.String()
	}
	if code, out, diag := run("--input", input, "-md"); code != 0 || out != "" || !strings.Contains(diag, "Saved") {
		t.Fatal(code, out, diag)
	}
	dest := filepath.Join(dir, "program", "input.md")
	data, err := os.ReadFile(dest)
	if err != nil || string(data) != "# First\n" {
		t.Fatal(string(data), err)
	}
	writeTestFile(t, input, "<h1>Second</h1>")
	if code, _, diag := run("--input", input, "-md"); code != 1 || !strings.Contains(diag, "already exists") {
		t.Fatal(code, diag)
	}
	data, _ = os.ReadFile(dest)
	if string(data) != "# First\n" {
		t.Fatal("existing file changed")
	}
	if code, _, diag := run("--input", input, "-md", "--overwrite"); code != 0 {
		t.Fatal(diag)
	}
	data, _ = os.ReadFile(dest)
	if string(data) != "# Second\n" {
		t.Fatal(string(data))
	}
	if code, _, diag := run("--input", input, "-txt", "--output", filepath.Join(dir, "new folder", "chosen.txt")); code != 0 {
		t.Fatal(diag)
	}
}

func TestHTTPFailuresLimitsAndPDF(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/404":
			http.Error(w, "missing", 404)
		case "/large":
			fmt.Fprint(w, strings.Repeat("X", 1024))
		case "/pdf":
			w.Header().Set("Content-Type", "application/pdf")
			fmt.Fprint(w, "%PDF-1.7")
		case "/json":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"a":1}`)
		case "/slow":
			select {
			case <-r.Context().Done():
				return
			case <-time.After(time.Second):
				fmt.Fprint(w, "<p>late</p>")
			}
		}
	}))
	defer server.Close()
	for _, tt := range []struct {
		path, expected string
		extra          []string
	}{{"/404", "404", nil}, {"/large", "limit", []string{"--max-bytes", "100"}}, {"/pdf", "extract PDF", nil}, {"/json", "content type", nil}, {"/slow", "deadline", []string{"--timeout", "10ms"}}} {
		var out, diag bytes.Buffer
		args := append([]string{server.URL + tt.path, "-md", "--stdout"}, tt.extra...)
		if code := Run(args, strings.NewReader(""), &out, &diag, "websrc.exe", "test"); code != 1 || out.Len() != 0 || !strings.Contains(diag.String(), tt.expected) {
			t.Errorf("%s: code %d output %q diagnostic %q", tt.path, code, out.String(), diag.String())
		}
	}
}

func TestRedirectsCharsetsAndExternalCSS(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/start":
			http.Redirect(w, r, "/folder/page", http.StatusFound)
		case "/folder/page":
			w.Header().Set("Content-Type", "text/html; charset=windows-1252")
			fmt.Fprint(w, "<link rel='stylesheet' href='theme.css'><div class='title'>Caf\xe9</div><p>This body text is considerably longer and determines the common font size.</p><p><a href='next'>Next</a></p>")
		case "/folder/theme.css":
			fmt.Fprint(w, ".title { font-size:32px }")
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	var out, diag bytes.Buffer
	code := Run([]string{server.URL + "/start", "-mdx", "--stdout"}, strings.NewReader(""), &out, &diag, "websrc.exe", "test")
	if code != 0 || !strings.HasPrefix(out.String(), "# Café\n") || !strings.Contains(out.String(), server.URL+"/folder/next") {
		t.Fatal(code, out.String(), diag.String())
	}
}

func TestInteractiveAndArgumentFileExecution(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "page.html")
	writeTestFile(t, input, "<h1>Interactive</h1>")
	line := `--input "` + input + `" -txt --stdout`
	var out, diag bytes.Buffer
	if code := Run(nil, strings.NewReader("help\n"+line+"\nexit\n"), &out, &diag, "websrc.exe", "test"); code != 0 || out.String() != "Interactive\n" || !strings.Contains(diag.String(), "Usage:") {
		t.Fatal(code, out.String(), diag.String())
	}
	argsFile := filepath.Join(dir, "arguments.txt")
	writeTestFile(t, argsFile, line)
	out.Reset()
	diag.Reset()
	if code := Run([]string{argsFile}, strings.NewReader(""), &out, &diag, "websrc.exe", "test"); code != 0 || out.String() != "Interactive\n" {
		t.Fatal(code, out.String(), diag.String())
	}
}

func TestInteractiveTitleBanner(t *testing.T) {
	title, err := os.ReadFile("banner.txt")
	if err != nil {
		t.Fatal(err)
	}
	var out, diagnostic bytes.Buffer
	code := Run(nil, strings.NewReader("help\nhelp\nexit\n"), &out, &diagnostic, "websrc.exe", "0.2.0")
	wantPrefix := string(title)
	if !strings.HasSuffix(wantPrefix, "\n") {
		wantPrefix += "\n"
	}
	wantPrefix += "websrc 0.2.0 - HTML and PDF downloader and converter\n"
	if code != 0 || out.Len() != 0 || !strings.HasPrefix(diagnostic.String(), wantPrefix) {
		t.Fatalf("interactive title/version ordering: exit=%d stdout=%q terminal=%q", code, out.String(), diagnostic.String())
	}
	if strings.Count(diagnostic.String(), strings.TrimSpace(string(title))) != 1 {
		t.Fatal("the title should appear once per interactive session")
	}
	out.Reset()
	diagnostic.Reset()
	code = Run([]string{"--version"}, strings.NewReader(""), &out, &diagnostic, "websrc.exe", "0.2.0")
	if code != 0 || out.String() != "websrc 0.2.0\n" || diagnostic.Len() != 0 {
		t.Fatalf("noninteractive version output changed: exit=%d stdout=%q stderr=%q", code, out.String(), diagnostic.String())
	}
}

func TestOutputNames(t *testing.T) {
	for _, tt := range []struct{ address, format, want string }{{"https://example.com/", "md", "example.com.md"}, {"https://example.com/index.html?q=1", "raw", "index.txt"}, {"https://example.com/CON.html", "txtx", "_CON.txt"}, {"https://example.com/a%3Ab.html", "xml", "a_b.xml"}} {
		u, _ := url.Parse(tt.address)
		if got := outputName(u, tt.format); got != tt.want {
			t.Errorf("%s: %s != %s", tt.address, got, tt.want)
		}
	}
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}
