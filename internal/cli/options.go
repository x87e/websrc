/*
              _                                 
__      _____| |__  ___ _ __ ___   _____  _____ 
\ \ /\ / / _ \ '_ \/ __| '__/ __| / _ \ \/ / _ \
 \ V  V /  __/ |_) \__ \ | | (__ |  __/>  <  __/
  \_/\_/ \___|_.__/|___/_|  \___(_)___/_/\_\___|
                                                 
*/

package cli

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type options struct {
	URL, Input, Format, Output, Dir  string
	Password, PasswordEnv            string
	Stdout, Overwrite, Help, Version bool
	Timeout                          time.Duration
	MaxBytes                         int64
	UserAgent                        string
}

func parseOptions(args []string) (options, error) {
	o := options{Format: "raw", Timeout: 30 * time.Second, MaxBytes: 20 << 20, UserAgent: "websrc/1.0"}
	formatSet := false
	setFormat := func(f string) error {
		if formatSet && o.Format != f {
			return errors.New("choose only one output format")
		}
		o.Format, formatSet = f, true
		return nil
	}
	positional := []string{}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			positional = append(positional, args[i+1:]...)
			break
		}
		name, value, hasValue := strings.Cut(arg, "=")
		if !strings.HasPrefix(arg, "-") {
			positional = append(positional, arg)
			continue
		}
		take := func() (string, error) {
			if hasValue {
				return value, nil
			}
			if i+1 >= len(args) {
				return "", fmt.Errorf("%s requires a value", name)
			}
			i++
			return args[i], nil
		}
		var err error
		switch name {
		case "-md", "--md", "-txt", "--txt", "-txtx", "--txtx", "-mdx", "--mdx", "-raw", "--raw", "-xml", "--xml":
			if hasValue {
				return o, fmt.Errorf("%s does not take a value", name)
			}
			err = setFormat(strings.TrimLeft(name, "-"))
		case "--format":
			var f string
			f, err = take()
			if err == nil {
				switch f {
				case "raw", "md", "txt", "txtx", "mdx", "xml":
					err = setFormat(f)
				default:
					err = fmt.Errorf("unknown format %q", f)
				}
			}
		case "-o", "--output", "-d", "--dir", "--input", "--timeout", "--max-bytes", "--user-agent", "--password", "--password-env":
			var v string
			v, err = take()
			if err == nil && v == "" {
				err = fmt.Errorf("%s requires a nonempty value", name)
			}
			if err == nil {
				switch name {
				case "-o", "--output":
					o.Output = expandPath(v)
				case "-d", "--dir":
					o.Dir = expandPath(v)
				case "--input":
					o.Input = expandPath(v)
				case "--timeout":
					o.Timeout, err = time.ParseDuration(v)
				case "--max-bytes":
					o.MaxBytes, err = strconv.ParseInt(v, 10, 64)
				case "--user-agent":
					o.UserAgent = v
				case "--password":
					o.Password = v
				case "--password-env":
					o.PasswordEnv = v
				}
			}
		case "--stdout", "--overwrite", "-h", "--help", "--version":
			if hasValue {
				return o, fmt.Errorf("%s does not take a value", name)
			}
			switch name {
			case "--stdout":
				o.Stdout = true
			case "--overwrite":
				o.Overwrite = true
			case "-h", "--help":
				o.Help = true
			case "--version":
				o.Version = true
			}
		default:
			return o, fmt.Errorf("unknown option %q (use --help)", name)
		}
		if err != nil {
			return o, err
		}
	}
	if o.Help || o.Version {
		return o, nil
	}
	if o.Input == "" && len(positional) > 0 {
		source := expandPath(positional[0])
		if isLocalInput(source) {
			o.Input = source
		} else {
			o.URL = positional[0]
		}
		positional = positional[1:]
	}
	if len(positional) > 1 {
		return o, errors.New("expected one URL and at most one output directory; quote paths containing spaces")
	}
	if len(positional) == 1 {
		if o.Dir != "" {
			return o, errors.New("output directory was specified twice")
		}
		o.Dir = expandPath(positional[0])
	}
	if o.Input == "" && o.URL == "" {
		return o, errors.New("provide an HTTP(S) URL or a local HTML/PDF path (or --input FILE)")
	}
	if o.URL != "" {
		u, err := url.Parse(o.URL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
			return o, errors.New("URL must start with http:// or https:// and include a host")
		}
		if u.User != nil {
			return o, errors.New("URLs containing credentials are not supported")
		}
	}
	if o.Output == "-" {
		o.Stdout, o.Output = true, ""
	}
	if o.Output != "" && o.Dir != "" {
		return o, errors.New("use either --output or --dir")
	}
	if o.Stdout && (o.Output != "" || o.Dir != "") {
		return o, errors.New("--stdout cannot be combined with an output path")
	}
	if o.Timeout <= 0 || o.Timeout > 24*time.Hour {
		return o, errors.New("timeout must be greater than zero and at most 24h")
	}
	if o.MaxBytes <= 0 || o.MaxBytes > 1<<30 {
		return o, errors.New("max-bytes must be between 1 and 1073741824")
	}
	if o.Password != "" && o.PasswordEnv != "" {
		return o, errors.New("choose either --password or --password-env")
	}
	if o.PasswordEnv != "" {
		var ok bool
		o.Password, ok = os.LookupEnv(o.PasswordEnv)
		if !ok {
			return o, errors.New("the PDF password environment variable is not set")
		}
	}
	return o, nil
}

