/*
              _                                 
__      _____| |__  ___ _ __ ___   _____  _____ 
\ \ /\ / / _ \ '_ \/ __| '__/ __| / _ \ \/ / _ \
 \ V  V /  __/ |_) \__ \ | | (__ |  __/>  <  __/
  \_/\_/ \___|_.__/|___/_|  \___(_)___/_/\_\___|
                                                 
*/

// Package document extracts page structure without executing active content.
package document

import (
	"fmt"
	"io"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/net/html"
)

type Inline struct{ MD, Text string }
type Block struct {
	Kind         string
	Level, Start int
	Ordered      bool
	Inline       Inline
	Children     []Block
	Rows         [][]Inline
	Language     string
	node         *html.Node
	nodes        []*html.Node
}
type Document struct {
	Root   *html.Node
	Base   *url.URL
	Blocks []Block
	styles map[*html.Node]computedStyle
}

// TextBlock creates a paragraph from extracted text without interpreting it as
// HTML or Markdown. PDF text uses the same renderers as HTML-derived blocks.
func TextBlock(text string) Block {
	lines := strings.Split(markdownEscaper.Replace(text), "\n")
	for i := range lines {
		lines[i] = escapeBlockStart(lines[i])
	}
	return Block{Kind: "paragraph", Inline: Inline{MD: strings.Join(lines, "  \n"), Text: text}}
}

func Parse(r io.Reader, base *url.URL) (*Document, error) {
	root, err := html.Parse(r)
	if err != nil {
		return nil, err
	}
	d := &Document{Root: root, Base: base}
	// The first valid base href controls relative links, as in a browser.
	walk(root, func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "base" && attr(n, "href") != "" && d.Base == base {
			if u, err := url.Parse(attr(n, "href")); err == nil && base != nil {
				resolved := base.ResolveReference(u)
				if resolved.Scheme == "http" || resolved.Scheme == "https" {
					d.Base = resolved
				}
			}
		}
	})
	d.styles = computeStyles(root, d.EmbeddedCSS())
	d.Blocks = d.children(root)
	return d, nil
}

func attr(n *html.Node, name string) string {
	for _, a := range n.Attr {
		if a.Key == name {
			return a.Val
		}
	}
	return ""
}
func hasAttr(n *html.Node, name string) bool {
	for _, a := range n.Attr {
		if a.Key == name {
			return true
		}
	}
	return false
}
func walk(n *html.Node, fn func(*html.Node)) {
	fn(n)
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		walk(c, fn)
	}
}

func (d *Document) hidden(n *html.Node) bool {
	if n.Type != html.ElementNode {
		return false
	}
	switch n.Data {
	case "head", "script", "style", "noscript", "template", "svg", "canvas", "iframe", "object":
		return true
	}
	return hasAttr(n, "hidden") || strings.EqualFold(attr(n, "aria-hidden"), "true") || d.styles[n].Hidden
}

func (d *Document) isBlock(n *html.Node) bool {
	if n.Type != html.ElementNode {
		return false
	}
	switch n.Data {
	case "html", "body", "main", "article", "section", "div", "header", "footer", "nav", "aside", "p", "h1", "h2", "h3", "h4", "h5", "h6", "ul", "ol", "li", "blockquote", "pre", "table", "hr", "figure", "figcaption", "address", "form", "fieldset", "details", "summary", "dl", "dt", "dd":
		return true
	}
	style := d.styles[n]
	return style.Display == "block" || style.Display == "flex" || style.Display == "grid"
}

// children groups adjacent inline nodes, retaining block boundaries and order.
func (d *Document) children(n *html.Node) []Block {
	var blocks []Block
	var run []*html.Node
	flush := func() {
		if len(run) == 0 {
			return
		}
		i := d.inlineNodes(run)
		if i.Text != "" || i.MD != "" {
			blocks = append(blocks, Block{Kind: "paragraph", Inline: i, node: n, nodes: run})
		}
		run = nil
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if d.hidden(c) {
			continue
		}
		if d.isBlock(c) {
			flush()
			blocks = append(blocks, d.block(c)...)
		} else {
			run = append(run, c)
		}
	}
	flush()
	return blocks
}

