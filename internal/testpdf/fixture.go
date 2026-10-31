/*
              _                                 
__      _____| |__  ___ _ __ ___   _____  _____ 
\ \ /\ / / _ \ '_ \/ __| '__/ __| / _ \ \/ / _ \
 \ V  V /  __/ |_) \__ \ | | (__ |  __/>  <  __/
  \_/\_/ \___|_.__/|___/_|  \___(_)___/_/\_\___|
                                                 
*/

// Package testpdf constructs small, synthetic PDF fixtures without a PDF printer.
// It is imported only by tests and the example generator, not by websrc.exe.
package testpdf

import (
	"fmt"
	"strings"

	"github.com/giraffesyo/pdf/pdftest"
)

type Text struct {
	Value      string
	Size, X, Y float64
}
type Page struct {
	Text  []Text
	Image bool
}

func objects(pages []Page) []string {
	objects := []string{pdftest.Catalog(2), "", pdftest.Helvetica()}
	var children []int
	for _, page := range pages {
		pageID := len(objects) + 1
		streamID := pageID + 1
		children = append(children, pageID)
		objects = append(objects, "", "")
		resources := "/Font << /F1 3 0 R >>"
		var content strings.Builder
		for _, text := range page.Text {
			value := strings.NewReplacer(`\`, `\\`, "(", `\(`, ")", `\)`).Replace(text.Value)
			fmt.Fprintf(&content, "BT /F1 %g Tf 1 0 0 1 %g %g Tm (%s) Tj ET\n", text.Size, text.X, text.Y, value)
		}
		if page.Image {
			imageID := len(objects) + 1
			objects = append(objects, pdftest.Stream("/Type /XObject /Subtype /Image /Width 1 /Height 1 /ColorSpace /DeviceGray /BitsPerComponent 8", "\x00"))
			resources += fmt.Sprintf(" /XObject << /Im0 %d 0 R >>", imageID)
			content.WriteString("q 100 0 0 100 72 500 cm /Im0 Do Q\n")
		}
		objects[pageID-1] = pdftest.Page(2, streamID, "<< "+resources+" >>")
		objects[streamID-1] = pdftest.Stream("", content.String())
	}
	objects[1] = pdftest.Pages(children...)
	return objects
}

func Build(pages ...Page) []byte { return pdftest.Build(1, objects(pages)...) }
func Encrypted(password string, pages ...Page) []byte {
	return pdftest.BuildEncrypted(1, pdftest.EncryptSpec{R: 4, AES: true, UserPassword: password, OwnerPassword: "fixture-owner"}, objects(pages)...)
}

func Sample() []byte {
	return Build(
		Page{Text: []Text{
			{"PDF extraction example", 24, 72, 730},
			{"This is selectable PDF text. It uses the same export flags as HTML.", 12, 72, 690},
			{"Body lines keep their reading order and original line breaks.", 12, 72, 672},
			{"A smaller heading", 18, 72, 620},
			{"Characters such as <, >, & and literal *Markdown* are exported safely.", 12, 72, 580},
		}},
		Page{Text: []Text{
			{"A second page", 18, 72, 730},
			{"This page follows the first. No OCR or external PDF reader is needed.", 12, 72, 690},
		}},
	)
}
