/*
              _                                 
__      _____| |__  ___ _ __ ___   _____  _____ 
\ \ /\ / / _ \ '_ \/ __| '__/ __| / _ \ \/ / _ \
 \ V  V /  __/ |_) \__ \ | | (__ |  __/>  <  __/
  \_/\_/ \___|_.__/|___/_|  \___(_)___/_/\_\___|
                                                 
*/

package document

import (
	"encoding/xml"
	"io"
	"net/url"
	"strings"
	"testing"
)

func parsePage(t *testing.T, source string) *Document {
	t.Helper()
	base, _ := url.Parse("https://example.test/articles/page.html")
	doc, err := Parse(strings.NewReader(source), base)
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

func TestStructuredMarkdown(t *testing.T) {
	doc := parsePage(t, `<html><head><title>Not body text</title></head><body>
<h1>Title &amp; more</h1><h2>Subheading</h2>
<p>Hello <strong>bold</strong> and <em>italic</em>. <a href="../guide?q=1&amp;b=2">Guide</a></p>
<ol start="3"><li>First<ul><li>Nested</li></ul></li><li>Second</li></ol>
<blockquote><p>A quote</p><p>Another paragraph</p></blockquote>
<pre><code class="language-go">if a &lt; b {
  println("`+"```"+`")
}</code></pre>
<table><caption>Numbers</caption><tr><th>Name</th><th>Value</th></tr><tr><td>A</td><td>2</td></tr></table>
<script>secret script</script><p hidden>hidden text</p><p aria-hidden="true">aria secret</p>
</body></html>`)
	got := doc.Render("md")
	for _, want := range []string{
		`# Title \& more`, "## Subheading", "Hello **bold** and *italic*.",
		"[Guide](<https://example.test/guide?q=1&b=2>)", "3. First\n   \n   - Nested\n4. Second",
		"> A quote\n> \n> Another paragraph", "````go\nif a < b {", "| Name | Value |\n| --- | --- |\n| A | 2 |",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	for _, forbidden := range []string{"Not body text", "secret script", "hidden text", "aria secret"} {
		if strings.Contains(got, forbidden) {
			t.Errorf("unexpected %q", forbidden)
		}
	}
}

func TestPlainTextAndXML(t *testing.T) {
	doc := parsePage(t, `<h1>A &amp; B</h1><p><strong>Bold</strong> and <a href="/x">link</a> &lt;value&gt;</p><ul><li>One</li></ul>`)
	plain := doc.Render("txt")
	if plain != "A & B\n\nBold and link <value>\n\n- One\n" {
		t.Fatalf("plain: %q", plain)
	}
	tagged := doc.Render("txtx")
	if !strings.Contains(tagged, "<header1>A &amp; B</header1>") || !strings.Contains(tagged, "<p>Bold and link &lt;value&gt;</p>") {
		t.Fatal(tagged)
	}
	if strings.Contains(tagged, "<strong>") || strings.Contains(tagged, "<a ") {
		t.Fatal("styling leaked into tagged output")
	}
	decoder := xml.NewDecoder(strings.NewReader(tagged))
	for {
		_, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	if doc.Render("xml") != tagged {
		t.Fatal("XML and tagged text structures should match")
	}
}

func TestMalformedHTMLAndInlineBoundaries(t *testing.T) {
	doc := parsePage(t, `<h1>Heading</h1><div>Before <span>inside</span><div>Inner</div>After</div><ul><li>A<li>B</ul><p>X<br>Y`)
	got := doc.Render("txt")
	if got != "Heading\n\nBefore inside\n\nInner\n\nAfter\n\n- A\n- B\n\nX\nY\n" {
		t.Fatal(got)
	}
}

func TestLinksImagesAndLiteralMarkdown(t *testing.T) {
	doc := parsePage(t, `<base href="https://assets.test/root/"><p><a href="javascript:alert(1)">Safe label</a></p><p><img src="pic.png" alt=""></p><p>*literal* &lt;tag&gt; &amp;copy;</p><p>1. Not a list</p><p>---</p><p><code>a`+"`"+`b</code></p>`)
	got := doc.Render("md")
	for _, want := range []string{"Safe label", "![](<https://assets.test/root/pic.png>)", `\*literal\* \<tag\> \&copy;`, `1\. Not a list`, `\---`, "``a`b``"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %s", want, got)
		}
	}
	if strings.Contains(got, "javascript:") {
		t.Fatal(got)
	}
}

func TestCSSInferenceAndCascade(t *testing.T) {
	doc := parsePage(t, `<style>
html { font-size: 16px } body { font-size: 1rem }
.title { font-size: 30px } #title { font-size: 2.5rem !important }
main > .subtitle { font-size: 150% } .hidden { display: none }
@media (max-width: 500px) { .title { font-size: 8px } }
</style><main><div class="title" id="title" style="font-size:12px">Big title</div>
<div class="subtitle">Smaller title</div>
<div>This is the body paragraph, with enough ordinary text to establish the most common size.</div>
<p class="hidden">This should disappear.</p><h3>Real heading</h3></main>`)
	doc.InferHeadings(doc.EmbeddedCSS())
	got := doc.Render("mdx")
	for _, want := range []string{"# Big title", "## Smaller title", "### Real heading", "\n\nThis is the body"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %s", want, got)
		}
	}
	if strings.Contains(got, "disappear") {
		t.Fatal(got)
	}
}

func TestInferenceUsesOnlyTextInItsBlock(t *testing.T) {
	doc := parsePage(t, `<div><span style="font-size:32px">Title</span><div>Long body paragraph with many characters, ordinary text, and an ordinary font size.</div><span style="font-size:24px">Subtitle</span></div>`)
	doc.InferHeadings("")
	got := doc.Render("mdx")
	if !strings.HasPrefix(got, "# Title\n") || !strings.Contains(got, "## Subtitle") {
		t.Fatal(got)
	}
}

func TestCSSSourceOrder(t *testing.T) {
	doc := parsePage(t, `<link rel="stylesheet" href="/first.css"><style>.title{font-size:32px}</style><link rel="stylesheet" href="/last.css"><div class="title">Title</div><div>Body text is much longer than the title and determines the baseline font.</div>`)
	css := doc.CSSInOrder(map[string]string{"https://example.test/first.css": ".title{font-size:40px}", "https://example.test/last.css": ".title{font-size:16px}"})
	doc.InferHeadings(css)
	if strings.HasPrefix(doc.Render("mdx"), "#") {
		t.Fatal("later external rule should win")
	}
}

func TestCSSFontUnits(t *testing.T) {
	for _, tt := range []struct {
		value              string
		parent, root, want float64
	}{{"24px", 16, 16, 24}, {"18pt", 16, 16, 24}, {"1.5em", 20, 16, 30}, {"2rem", 20, 18, 36}, {"150%", 20, 16, 30}, {"calc(2px + 1em)", 20, 16, 20}} {
		if got := fontSize(tt.value, tt.parent, tt.root); got != tt.want {
			t.Errorf("%s: %g != %g", tt.value, got, tt.want)
		}
	}
}

func TestTableCodePipesAndParagraphBoundaries(t *testing.T) {
	doc := parsePage(t, `<table><tr><th>Column</th></tr><tr><td><p>One</p><p>Two <code>x|y</code> and a|b</p></td></tr></table>`)
	got := doc.Render("md")
	if !strings.Contains(got, "| One<br>Two `x\\|y` and a\\|b |") {
		t.Fatal(got)
	}
	if !strings.Contains(doc.Render("txt"), "One\nTwo x|y and a|b") {
		t.Fatal(doc.Render("txt"))
	}
}

func TestXMLControlCharacters(t *testing.T) {
	if got := xmlText("A\x01<&B"); got != "A&lt;&amp;B" {
		t.Fatal(got)
	}
}

func FuzzParseAndRender(f *testing.F) {
	for _, seed := range []string{"<h1>Hello</h1><p>World</p>", "<table><tr><td>X", "<div style='font-size:32px'>Title</div><p>Body body body body body</p>"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, source string) {
		if len(source) > 100000 {
			t.Skip()
		}
		doc, err := Parse(strings.NewReader(source), nil)
		if err != nil {
			return
		}
		for _, format := range []string{"md", "txt", "xml"} {
			result := doc.Render(format)
			if format == "xml" {
				decoder := xml.NewDecoder(strings.NewReader(result))
				for {
					_, err := decoder.Token()
					if err == io.EOF {
						break
					}
					if err != nil {
						t.Fatalf("invalid XML: %v", err)
					}
				}
			}
		}
		doc.InferHeadings(doc.EmbeddedCSS())
		_ = doc.Render("mdx")
	})
}
