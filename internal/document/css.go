/*
              _                                 
__      _____| |__  ___ _ __ ___   _____  _____ 
\ \ /\ / / _ \ '_ \/ __| '__/ __| / _ \ \/ / _ \
 \ V  V /  __/ |_) \__ \ | | (__ |  __/>  <  __/
  \_/\_/ \___|_.__/|___/_|  \___(_)___/_/\_\___|
                                                 
*/

package document

import (
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"golang.org/x/net/html"
)

type computedStyle struct {
	Size    float64
	Hidden  bool
	Display string
}
type declaration struct {
	name, value string
	important   bool
}
type cssRule struct {
	selector     string
	specificity  int
	declarations []declaration
}

var cssComments = regexp.MustCompile(`(?s)/\*.*?\*/`)
var sizeValue = regexp.MustCompile(`^([0-9]*\.?[0-9]+)(px|pt|em|rem|%)?$`)
var shorthandSize = regexp.MustCompile(`(?:^|\s)([0-9]*\.?[0-9]+(?:px|pt|rem|em|%))(?:[/\s]|$)`)
var simpleSelector = regexp.MustCompile(`^(?:[a-zA-Z][a-zA-Z0-9_-]*|\*)?(?:[.#][a-zA-Z0-9_-]+)*$`)
var selectorPart = regexp.MustCompile(`[.#]?[a-zA-Z0-9_-]+|\*`)

func declarations(body string) []declaration {
	var result []declaration
	for _, part := range strings.Split(body, ";") {
		name, value, ok := strings.Cut(part, ":")
		if !ok {
			continue
		}
		name, value = strings.ToLower(strings.TrimSpace(name)), strings.ToLower(strings.TrimSpace(value))
		important := false
		if i := strings.Index(value, "!important"); i >= 0 {
			important = true
			value = strings.TrimSpace(value[:i])
		}
		if name == "font" {
			if m := shorthandSize.FindStringSubmatch(value); len(m) > 1 {
				name, value = "font-size", m[1]
			}
		}
		switch name {
		case "font-size", "display", "visibility":
			result = append(result, declaration{name, value, important})
		}
	}
	return result
}

// Nested conditional rules are skipped because there is no browser viewport.
func parseCSS(css string) []cssRule {
	css = cssComments.ReplaceAllString(css, "")
	var result []cssRule
	for len(css) > 0 {
		open := strings.IndexByte(css, '{')
		if open < 0 {
			break
		}
		selector := strings.TrimSpace(css[:open])
		// Drop preceding statements such as @charset or @import.
		if i := strings.LastIndexByte(selector, ';'); i >= 0 {
			selector = strings.TrimSpace(selector[i+1:])
		}
		depth, end := 1, open+1
		var quote byte
		for ; end < len(css) && depth > 0; end++ {
			c := css[end]
			if quote != 0 {
				if c == '\\' {
					end++
				} else if c == quote {
					quote = 0
				}
				continue
			}
			if c == '\'' || c == '"' {
				quote = c
				continue
			}
			if c == '{' {
				depth++
			}
			if c == '}' {
				depth--
			}
		}
		if depth != 0 {
			break
		}
		body := css[open+1 : end-1]
		css = css[end:]
		if strings.HasPrefix(selector, "@") {
			continue
		}
		decls := declarations(body)
		if len(decls) == 0 {
			continue
		}
		for _, sel := range strings.Split(selector, ",") {
			sel = strings.TrimSpace(sel)
			specificity, ok := selectorSpecificity(sel)
			if ok {
				result = append(result, cssRule{sel, specificity, decls})
			}
		}
	}
	return result
}

func selectorSpecificity(selector string) (int, bool) {
	parts := strings.Fields(strings.ReplaceAll(selector, ">", " > "))
	if len(parts) == 0 || parts[0] == ">" || parts[len(parts)-1] == ">" {
		return 0, false
	}
	score := 0
	for i, part := range parts {
		if part == ">" {
			if i > 0 && parts[i-1] == ">" {
				return 0, false
			}
			continue
		}
		if part == ":root" {
			score += 10
			continue
		}
		if !simpleSelector.MatchString(part) {
			return 0, false
		}
		for _, token := range selectorPart.FindAllString(part, -1) {
			switch token[0] {
			case '#':
				score += 100
			case '.':
				score += 10
			case '*':
			default:
				score++
			}
		}
	}
	return score, true
}

func matchesSimple(n *html.Node, selector string) bool {
	if n == nil || n.Type != html.ElementNode {
		return false
	}
	if selector == ":root" {
		return n.Data == "html"
	}
	for _, token := range selectorPart.FindAllString(selector, -1) {
		switch token[0] {
		case '*':
		case '#':
			if attr(n, "id") != token[1:] {
				return false
			}
		case '.':
			found := false
			for _, class := range strings.Fields(attr(n, "class")) {
				if class == token[1:] {
					found = true
					break
				}
			}
			if !found {
				return false
			}
		default:
			if !strings.EqualFold(n.Data, token) {
				return false
			}
		}
	}
	return true
}

