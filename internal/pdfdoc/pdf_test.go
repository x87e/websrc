/*
              _                                 
__      _____| |__  ___ _ __ ___   _____  _____ 
\ \ /\ / / _ \ '_ \/ __| '__/ __| / _ \ \/ / _ \
 \ V  V /  __/ |_) \__ \ | | (__ |  __/>  <  __/
  \_/\_/ \___|_.__/|___/_|  \___(_)___/_/\_\___|
                                                 
*/

package pdfdoc

import (
	"context"
	"encoding/xml"
	"io"
	"strings"
	"testing"

	"websrc/internal/testpdf"
)

func TestPDFFormatsAndHeadings(t *testing.T) {
	doc, warnings, err := Parse(context.Background(), testpdf.Sample(), "")
	if err != nil || len(warnings) > 0 {
		t.Fatal(err, warnings)
	}
	md := doc.Render("md")
	for _, want := range []string{"# PDF extraction example\n", "## A smaller heading\n", "## A second page\n", `literal \*Markdown\*`, "HTML.  \nBody lines"} {
		if !strings.Contains(md, want) {
			t.Errorf("missing %q in:\n%s", want, md)
		}
	}
	if doc.Render("mdx") != md {
		t.Fatal("PDF md/mdx should share inferred headings")
	}
	plain := doc.Render("txt")
	if strings.Contains(plain, "# PDF") || !strings.Contains(plain, "<, >, & and literal *Markdown*") {
		t.Fatal(plain)
	}
	for _, format := range []string{"xml", "txtx"} {
		text := doc.Render(format)
		if !strings.Contains(text, "<header1>PDF extraction example</header1>") || !strings.Contains(text, "&lt;, &gt;, &amp;") {
			t.Fatal(text)
		}
		decoder := xml.NewDecoder(strings.NewReader(text))
		for {
			_, err := decoder.Token()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestImageOnlyAndMixedPDF(t *testing.T) {
	_, _, err := Parse(context.Background(), testpdf.Build(testpdf.Page{Image: true}), "")
	if err == nil || !strings.Contains(err.Error(), "OCR") {
		t.Fatal(err)
	}
	data := testpdf.Build(testpdf.Page{Image: true}, testpdf.Page{Text: []testpdf.Text{{Value: "Readable text on page two", Size: 12, X: 72, Y: 700}}})
	doc, warnings, err := Parse(context.Background(), data, "")
	if err != nil || !strings.Contains(doc.Render("txt"), "Readable text") || len(warnings) != 1 || !strings.Contains(warnings[0], "page 1") || !strings.Contains(warnings[0], "OCR") {
		t.Fatal(doc, warnings, err)
	}
}

func TestEncryptedPDF(t *testing.T) {
	data := testpdf.Encrypted("secret", testpdf.Page{Text: []testpdf.Text{{Value: "Private document text", Size: 12, X: 72, Y: 700}}})
	for _, password := range []string{"", "wrong"} {
		_, _, err := Parse(context.Background(), data, password)
		if err == nil || !strings.Contains(err.Error(), "password") {
			t.Fatal(err)
		}
	}
	doc, warnings, err := Parse(context.Background(), data, "secret")
	if err != nil || len(warnings) > 0 || doc.Render("txt") != "Private document text\n" {
		t.Fatal(doc, warnings, err)
	}
}

func TestMalformedAndCancelledPDF(t *testing.T) {
	for _, input := range [][]byte{nil, []byte("not a PDF"), []byte("%PDF-1.7\ntruncated")} {
		if _, _, err := Parse(context.Background(), input, ""); err == nil {
			t.Fatal("malformed PDF accepted")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := Parse(ctx, testpdf.Sample(), ""); err == nil {
		t.Fatal("cancelled extraction succeeded")
	}
}

func TestWinAnsiAndLiteralMarkup(t *testing.T) {
	data := testpdf.Build(testpdf.Page{Text: []testpdf.Text{{Value: "Caf\xe9 <tag> & value", Size: 12, X: 72, Y: 700}}})
	doc, _, err := Parse(context.Background(), data, "")
	if err != nil {
		t.Fatal(err)
	}
	if doc.Render("txt") != "Café <tag> & value\n" {
		t.Fatal(doc.Render("txt"))
	}
}
