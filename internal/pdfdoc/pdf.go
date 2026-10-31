/*
              _                                 
__      _____| |__  ___ _ __ ___   _____  _____ 
\ \ /\ / / _ \ '_ \/ __| '__/ __| / _ \ \/ / _ \
 \ V  V /  __/ |_) \__ \ | | (__ |  __/>  <  __/
  \_/\_/ \___|_.__/|___/_|  \___(_)___/_/\_\___|
                                                 
*/

// Package pdfdoc converts PDF text layers into the shared document structure.
package pdfdoc

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	pdf "github.com/giraffesyo/pdf"
	"websrc/internal/document"
)

const maxTextBytes = 64 << 20

type line struct {
	text string
	size float64
}

// Parse streams extraction page by page. No images are decoded and no OCR or
// external process is invoked. Warnings describe any omitted/partial content.
func Parse(ctx context.Context, data []byte, password string) (*document.Document, []string, error) {
	var pages [][]line
	weights := map[float64]int{}
	var warnings []string
	textBytes := 0
	addWarning := func(message string) {
		if len(warnings) < 20 {
			warnings = append(warnings, message)
		}
	}
	result, err := pdf.ExtractPages(ctx, bytes.NewReader(data), int64(len(data)), pdf.Options{
		Password:    password,
		Concurrency: 1,
		Limits:      pdf.Limits{MaxStreamBytes: 32 << 20, MaxGlyphsPerPage: 250000, MaxOperatorsPerPage: 1000000},
	}, func(page pdf.Page) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if len(pages) >= 10000 {
			return errors.New("PDF exceeds the 10000-page extraction limit")
		}
		text := strings.TrimSpace(page.Text())
		textBytes += len(text)
		if textBytes > maxTextBytes {
			return errors.New("PDF extracted text exceeds the 64 MiB limit")
		}
		if text == "" {
			if page.ImageCount > 0 {
				addWarning(fmt.Sprintf("PDF page %d has images but no extractable text; OCR is required", page.Number))
			} else {
				addWarning(fmt.Sprintf("PDF page %d has no extractable text (blank, outlined or unsupported content)", page.Number))
			}
			pages = append(pages, nil)
			return nil
		}
		groups := map[float64][]pdf.Glyph{}
		for _, glyph := range page.Glyphs {
			size := math.Round(glyph.Size*10) / 10
			if size <= 0 || math.IsNaN(size) || math.IsInf(size, 0) {
				continue
			}
			weights[size] += utf8.RuneCountInString(normalize(glyph.Text))
			groups[size] = append(groups[size], glyph)
		}
		// Match whole reconstructed lines against each font-size subset. Mixed
		// sizes remain ordinary text instead of guessing from one large letter.
		sizes := map[string]float64{}
		if len(groups) <= 64 {
			for size, glyphs := range groups {
				if err := ctx.Err(); err != nil {
					return err
				}
				subset := page
				subset.Glyphs = glyphs
				for _, text := range strings.Split(subset.Text(), "\n") {
					key := normalize(text)
					if previous, exists := sizes[key]; !exists || size < previous {
						sizes[key] = size
					}
				}
			}
		} else {
			addWarning(fmt.Sprintf("PDF page %d has too many font sizes for heading inference", page.Number))
		}
		var lines []line
		for _, text := range strings.Split(text, "\n") {
			text = strings.TrimSpace(text)
			if text != "" {
				lines = append(lines, line{text, sizes[normalize(text)]})
			}
		}
		pages = append(pages, lines)
		return nil
	})
	if errors.Is(err, pdf.ErrPasswordRequired) {
		return nil, nil, errors.New("PDF requires a password or the supplied password is incorrect; use --password or --password-env")
	}
	if err != nil {
		return nil, nil, fmt.Errorf("extract PDF: %w", err)
	}
	for _, warning := range result.Warnings {
		addWarning(warning.Error())
	}
	if textBytes == 0 {
		return nil, nil, errors.New("PDF contains no extractable text; scanned pages require OCR (not included), and outlined text requires a renderer")
	}
	if len(warnings) == 20 {
		warnings = append(warnings, "additional PDF warnings may have been omitted")
	}
	return buildDocument(pages, weights), warnings, nil
}

func normalize(text string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, text)
}

func buildDocument(pages [][]line, weights map[float64]int) *document.Document {
	body, weight := 12.0, 0
	for size, count := range weights {
		if count > weight || count == weight && size < body {
			body, weight = size, count
		}
	}
	headingSizes := map[float64]bool{}
	isHeading := func(l line) bool { return l.size > body*1.15 && utf8.RuneCountInString(l.text) <= 200 }
	for _, lines := range pages {
		for _, l := range lines {
			if isHeading(l) {
				headingSizes[l.size] = true
			}
		}
	}
	var ranked []float64
	for size := range headingSizes {
		ranked = append(ranked, size)
	}
	sort.Sort(sort.Reverse(sort.Float64Slice(ranked)))
	doc := &document.Document{}
	for _, lines := range pages {
		var paragraph []string
		flush := func() {
			if len(paragraph) > 0 {
				doc.Blocks = append(doc.Blocks, document.TextBlock(strings.Join(paragraph, "\n")))
				paragraph = nil
			}
		}
		for _, l := range lines {
			if isHeading(l) {
				flush()
				b := document.TextBlock(l.text)
				b.Kind = "heading"
				b.Level = min(6, sort.Search(len(ranked), func(i int) bool { return ranked[i] <= l.size })+1)
				doc.Blocks = append(doc.Blocks, b)
			} else {
				paragraph = append(paragraph, l.text)
			}
		}
		flush()
	}
	return doc
}