func matches(n *html.Node, selector string) bool {
	parts := strings.Fields(strings.ReplaceAll(selector, ">", " > "))
	var match func(*html.Node, int) bool
	match = func(n *html.Node, i int) bool {
		if !matchesSimple(n, parts[i]) {
			return false
		}
		if i == 0 {
			return true
		}
		if parts[i-1] == ">" {
			return i >= 2 && match(n.Parent, i-2)
		}
		for p := n.Parent; p != nil; p = p.Parent {
			if match(p, i-1) {
				return true
			}
		}
		return false
	}
	return match(n, len(parts)-1)
}

func fontSize(value string, parent, root float64) float64 {
	switch value {
	case "inherit", "unset":
		return parent
	case "initial", "medium":
		return 16
	case "xx-small":
		return 9
	case "x-small":
		return 10
	case "small":
		return 13
	case "large":
		return 18
	case "x-large":
		return 24
	case "xx-large":
		return 32
	case "xxx-large":
		return 48
	case "larger":
		return parent * 1.2
	case "smaller":
		return parent / 1.2
	}
	m := sizeValue.FindStringSubmatch(value)
	if len(m) == 0 {
		return parent
	}
	n, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return parent
	}
	switch m[2] {
	case "pt":
		n *= 96.0 / 72
	case "em":
		n *= parent
	case "rem":
		n *= root
	case "%":
		n *= parent / 100
	case "":
		if n != 0 {
			return parent
		}
	}
	if n < 0 || n > 10000 {
		return parent
	}
	return n
}

func computeStyles(root *html.Node, css string) map[*html.Node]computedStyle {
	rules := parseCSS(css)
	styles := map[*html.Node]computedStyle{}
	rootSize := 16.0
	var visit func(*html.Node, computedStyle)
	visit = func(n *html.Node, parent computedStyle) {
		style := parent
		style.Display = ""
		if n.Type == html.ElementNode {
			values := map[string]string{}
			scores := map[string]int{}
			apply := func(decls []declaration, specificity int) {
				for _, decl := range decls {
					score := specificity
					if decl.important {
						score += 1000000
					}
					if old, ok := scores[decl.name]; !ok || score >= old {
						scores[decl.name] = score
						values[decl.name] = decl.value
					}
				}
			}
			for _, rule := range rules {
				if matches(n, rule.selector) {
					apply(rule.declarations, rule.specificity)
				}
			}
			apply(declarations(attr(n, "style")), 1000)
			if value, ok := values["font-size"]; ok {
				style.Size = fontSize(value, parent.Size, rootSize)
			}
			if n.Data == "html" {
				rootSize = style.Size
			}
			style.Display = values["display"]
			style.Hidden = parent.Hidden || values["display"] == "none" || values["visibility"] == "hidden" || values["visibility"] == "collapse" || hasAttr(n, "hidden") || strings.EqualFold(attr(n, "aria-hidden"), "true")
		}
		styles[n] = style
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			visit(c, style)
		}
	}
	visit(root, computedStyle{Size: 16})
	return styles
}

func (d *Document) InferHeadings(css string) {
	d.styles = computeStyles(d.Root, css)
	d.Blocks = d.children(d.Root)
	weights := map[float64]int{}
	var paragraphs []*Block
	var collect func([]Block)
	collect = func(blocks []Block) {
		for i := range blocks {
			b := &blocks[i]
			if b.Kind == "paragraph" && b.Inline.Text != "" {
				paragraphs = append(paragraphs, b)
				for size, count := range d.blockTextSizes(b) {
					weights[size] += count
				}
			}
			collect(b.Children)
		}
	}
	collect(d.Blocks)
	bodySize, bodyWeight := 16.0, 0
	for size, weight := range weights {
		if weight > bodyWeight || weight == bodyWeight && size < bodySize {
			bodySize, bodyWeight = size, weight
		}
	}
	if bodyWeight == 0 {
		return
	}
	type candidate struct {
		block *Block
		size  float64
	}
	var candidates []candidate
	sizes := map[float64]bool{}
	for _, b := range paragraphs {
		if utf8.RuneCountInString(b.Inline.Text) > 200 || strings.Contains(b.Inline.Text, "\n") {
			continue
		}
		most, dominant, total := 0, 0.0, 0
		for size, count := range d.blockTextSizes(b) {
			total += count
			if count > most || count == most && size < dominant {
				most, dominant = count, size
			}
		}
		if dominant > bodySize*1.15 && most*4 >= total*3 {
			candidates = append(candidates, candidate{b, dominant})
			sizes[dominant] = true
		}
	}
	var ranked []float64
	for size := range sizes {
		ranked = append(ranked, size)
	}
	sort.Sort(sort.Reverse(sort.Float64Slice(ranked)))
	for _, c := range candidates {
		level := sort.Search(len(ranked), func(i int) bool { return ranked[i] <= c.size })
		c.block.Kind, c.block.Level = "heading", min(6, level+1)
	}
}

func (d *Document) blockTextSizes(b *Block) map[float64]int {
	weights := map[float64]int{}
	var visit func(*html.Node)
	visit = func(n *html.Node) {
		if d.hidden(n) {
			return
		}
		if n.Type == html.TextNode {
			count := utf8.RuneCountInString(strings.Join(strings.Fields(n.Data), ""))
			size := math.Round(d.styles[n].Size*10) / 10
			weights[size] += count
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			visit(c)
		}
	}
	for _, n := range b.nodes {
		visit(n)
	}
	return weights
}