func isLocalInput(source string) bool {
	if strings.Contains(source, "://") {
		return false
	}
	switch strings.ToLower(filepath.Ext(source)) {
	case ".pdf", ".html", ".htm", ".xhtml":
		return true
	}
	info, err := os.Stat(source)
	return err == nil && !info.IsDir()
}

var windowsEnv = regexp.MustCompile(`%([^%]+)%`)

func expandPath(path string) string {
	path = windowsEnv.ReplaceAllStringFunc(path, func(v string) string {
		if s, ok := os.LookupEnv(v[1 : len(v)-1]); ok {
			return s
		}
		return v
	})
	path = os.ExpandEnv(path)
	if path == "~" || strings.HasPrefix(path, "~/") || strings.HasPrefix(path, `~\`) {
		if home, err := os.UserHomeDir(); err == nil {
			path = filepath.Join(home, strings.TrimLeft(path[1:], `/\`))
		}
	}
	return path
}

// splitArgs deliberately treats backslashes as literal so Windows paths survive.
func splitArgs(line string) ([]string, error) {
	var args []string
	var word strings.Builder
	var quote rune
	started := false
	for _, r := range line {
		if quote != 0 {
			if r == quote {
				quote = 0
			} else {
				word.WriteRune(r)
			}
		} else {
			switch r {
			case '\'', '"':
				quote, started = r, true
			case ' ', '\t', '\r', '\n':
				if started {
					args = append(args, word.String())
					word.Reset()
					started = false
				}
			default:
				word.WriteRune(r)
				started = true
			}
		}
	}
	if quote != 0 {
		return nil, errors.New("unclosed quote in arguments")
	}
	if started {
		args = append(args, word.String())
	}
	return args, nil
}

func expandArgs(args []string, base string, depth int, seen map[string]bool) ([]string, error) {
	if depth > 8 {
		return nil, errors.New("argument files nested more than 8 levels")
	}
	var out []string
	for _, arg := range args {
		isFile := strings.HasPrefix(arg, "@")
		// A lone .txt argument is the Windows drag-and-drop workflow.
		if len(args) == 1 && !strings.Contains(arg, "://") && strings.EqualFold(filepath.Ext(arg), ".txt") {
			isFile = true
		}
		if !isFile {
			out = append(out, arg)
			continue
		}
		path := expandPath(strings.TrimPrefix(arg, "@"))
		if !filepath.IsAbs(path) {
			path = filepath.Join(base, path)
		}
		path, err := filepath.Abs(path)
		if err != nil {
			return nil, err
		}
		key := strings.ToLower(path)
		if seen[key] {
			return nil, fmt.Errorf("argument file cycle at %s", path)
		}
		f, err := os.Open(path)
		if err != nil {
			return nil, fmt.Errorf("read argument file: %w", err)
		}
		data, err := readLimited(f, 1<<20)
		f.Close()
		if err != nil {
			return nil, fmt.Errorf("read argument file: %w", err)
		}
		var lines []string
		for _, line := range strings.Split(strings.TrimPrefix(string(data), "\ufeff"), "\n") {
			if !strings.HasPrefix(strings.TrimSpace(line), "#") {
				lines = append(lines, line)
			}
		}
		tokens, err := splitArgs(strings.Join(lines, "\n"))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		seen[key] = true
		expanded, err := expandArgs(tokens, filepath.Dir(path), depth+1, seen)
		delete(seen, key)
		if err != nil {
			return nil, err
		}
		out = append(out, expanded...)
	}
	return out, nil
}
