package notify

import (
	"errors"
	"strings"
	"testing"
)

func TestDisabledIsNoop(t *testing.T) {
	n, err := New("")
	if err != nil {
		t.Fatal(err)
	}
	if n.Enabled() {
		t.Error("expected disabled notifier")
	}
	if err := n.Send("hello"); err != nil {
		t.Errorf("Send on disabled notifier: %v", err)
	}
	var nilN *Notifier
	if nilN.Enabled() || nilN.Send("x") != nil {
		t.Error("nil notifier must be a no-op")
	}
}

func TestInvalidURLErrorIsRedacted(t *testing.T) {
	const raw = "doesnotexist://user:SUPERSECRETTOKEN@host/path"
	_, err := New(raw)
	if err == nil {
		t.Fatal("expected error for unknown service")
	}
	if strings.Contains(err.Error(), "SUPERSECRETTOKEN") || strings.Contains(err.Error(), raw) {
		t.Errorf("error leaks secret: %v", err)
	}
}

func TestRedact(t *testing.T) {
	n := &Notifier{rawURL: "telegram://123456:ABCDEFTOKEN@telegram?chats=987654321"}
	err := n.redact(errors.New(`sending via "telegram://123456:ABCDEFTOKEN@telegram?chats=987654321": token ABCDEFTOKEN bad for chat 987654321`))
	for _, leak := range []string{"ABCDEFTOKEN", "987654321"} {
		if strings.Contains(err.Error(), leak) {
			t.Errorf("redacted error still contains %q: %v", leak, err)
		}
	}
}
