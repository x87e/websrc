/*
              _                                 
__      _____| |__  ___ _ __ ___   _____  _____ 
\ \ /\ / / _ \ '_ \/ __| '__/ __| / _ \ \/ / _ \
 \ V  V /  __/ |_) \__ \ | | (__ |  __/>  <  __/
  \_/\_/ \___|_.__/|___/_|  \___(_)___/_/\_\___|
                                                 
*/

package cli

import (
	"bufio"
	"bytes"
	"context"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"golang.org/x/net/html/charset"
	"websrc/internal/document"
	"websrc/internal/pdfdoc"
)

//go:embed banner.txt
var terminalBanner string

func Run(args []string, in io.Reader, out, diagnostic io.Writer, executable, version string) int {
	if len(args) == 0 {
		return interactive(in, out, diagnostic, executable, version)
	}
	return runCommand(args, out, diagnostic, executable, version)
}

func runCommand(args []string, out, diagnostic io.Writer, executable, version string) int {
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(diagnostic, "websrc:", err)
		return 1
	}
	args, err = expandArgs(args, cwd, 0, map[string]bool{})
	if err != nil {
		fmt.Fprintln(diagnostic, "websrc:", err)
		return 2
	}
	o, err := parseOptions(args)
	if err != nil {
		fmt.Fprintln(diagnostic, "websrc:", err)
		return 2
	}
	if o.Help {
		fmt.Fprint(out, help)
		return 0
	}
	if o.Version {
		fmt.Fprintln(out, "websrc", version)
		return 0
	}
	if err := execute(o, out, diagnostic, executable); err != nil {
		fmt.Fprintln(diagnostic, "websrc:", err)
		return 1
	}
	return 0
}

func interactive(in io.Reader, out, diagnostic io.Writer, executable, version string) int {
	fmt.Fprint(diagnostic, terminalBanner)
	if !strings.HasSuffix(terminalBanner, "\n") {
		fmt.Fprintln(diagnostic)
	}
	fmt.Fprintln(diagnostic, "websrc", version, "- HTML and PDF downloader and converter")
	fmt.Fprintln(diagnostic, "Enter a URL or local HTML/PDF path with options (for example: report.pdf -md).")
	fmt.Fprintln(diagnostic, "Type help for all commands, or exit to close.")
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 4096), 1<<20)
	lastCode := 0
	for {
		fmt.Fprint(diagnostic, "websrc> ")
		if !scanner.Scan() {
			break
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "exit" || line == "quit" {
			break
		}
		if line == "" {
			continue
		}
		if line == "help" {
			fmt.Fprint(diagnostic, help)
			continue
		}
		args, err := splitArgs(line)
		if err != nil {
			fmt.Fprintln(diagnostic, "websrc:", err)
			lastCode = 2
			continue
		}
		if len(args) > 0 && (strings.EqualFold(args[0], "websrc") || strings.EqualFold(args[0], "websrc.exe")) {
			args = args[1:]
		}
		lastCode = runCommand(args, out, diagnostic, executable, version)
	}
	if err := scanner.Err(); err != nil {
		fmt.Fprintln(diagnostic, "websrc:", err)
		return 1
	}
	return lastCode
}

type resource struct {
	Data        []byte
	URL         *url.URL
	ContentType string
}

func readLimited(r io.Reader, max int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > max {
		return nil, fmt.Errorf("input exceeds the %d-byte limit", max)
	}
	return data, nil
}

