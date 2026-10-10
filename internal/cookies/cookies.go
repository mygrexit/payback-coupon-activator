// Package cookies loads browser cookies exported from a desktop browser
// and saves refreshed cookies back to disk.
//
// Supported input formats:
//   - JSON array as exported by the "Cookie-Editor" extension
//     (also the format this package writes)
//   - Netscape cookies.txt (e.g. "Get cookies.txt LOCALLY" extension)
//
// Cookie values are secrets. Nothing in this package logs them.
package cookies

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/publicsuffix"
)

// ErrFormat is returned when the cookie file cannot be parsed. Error
// messages never quote file contents.
var ErrFormat = errors.New("invalid cookie file")

// SameSite values as used by Chrome DevTools Protocol.
const (
	SameSiteStrict = "Strict"
	SameSiteLax    = "Lax"
	SameSiteNone   = "None"
)

// Cookie is a browser cookie. A Domain with a leading dot is a domain
// cookie (sent to subdomains); without a dot it is a host-only cookie.
type Cookie struct {
	Name     string
	Value    string
	Domain   string
	Path     string
	Expires  time.Time // zero means session cookie
	Secure   bool
	HTTPOnly bool
	SameSite string // "", Strict, Lax or None
}

// HostOnly reports whether the cookie is only sent to its exact host.
func (c Cookie) HostOnly() bool { return !strings.HasPrefix(c.Domain, ".") }

// fileCookie is the Cookie-Editor JSON representation.
type fileCookie struct {
	Name           string   `json:"name"`
	Value          string   `json:"value"`
	Domain         string   `json:"domain"`
	Path           string   `json:"path"`
	ExpirationDate *float64 `json:"expirationDate,omitempty"`
	HostOnly       *bool    `json:"hostOnly,omitempty"`
	Secure         bool     `json:"secure"`
	HTTPOnly       bool     `json:"httpOnly"`
	SameSite       *string  `json:"sameSite,omitempty"`
	Session        bool     `json:"session"`
}

// Load reads cookies from path, auto-detecting the format.
func Load(path string) ([]Cookie, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read cookie file: %w", err)
	}
	return Parse(data)
}

// Parse decodes cookies in Cookie-Editor JSON or Netscape format.
func Parse(data []byte) ([]Cookie, error) {
	trimmed := bytes.TrimSpace(bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))) // UTF-8 BOM
	if len(trimmed) == 0 {
		return nil, fmt.Errorf("%w: file is empty", ErrFormat)
	}
	if trimmed[0] == '[' {
		return parseJSON(trimmed)
	}
	return parseNetscape(trimmed)
}

func parseJSON(data []byte) ([]Cookie, error) {
	var raw []fileCookie
	if err := json.Unmarshal(data, &raw); err != nil {
		// Do not wrap the JSON error: it may quote parts of the file.
		return nil, fmt.Errorf("%w: not a valid JSON cookie array", ErrFormat)
	}
	out := make([]Cookie, 0, len(raw))
	for _, fc := range raw {
		if fc.Name == "" || fc.Domain == "" {
			continue
		}
		c := Cookie{
			Name:     fc.Name,
			Value:    fc.Value,
			Domain:   fc.Domain,
			Path:     fc.Path,
			Secure:   fc.Secure,
			HTTPOnly: fc.HTTPOnly,
		}
		if c.Path == "" {
			c.Path = "/"
		}
		if fc.HostOnly != nil && !*fc.HostOnly && !strings.HasPrefix(c.Domain, ".") {
			c.Domain = "." + c.Domain
		}
		if fc.ExpirationDate != nil && !fc.Session && *fc.ExpirationDate > 0 {
			c.Expires = fromUnix(*fc.ExpirationDate)
		}
		if fc.SameSite != nil {
			c.SameSite = normalizeSameSite(*fc.SameSite)
		}
		out = append(out, c)
	}
	return out, nil
}

