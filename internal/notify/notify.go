// Package notify sends short messages through any service supported by
// shoutrrr (Pushover, Telegram, ntfy, Gotify, Discord, e-mail, ...).
package notify

import (
	"errors"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/nicholas-fedor/shoutrrr"
	"github.com/nicholas-fedor/shoutrrr/pkg/router"
	"github.com/nicholas-fedor/shoutrrr/pkg/types"
)

const sendTimeout = 30 * time.Second

// Notifier sends messages. The zero value and a Notifier created with an
// empty URL are no-ops.
type Notifier struct {
	rawURL string // secret
	sender *router.ServiceRouter
}

// New creates a Notifier for a shoutrrr service URL. An empty URL
// disables notifications.
func New(rawURL string) (*Notifier, error) {
	if rawURL == "" {
		return &Notifier{}, nil
	}
	n := &Notifier{rawURL: rawURL}
	sender, err := shoutrrr.CreateSenderWithOptions(types.SenderOptions{Timeout: sendTimeout}, rawURL)
	if err != nil {
		return nil, n.redact(err)
	}
	n.sender = sender
	return n, nil
}

// Enabled reports whether a notification service is configured.
func (n *Notifier) Enabled() bool { return n != nil && n.sender != nil }

// Send delivers message. Returned errors never contain the service URL
// or its credentials.
func (n *Notifier) Send(message string) error {
	if !n.Enabled() {
		return nil
	}
	return n.redact(errors.Join(n.sender.Send(message, &types.Params{})...))
}

// redact removes the service URL and its credential-bearing parts from
// err. shoutrrr includes the raw URL in some error messages.
func (n *Notifier) redact(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	for _, s := range secretParts(n.rawURL) {
		msg = strings.ReplaceAll(msg, s, "[REDACTED]")
	}
	return errors.New("notification failed: " + msg)
}

// secretParts returns the raw URL plus every component that could carry
// a token (userinfo, host, path segments, query values), longest first.
func secretParts(raw string) []string {
	parts := []string{raw}
	if u, err := url.Parse(raw); err == nil {
		if u.User != nil {
			parts = append(parts, u.User.String(), u.User.Username())
			if p, ok := u.User.Password(); ok {
				parts = append(parts, p)
			}
		}
		parts = append(parts, u.Host)
		parts = append(parts, strings.Split(u.Path, "/")...)
		for _, vs := range u.Query() {
			parts = append(parts, vs...)
		}
	}
	var out []string
	for _, p := range parts {
		if len(p) >= 4 {
			out = append(out, p)
		}
	}
	sort.Slice(out, func(i, j int) bool { return len(out[i]) > len(out[j]) })
	return out
}