func fetch(ctx context.Context, client *http.Client, address, agent string, max int64) (resource, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return resource{}, err
	}
	req.Header.Set("User-Agent", agent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/pdf,text/plain;q=0.9,*/*;q=0.5")
	resp, err := client.Do(req)
	if err != nil {
		return resource{}, fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return resource{}, fmt.Errorf("server returned %s", resp.Status)
	}
	if resp.ContentLength > max {
		return resource{}, fmt.Errorf("response exceeds the %d-byte limit", max)
	}
	data, err := readLimited(resp.Body, max)
	if err != nil {
		return resource{}, fmt.Errorf("download: %w", err)
	}
	return resource{data, resp.Request.URL, resp.Header.Get("Content-Type")}, nil
}

func execute(o options, out, diagnostic io.Writer, executable string) error {
	ctx, cancel := context.WithTimeout(context.Background(), o.Timeout)
	defer cancel()
	client := &http.Client{Timeout: o.Timeout, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return errors.New("stopped after 10 redirects")
		}
		if req.URL.Scheme != "https" && req.URL.Scheme != "http" {
			return errors.New("redirect must use HTTP(S)")
		}
		return nil
	}}
	var src resource
	var err error
	if o.Input != "" {
		var f *os.File
		f, err = os.Open(o.Input)
		if err != nil {
			return fmt.Errorf("open input: %w", err)
		}
		src.Data, err = readLimited(f, o.MaxBytes)
		f.Close()
		abs, absErr := filepath.Abs(o.Input)
		if absErr != nil {
			return absErr
		}
		path := filepath.ToSlash(abs)
		if !strings.HasPrefix(path, "/") {
			path = "/" + path
		}
		src.URL = &url.URL{Scheme: "file", Path: path}
	} else {
		src, err = fetch(ctx, client, o.URL, o.UserAgent, o.MaxBytes)
	}
	if err != nil {
		return err
	}
	output := src.Data
	pdfInput := isPDF(src)
	if o.Format != "raw" {
		var doc *document.Document
		if pdfInput {
			var warnings []string
			doc, warnings, err = pdfdoc.Parse(ctx, src.Data, o.Password)
			if err != nil {
				return err
			}
			for _, warning := range warnings {
				fmt.Fprintln(diagnostic, "websrc: warning:", warning)
			}
		} else {
			media, _, _ := mime.ParseMediaType(src.ContentType)
			if media != "" && media != "text/html" && media != "application/xhtml+xml" && media != "text/plain" && media != "application/octet-stream" {
				return fmt.Errorf("cannot convert content type %q; use -raw to save the original", media)
			}
			reader, err := charset.NewReader(bytes.NewReader(src.Data), src.ContentType)
			if err != nil {
				return fmt.Errorf("decode HTML: %w", err)
			}
			doc, err = document.Parse(reader, src.URL)
			if err != nil {
				return fmt.Errorf("parse HTML: %w", err)
			}
			if o.Format == "mdx" {
				external := map[string]string{}
				links := doc.StylesheetURLs()
				if len(links) > 8 {
					fmt.Fprintln(diagnostic, "websrc: warning: only the first 8 external stylesheets are considered")
					links = links[:8]
				}
				for _, link := range links {
					// A local HTML file may reference remote CSS, but never fetch local files
					// from a remotely downloaded page.
					if link.Scheme != "http" && link.Scheme != "https" {
						continue
					}
					cssCtx, cssCancel := context.WithTimeout(ctx, min(o.Timeout, 5*time.Second))
					r, cssErr := fetch(cssCtx, client, link.String(), o.UserAgent, 1<<20)
					cssCancel()
					if cssErr != nil {
						fmt.Fprintln(diagnostic, "websrc: warning: stylesheet unavailable:", link.Redacted(), cssErr)
						continue
					}
					cssReader, decodeErr := charset.NewReader(bytes.NewReader(r.Data), r.ContentType)
					if decodeErr == nil {
						decoded, readErr := readLimited(cssReader, 4<<20)
						if readErr == nil {
							external[link.String()] = string(decoded)
						}
					}
				}
				doc.InferHeadings(doc.CSSInOrder(external))
				fmt.Fprintln(diagnostic, "websrc: -mdx uses static CSS font sizes; JavaScript, layout, @import and conditional CSS are not evaluated")
			}
		}
		output = []byte(doc.Render(o.Format))
	}
	if o.Stdout {
		_, err = out.Write(output)
		return err
	}
	dest := o.Output
	if dest == "" {
		dir := o.Dir
		if dir == "" {
			dir = filepath.Dir(executable)
		}
		dest = filepath.Join(dir, outputName(src.URL, o.Format))
		if o.Format == "raw" && pdfInput {
			dest = strings.TrimSuffix(dest, ".txt") + ".pdf"
		}
	}
	dest, err = filepath.Abs(dest)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}
	flags := os.O_WRONLY | os.O_CREATE | os.O_EXCL
	if o.Overwrite {
		flags = os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	}
	f, err := os.OpenFile(dest, flags, 0644)
	if errors.Is(err, os.ErrExist) {
		return fmt.Errorf("output already exists: %s (use --overwrite or another filename)", dest)
	}
	if err != nil {
		return fmt.Errorf("create output: %w", err)
	}
	_, writeErr := f.Write(output)
	closeErr := f.Close()
	if writeErr != nil {
		return fmt.Errorf("write output: %w", writeErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close output: %w", closeErr)
	}
	fmt.Fprintln(diagnostic, "Saved", dest)
	return nil
}

func isPDF(src resource) bool {
	media, _, _ := mime.ParseMediaType(src.ContentType)
	return bytes.Contains(src.Data[:min(len(src.Data), 1024)], []byte("%PDF-")) || media == "application/pdf" || media == "application/x-pdf" || strings.EqualFold(path.Ext(src.URL.Path), ".pdf")
}

var invalidFilename = regexp.MustCompile(`[<>:"/\\|?*\x00-\x1f]`)

func outputName(u *url.URL, format string) string {
	name := path.Base(u.Path)
	if name == "." || name == "/" || name == "" {
		name = u.Hostname()
	} else if ext := filepath.Ext(name); ext != "" {
		name = strings.TrimSuffix(name, ext)
	}
	name = invalidFilename.ReplaceAllString(name, "_")
	name = strings.Trim(name, ". ")
	if name == "" {
		name = "page"
	}
	upper := strings.ToUpper(strings.SplitN(name, ".", 2)[0])
	if upper == "CON" || upper == "PRN" || upper == "AUX" || upper == "NUL" || regexp.MustCompile(`^(COM|LPT)[1-9]$`).MatchString(upper) {
		name = "_" + name
	}
	runes := []rune(name)
	if len(runes) > 100 {
		name = string(runes[:100])
	}
	ext := ".txt"
	if format == "md" || format == "mdx" {
		ext = ".md"
	}
	if format == "xml" {
		ext = ".xml"
	}
	return name + ext
}