func parseNetscape(data []byte) ([]Cookie, error) {
	var out []Cookie
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	line := 0
	for sc.Scan() {
		line++
		text := strings.TrimRight(sc.Text(), "\r")
		httpOnly := false
		if rest, ok := strings.CutPrefix(text, "#HttpOnly_"); ok {
			text, httpOnly = rest, true
		}
		if strings.TrimSpace(text) == "" || strings.HasPrefix(text, "#") {
			continue
		}
		f := strings.Split(text, "\t")
		if len(f) != 7 {
			return nil, fmt.Errorf("%w: cookies.txt line %d: expected 7 tab-separated fields", ErrFormat, line)
		}
		exp, err := strconv.ParseInt(f[4], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("%w: cookies.txt line %d: invalid expiry", ErrFormat, line)
		}
		c := Cookie{
			Domain:   f[0],
			Path:     f[2],
			Secure:   strings.EqualFold(f[3], "TRUE"),
			HTTPOnly: httpOnly,
			Name:     f[5],
			Value:    f[6],
		}
		if strings.EqualFold(f[1], "TRUE") && !strings.HasPrefix(c.Domain, ".") {
			c.Domain = "." + c.Domain
		}
		if exp > 0 {
			c.Expires = time.Unix(exp, 0)
		}
		out = append(out, c)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("read cookies.txt: %w", err)
	}
	return out, nil
}

// ForHost keeps cookies that belong to host's site (e.g. all *.payback.de
// cookies for www.payback.de) and drops expired ones.
func ForHost(cs []Cookie, host string, now time.Time) []Cookie {
	site := siteDomain(host)
	var out []Cookie
	for _, c := range cs {
		d := strings.ToLower(strings.TrimPrefix(c.Domain, "."))
		if d != site && !strings.HasSuffix(d, "."+site) {
			continue
		}
		if !c.Expires.IsZero() && !c.Expires.After(now) {
			continue
		}
		out = append(out, c)
	}
	return out
}

// siteDomain returns the registrable domain of host ("www.payback.de" ->
// "payback.de"). IP addresses and hosts without a public suffix (e.g.
// "localhost") are returned as-is.
func siteDomain(host string) string {
	host = strings.ToLower(host)
	if net.ParseIP(host) != nil {
		return host
	}
	if d, err := publicsuffix.EffectiveTLDPlusOne(host); err == nil {
		return d
	}
	return host
}

// Save writes cookies atomically in Cookie-Editor JSON format with mode
// 0600, so a crash never leaves a truncated file behind.
func Save(path string, cs []Cookie) error {
	out := make([]fileCookie, 0, len(cs))
	for _, c := range cs {
		hostOnly := c.HostOnly()
		fc := fileCookie{
			Name:     c.Name,
			Value:    c.Value,
			Domain:   c.Domain,
			Path:     c.Path,
			HostOnly: &hostOnly,
			Secure:   c.Secure,
			HTTPOnly: c.HTTPOnly,
			Session:  c.Expires.IsZero(),
		}
		if !c.Expires.IsZero() {
			v := float64(c.Expires.Unix())
			fc.ExpirationDate = &v
		}
		if c.SameSite != "" {
			v := strings.ToLower(c.SameSite)
			if c.SameSite == SameSiteNone {
				v = "no_restriction"
			}
			fc.SameSite = &v
		}
		out = append(out, fc)
	}
	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return fmt.Errorf("encode cookies: %w", err)
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), ".cookies-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp cookie file: %w", err)
	}
	defer os.Remove(tmp.Name()) // no-op after a successful rename
	// os.CreateTemp creates the file with mode 0600.
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		tmp.Close()
		return fmt.Errorf("write temp cookie file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("sync temp cookie file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp cookie file: %w", err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("replace cookie file: %w", err)
	}
	return nil
}

func normalizeSameSite(s string) string {
	switch strings.ToLower(s) {
	case "strict":
		return SameSiteStrict
	case "lax":
		return SameSiteLax
	case "none", "no_restriction":
		return SameSiteNone
	default: // "unspecified", ""
		return ""
	}
}

func fromUnix(sec float64) time.Time {
	whole, frac := math.Modf(sec)
	return time.Unix(int64(whole), int64(frac*1e9))
}
