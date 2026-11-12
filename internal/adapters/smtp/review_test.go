package smtp

import (
	"context"
	"errors"
	"net"
	"testing"

	"github.com/albievan/cactusinv/internal/ports"
)

// A host that is not a reachable mail server fails as ports.ErrMailUnreachable, so the
// API does not show what answered on that port (the host is user-supplied); a mail
// server's own refusal is not marked (M36).
func TestUnreachableMailServer(t *testing.T) {
	send := func(port int) error {
		m, err := New(Config{Host: "127.0.0.1", Port: port, From: "a@example.com", TLS: TLSNone})
		if err != nil {
			t.Fatal(err)
		}
		return m.Send(context.Background(), ports.Mail{MessageID: "m1", To: []string{"b@example.com"}, Subject: "s", Body: "b"})
	}
	listen := func(greeting string) int {
		ln, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = ln.Close() })
		go func() {
			for {
				c, err := ln.Accept()
				if err != nil {
					return
				}
				_, _ = c.Write([]byte(greeting))
				_ = c.Close()
			}
		}()
		return ln.Addr().(*net.TCPAddr).Port
	}

	closed := listen("")
	ln, _ := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	nothing := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	for name, port := range map[string]int{
		"nothing listening": nothing,
		"a database":        listen("5.5.5-10.11.6-MariaDB\x00garbage\r\n"),
		"hangs up":          closed,
	} {
		if err := send(port); !errors.Is(err, ports.ErrMailUnreachable) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if err := send(listen("554 5.7.1 no service for you\r\n")); err == nil || errors.Is(err, ports.ErrMailUnreachable) {
		t.Errorf("a mail server's refusal: %v", err)
	}
}
