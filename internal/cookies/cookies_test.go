package cookies

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const cookieEditorJSON = `[
  {"domain": ".payback.de", "expirationDate": 4102444800.5, "hostOnly": false, "httpOnly": true,
   "name": "session", "path": "/", "sameSite": "lax", "secure": true, "session": false, "value": "abc"},
  {"domain": "www.payback.de", "hostOnly": true, "httpOnly": false,
   "name": "pref", "path": "/", "sameSite": null, "secure": true, "session": true, "value": "1"},
  {"domain": "payback.de", "hostOnly": false, "name": "nodot", "path": "", "sameSite": "no_restriction",
   "secure": true, "session": true, "value": "x"}
]`

const netscapeTXT = "# Netscape HTTP Cookie File\n" +
	"#HttpOnly_.payback.de\tTRUE\t/\tTRUE\t4102444800\tsession\tabc\n" +
	"www.payback.de\tFALSE\t/coupons\tFALSE\t0\tpref\t1\n" +
	"\n" +
	".example.com\tTRUE\t/\tFALSE\t4102444800\tother\tzzz\n"

func TestParseCookieEditorJSON(t *testing.T) {
	cs, err := Parse([]byte(cookieEditorJSON))
	if err != nil {
		t.Fatal(err)
	}
	if len(cs) != 3 {
		t.Fatalf("got %d cookies", len(cs))
	}
	s := cs[0]
	if s.Name != "session" || s.Value != "abc" || s.Domain != ".payback.de" || !s.HTTPOnly || !s.Secure || s.SameSite != SameSiteLax {
		t.Errorf("session cookie = %+v", s)
	}
	if s.Expires.Unix() != 4102444800 {
		t.Errorf("expires = %v", s.Expires)
	}
	if p := cs[1]; !p.HostOnly() || !p.Expires.IsZero() || p.SameSite != "" {
		t.Errorf("pref cookie = %+v", p)
	}
	if n := cs[2]; n.Domain != ".payback.de" || n.Path != "/" || n.SameSite != SameSiteNone {
		t.Errorf("nodot cookie = %+v", n)
	}
}

func TestParseNetscape(t *testing.T) {
	cs, err := Parse([]byte(netscapeTXT))
	if err != nil {
		t.Fatal(err)
	}
	if len(cs) != 3 {
		t.Fatalf("got %d cookies", len(cs))
	}
	if s := cs[0]; s.Name != "session" || !s.HTTPOnly || !s.Secure || s.Domain != ".payback.de" || s.Expires.Unix() != 4102444800 {
		t.Errorf("session cookie = %+v", s)
	}
	if p := cs[1]; !p.HostOnly() || p.Path != "/coupons" || !p.Expires.IsZero() || p.HTTPOnly {
		t.Errorf("pref cookie = %+v", p)
	}
}

func TestParseStripsBOM(t *testing.T) {
	cs, err := Parse(append([]byte("\xef\xbb\xbf"), cookieEditorJSON...))
	if err != nil || len(cs) != 3 {
		t.Fatalf("Parse with BOM: %d cookies, err=%v", len(cs), err)
	}
}

func TestParseErrorsDoNotLeakValues(t *testing.T) {
	for _, in := range []string{"", "[{broken SECRETVALUE", "a\tb\tSECRETVALUE"} {
		_, err := Parse([]byte(in))
		if err == nil {
			t.Errorf("Parse(%q) expected error", in)
			continue
		}
		if !errors.Is(err, ErrFormat) {
			t.Errorf("Parse(%q) error is not ErrFormat: %v", in, err)
		}
		if strings.Contains(err.Error(), "SECRETVALUE") {
			t.Errorf("error leaks cookie content: %v", err)
		}
	}
}

func TestForHost(t *testing.T) {
	now := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	cs := []Cookie{
		{Name: "a", Domain: ".payback.de"},
		{Name: "b", Domain: "www.payback.de"},
		{Name: "c", Domain: "accounts.payback.de"},
		{Name: "d", Domain: ".example.com"},
		{Name: "e", Domain: ".notpayback.de"},
		{Name: "expired", Domain: ".payback.de", Expires: now.Add(-time.Second)},
		{Name: "future", Domain: ".payback.de", Expires: now.Add(time.Hour)},
	}
	var got []string
	for _, c := range ForHost(cs, "www.payback.de", now) {
		got = append(got, c.Name)
	}
	if strings.Join(got, ",") != "a,b,c,future" {
		t.Errorf("ForHost = %v", got)
	}

	ip := ForHost([]Cookie{{Name: "x", Domain: "127.0.0.1"}, {Name: "y", Domain: ".payback.de"}}, "127.0.0.1", now)
	if len(ip) != 1 || ip[0].Name != "x" {
		t.Errorf("ForHost(ip) = %+v", ip)
	}
}

func TestSaveRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cookies.json")
	in := []Cookie{
		{Name: "session", Value: "abc", Domain: ".payback.de", Path: "/", Expires: time.Unix(4102444800, 0), Secure: true, HTTPOnly: true, SameSite: SameSiteNone},
		{Name: "pref", Value: "1", Domain: "www.payback.de", Path: "/"},
	}
	if err := Save(path, in); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := st.Mode().Perm(); perm != 0o600 {
		t.Errorf("perm = %o, want 600", perm)
	}
	out, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 {
		t.Fatalf("got %d cookies", len(out))
	}
	for i := range in {
		a, b := in[i], out[i]
		if a.Name != b.Name || a.Value != b.Value || a.Domain != b.Domain || a.Path != b.Path ||
			!a.Expires.Equal(b.Expires) || a.Secure != b.Secure || a.HTTPOnly != b.HTTPOnly || a.SameSite != b.SameSite {
			t.Errorf("round trip mismatch:\n in  %+v\n out %+v", a, b)
		}
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Errorf("temp files left behind: %v", entries)
	}
}