func (d *Document) block(n *html.Node) []Block {
	if d.hidden(n) {
		return nil
	}
	b := Block{node: n}
	switch n.Data {
	case "h1", "h2", "h3", "h4", "h5", "h6":
		b.Kind, b.Level, b.Inline = "heading", int(n.Data[1]-'0'), d.inline(n)
	case "pre":
		b.Kind = "code"
		b.Inline.Text = strings.Trim(d.rawText(n), "\r\n")
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Data == "code" {
				for _, class := range strings.Fields(attr(c, "class")) {
					if strings.HasPrefix(class, "language-") {
						b.Language = safeLanguage(strings.TrimPrefix(class, "language-"))
					}
				}
			}
		}
	case "ul", "ol":
		b.Kind, b.Ordered, b.Start = "list", n.Data == "ol", 1
		if start, err := strconv.Atoi(attr(n, "start")); err == nil && start > 0 {
			b.Start = start
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == html.ElementNode && c.Data == "li" && !d.hidden(c) {
				b.Children = append(b.Children, Block{Kind: "item", Children: d.children(c), node: c})
			}
		}
	case "blockquote":
		b.Kind, b.Children = "quote", d.children(n)
	case "table":
		b.Kind = "table"
		var collect func(*html.Node)
		collect = func(parent *html.Node) {
			for c := parent.FirstChild; c != nil; c = c.NextSibling {
				if d.hidden(c) {
					continue
				}
				if c.Type == html.ElementNode && c.Data == "table" {
					continue
				}
				if c.Type == html.ElementNode && c.Data == "tr" {
					var row []Inline
					for cell := c.FirstChild; cell != nil; cell = cell.NextSibling {
						if (cell.Data == "td" || cell.Data == "th") && !d.hidden(cell) {
							row = append(row, d.inline(cell))
						}
					}
					if len(row) > 0 {
						b.Rows = append(b.Rows, row)
					}
				} else {
					collect(c)
				}
			}
		}
		collect(n)
		var result []Block
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Data == "caption" {
				i := d.inline(c)
				if i.Text != "" {
					result = append(result, Block{Kind: "paragraph", Inline: i, node: c})
				}
			}
		}
		if len(b.Rows) > 0 {
			result = append(result, b)
		}
		return result
	case "hr":
		b.Kind = "rule"
	default:
		return d.children(n)
	}
	if b.Kind == "heading" && b.Inline.Text == "" {
		return nil
	}
	return []Block{b}
}

var spaces = regexp.MustCompile(`[\t\n\r\f ]+`)
var markdownEscaper = strings.NewReplacer(`\`, `\\`, "*", `\*`, "_", `\_`, "[", `\[`, "]", `\]`, "`", "\\`", "#", `\#`, "<", `\<`, ">", `\>`, "!", `\!`, "|", `\|`, "~", `\~`, "&", `\&`)

func (d *Document) inlineNodes(nodes []*html.Node) Inline {
	var md, text strings.Builder
	for _, n := range nodes {
		i := d.inlineNode(n)
		md.WriteString(i.MD)
		text.WriteString(i.Text)
	}
	return Inline{strings.TrimSpace(md.String()), strings.TrimSpace(text.String())}
}
func (d *Document) inline(n *html.Node) Inline {
	var nodes []*html.Node
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		nodes = append(nodes, c)
	}
	return d.inlineNodes(nodes)
}
func (d *Document) inlineNode(n *html.Node) Inline {
	if d.hidden(n) {
		return Inline{}
	}
	if n.Type == html.TextNode {
		text := spaces.ReplaceAllString(n.Data, " ")
		return Inline{markdownEscaper.Replace(text), text}
	}
	if n.Type != html.ElementNode {
		return Inline{}
	}
	if n.Data == "br" {
		return Inline{"  \n", "\n"}
	}
	if n.Data == "img" {
		alt := spaces.ReplaceAllString(attr(n, "alt"), " ")
		link := d.link(attr(n, "src"))
		if link == "" {
			return Inline{markdownEscaper.Replace(alt), alt}
		}
		return Inline{"![" + markdownEscaper.Replace(alt) + "](<" + link + ">)", alt}
	}
	// Keep whitespace around emphasis outside its Markdown delimiters.
	var md, text strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		i := d.inlineNode(c)
		md.WriteString(i.MD)
		text.WriteString(i.Text)
	}
	i := Inline{md.String(), text.String()}
	wrap := func(marker string) {
		t := strings.TrimSpace(i.MD)
		if t != "" {
			left := i.MD[:len(i.MD)-len(strings.TrimLeft(i.MD, " \r\n\t"))]
			right := i.MD[len(strings.TrimRight(i.MD, " \r\n\t")):]
			i.MD = left + marker + t + marker + right
		}
	}
	switch n.Data {
	case "strong", "b":
		wrap("**")
	case "em", "i":
		wrap("*")
	case "del", "s", "strike":
		wrap("~~")
	case "code", "kbd", "samp":
		text := spaces.ReplaceAllString(d.rawText(n), " ")
		fence := strings.Repeat("`", longestRun(text, '`')+1)
		padding := ""
		if strings.HasPrefix(text, "`") || strings.HasSuffix(text, "`") || (strings.HasPrefix(text, " ") && strings.HasSuffix(text, " ") && strings.TrimSpace(text) != "") {
			padding = " "
		}
		i.MD = fence + padding + text + padding + fence
	case "a":
		if link := d.link(attr(n, "href")); link != "" && strings.TrimSpace(i.MD) != "" {
			i.MD = "[" + i.MD + "](<" + link + ">)"
		}
	case "input":
		if strings.EqualFold(attr(n, "type"), "checkbox") {
			if hasAttr(n, "checked") {
				return Inline{"[x] ", "[x] "}
			}
			return Inline{"[ ] ", "[ ] "}
		}
	}
	if d.isBlock(n) {
		i.MD += "\n"
		i.Text += "\n"
	}
	return i
}

