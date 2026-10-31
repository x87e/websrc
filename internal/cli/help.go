/*
              _                                 
__      _____| |__  ___ _ __ ___   _____  _____ 
\ \ /\ / / _ \ '_ \/ __| '__/ __| / _ \ \/ / _ \
 \ V  V /  __/ |_) \__ \ | | (__ |  __/>  <  __/
  \_/\_/ \___|_.__/|___/_|  \___(_)___/_/\_\___|
                                                 
*/

package cli

const help = `websrc - extract web pages and PDF documents

Usage:
  websrc URL [format] [DIRECTORY] [options]
  websrc FILE [format] [DIRECTORY] [options]  (local HTML or PDF)
  websrc --input FILE [format] [options]
  websrc @arguments.txt
  websrc arguments.txt             (also drag a .txt file onto websrc.exe)
  websrc                          (interactive prompt)

Formats (default: raw source; .txt for HTML, .pdf for PDF):
  -raw     Original response bytes, including HTML markup
  -md      Markdown: headings, links, lists, tables, quotes and code
  -txt     Plain text with section breaks
  -txtx    Text with simple structural tags, saved as .txt
  -xml     The same structural tags, saved as .xml
  -mdx     Markdown with font-size heading inference (CSS for HTML)

Options (may appear before or after the URL):
  -o, --output FILE   Exact output filename; use - for stdout
  -d, --dir DIR       Output directory (created if necessary)
  --stdout           Write only the result to stdout, for use by other apps
  --input FILE       Convert an existing local HTML or PDF file
  --password TEXT    Password for an encrypted PDF
  --password-env VAR Read the PDF password from environment variable VAR
  --overwrite        Allow replacing an existing output file
  --timeout 30s      Download/extraction deadline (also limits CSS requests)
  --max-bytes N      Maximum source size; default 20971520 (20 MiB)
  --user-agent TEXT  Set the HTTP User-Agent
  --format FORMAT    raw, md, txt, txtx, xml or mdx
  --version          Print version
  -h, --help         Show this help

Examples:
  websrc https://example.com/index.html
  websrc https://example.com/index.html -md "%APPDATA%"
  websrc https://example.com -txt --stdout
  websrc https://example.com -txtx -o "C:\Exports\page.txt"
  websrc --input saved.html -md --stdout
  websrc "C:\Documents\report.pdf" -txt --stdout
  websrc https://example.com/report.pdf -md -o report.md

Default output directory: the directory containing websrc.exe.
Diagnostics and saved-file paths go to stderr. Exit codes: 0 success,
1 download/conversion/write failure, 2 invalid arguments.
Argument files accept quoted paths, newlines and whole-line # comments.
No JavaScript is executed. -mdx uses static CSS heuristics; see README.md.
PDF text layers are supported; scanned images require OCR, not bundled.
PDF headings are inferred from font sizes in Markdown/tagged output.
PDF tables/links/styles are exported as text, without semantic reconstruction.
`