func longestRun(s string, char rune) int {
	maxRun, run := 0, 0
	for _, r := range s {
		if r == char {
			run++
			if run > maxRun {
				maxRun = run
			}
		} else {
			run = 0
		}
	}
	return maxRun
}
func safeLanguage(s string) string {
	var b strings.Builder
	for _, c := range s {
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == '+' {
			b.WriteRune(c)
		}
	}
	return b.String()
}
func (d *Document) rawText(n *html.Node) string {
	var b strings.Builder
	var visit func(*html.Node)
	visit = func(n *html.Node) {
		if d.hidden(n) {
			return
		}
		if n.Type == html.TextNode {
			b.WriteString(n.Data)
		}
		if n.Type == html.ElementNode && n.Data == "br" {
			b.WriteByte('\n')
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			visit(c)
		}
	}
	visit(n)
	return b.String()
}

func (d *Document) link(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return ""
	}
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	if d.Base != nil {
		u = d.Base.ResolveReference(u)
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https", "mailto", "tel", "":
	default:
		return ""
	}
	// Angle brackets delimit destinations, so encode literal brackets too.
	return strings.NewReplacer("<", "%3C", ">", "%3E", "\n", "%0A", "\r", "%0D").Replace(u.String())
}

func (d *Document) EmbeddedCSS() string {
	return d.CSSInOrder(nil)
}

// CSSInOrder preserves document order between style tags and downloaded sheets.
func (d *Document) CSSInOrder(external map[string]string) string {
	var css strings.Builder
	walk(d.Root, func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "style" && (attr(n, "media") == "" || attr(n, "media") == "all" || attr(n, "media") == "screen") {
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				if c.Type == html.TextNode {
					css.WriteString(c.Data)
					css.WriteByte('\n')
				}
			}
		}
		if n.Type == html.ElementNode && n.Data == "link" {
			u, err := url.Parse(attr(n, "href"))
			if err == nil {
				if d.Base != nil {
					u = d.Base.ResolveReference(u)
				}
				if sheet, ok := external[u.String()]; ok {
					css.WriteString(sheet)
					css.WriteByte('\n')
				}
			}
		}
	})
	return css.String()
}
func (d *Document) StylesheetURLs() []*url.URL {
	var result []*url.URL
	seen := map[string]bool{}
	walk(d.Root, func(n *html.Node) {
		if n.Type != html.ElementNode || n.Data != "link" {
			return
		}
		rel := " " + strings.ToLower(strings.Join(strings.Fields(attr(n, "rel")), " ")) + " "
		if !strings.Contains(rel, " stylesheet ") || hasAttr(n, "disabled") || (attr(n, "media") != "" && attr(n, "media") != "screen" && attr(n, "media") != "all") {
			return
		}
		u, err := url.Parse(attr(n, "href"))
		if err != nil || attr(n, "href") == "" {
			return
		}
		if d.Base != nil {
			u = d.Base.ResolveReference(u)
		}
		if !seen[u.String()] {
			seen[u.String()] = true
			result = append(result, u)
		}
	})
	return result
}

func (d *Document) Render(format string) string {
	if format == "xml" || format == "txtx" {
		return renderXML(d.Blocks)
	}
	text := renderBlocks(d.Blocks, format == "txt")
	if strings.TrimSpace(text) == "" {
		return ""
	}
	return strings.TrimSpace(text) + "\n"
}

func renderBlocks(blocks []Block, plain bool) string {
	var parts []string
	for _, b := range blocks {
		i := b.Inline.MD
		if plain {
			i = b.Inline.Text
		}
		var s string
		switch b.Kind {
		case "heading":
			s = i
			if !plain {
				s = strings.Repeat("#", b.Level) + " " + i
			}
		case "paragraph":
			s = i
			if !plain {
				s = escapeBlockStart(s)
			}
		case "code":
			s = b.Inline.Text
			if !plain {
				fence := strings.Repeat("`", max(3, longestRun(s, '`')+1))
				s = fence + b.Language + "\n" + s + "\n" + fence
			}
		case "rule":
			if !plain {
				s = "---"
			}
		case "quote":
			s = renderBlocks(b.Children, plain)
			if !plain {
				s = "> " + strings.ReplaceAll(s, "\n", "\n> ")
			}
		case "list":
			var items []string
			for j, item := range b.Children {
				body := strings.TrimSpace(renderBlocks(item.Children, plain))
				prefix := "- "
				if b.Ordered {
					prefix = fmt.Sprintf("%d. ", b.Start+j)
				}
				items = append(items, prefix+strings.ReplaceAll(body, "\n", "\n"+strings.Repeat(" ", len(prefix))))
			}
			s = strings.Join(items, "\n")
		case "table":
			width := 0
			for _, row := range b.Rows {
				width = max(width, len(row))
			}
			var rows []string
			for j, row := range b.Rows {
				cells := make([]string, width)
				for k, cell := range row {
					cells[k] = cell.MD
					if plain {
						cells[k] = cell.Text
					} else {
						cells[k] = escapeTableCell(strings.ReplaceAll(cells[k], "\n", "<br>"))
					}
				}
				if plain {
					rows = append(rows, strings.Join(cells, "\t"))
				} else {
					rows = append(rows, "| "+strings.Join(cells, " | ")+" |")
					if j == 0 {
						divider := make([]string, width)
						for k := range divider {
							divider[k] = "---"
						}
						rows = append(rows, "| "+strings.Join(divider, " | ")+" |")
					}
				}
			}
			s = strings.Join(rows, "\n")
		}
		if s != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, "\n\n")
}

var listStart = regexp.MustCompile(`^(\d+)([.)]) `)
var thematicBreak = regexp.MustCompile(`^(?:[- ]{3,}|[= ]{3,})$`)

func escapeBlockStart(s string) string {
	if strings.HasPrefix(s, "- ") || strings.HasPrefix(s, "+ ") || strings.HasPrefix(s, "= ") {
		return `\` + s
	}
	if thematicBreak.MatchString(s) {
		return `\` + s
	}
	return listStart.ReplaceAllString(s, `${1}\${2} `)
}

func escapeTableCell(s string) string {
	var out strings.Builder
	slashes := 0
	for _, r := range s {
		if r == '|' && slashes%2 == 0 {
			out.WriteByte('\\')
		}
		out.WriteRune(r)
		if r == '\\' {
			slashes++
		} else {
			slashes = 0
		}
	}
	return out.String()
}

func renderXML(blocks []Block) string {
	var out strings.Builder
	out.WriteString("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<document>\n")
	var render func([]Block, int)
	render = func(blocks []Block, depth int) {
		indent := strings.Repeat("  ", depth)
		text := func(tag, value string) {
			out.WriteString(indent + "<" + tag + ">" + xmlText(value) + "</" + tag + ">\n")
		}
		for _, b := range blocks {
			switch b.Kind {
			case "heading":
				text(fmt.Sprintf("header%d", b.Level), b.Inline.Text)
			case "paragraph":
				text("p", b.Inline.Text)
			case "code":
				text("pre", b.Inline.Text)
			case "rule":
				out.WriteString(indent + "<hr/>\n")
			case "quote", "list", "item":
				tag := "blockquote"
				if b.Kind == "item" {
					tag = "li"
				}
				if b.Kind == "list" {
					tag = "ul"
					if b.Ordered {
						tag = "ol"
					}
				}
				opening := tag
				if tag == "ol" && b.Start != 1 {
					opening = fmt.Sprintf("ol start=\"%d\"", b.Start)
				}
				out.WriteString(indent + "<" + opening + ">\n")
				render(b.Children, depth+1)
				out.WriteString(indent + "</" + tag + ">\n")
			case "table":
				out.WriteString(indent + "<table>\n")
				for _, row := range b.Rows {
					out.WriteString(indent + "  <tr>\n")
					for _, cell := range row {
						out.WriteString(indent + "    <td>" + xmlText(cell.Text) + "</td>\n")
					}
					out.WriteString(indent + "  </tr>\n")
				}
				out.WriteString(indent + "</table>\n")
			}
		}
	}
	render(blocks, 1)
	out.WriteString("</document>\n")
	return out.String()
}

func xmlText(text string) string {
	// XML 1.0 excludes most control characters, even if HTML accepted them.
	text = strings.Map(func(r rune) rune {
		if r == '\t' || r == '\n' || r == '\r' || r >= 0x20 && r <= 0xD7FF || r >= 0xE000 && r <= 0xFFFD || r >= 0x10000 && r <= 0x10FFFF {
			return r
		}
		return -1
	}, text)
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", "\"", "&quot;", "'", "&apos;").Replace(text)
}
