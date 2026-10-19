package auth

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/http/httptest"
	"net/mail"
	"net/textproto"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Each test uses a single local connection; no credentials or messages leave
// the machine. Waiting for completion also detects leaked failure connections.
func localSMTP(t *testing.T, serve func(net.Conn)) (SMTPConfig, <-chan struct{}) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
		serve(conn)
	}()
	t.Cleanup(func() { _ = listener.Close(); awaitMailServer(t, done) })
	_, port, _ := net.SplitHostPort(listener.Addr().String())
	n, _ := strconv.Atoi(port)
	return SMTPConfig{Host: "127.0.0.1", Port: n, Security: "none", SenderEmail: "sender@example.test", SenderName: "命运工坊"}, done
}

func awaitMailServer(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(6 * time.Second):
		t.Error("SMTP test connection did not close")
	}
}

func TestSMTPMailerEncodesUnicodeAndSMTPData(t *testing.T) {
	var raw string
	var commands []string
	cfg, done := localSMTP(t, func(conn net.Conn) {
		reader := textproto.NewReader(bufio.NewReader(conn))
		fmt.Fprint(conn, "220 localhost ESMTP\r\n")
		for {
			line, err := reader.ReadLine()
			if err != nil {
				return
			}
			commands = append(commands, line)
			switch {
			case strings.HasPrefix(line, "EHLO"):
				// No 8BITMIME: the library must encode the UTF-8 body.
				fmt.Fprint(conn, "250 localhost\r\n")
			case line == "DATA":
				fmt.Fprint(conn, "354 send message\r\n")
				data, err := reader.ReadDotBytes()
				if err != nil {
					t.Error(err)
					return
				}
				raw = string(data)
				fmt.Fprint(conn, "250 accepted\r\n")
			case line == "QUIT":
				fmt.Fprint(conn, "221 bye\r\n")
				return
			default:
				fmt.Fprint(conn, "250 ok\r\n")
			}
		}
	})
	want := MailMessage{To: "player@example.test", Subject: "注册验证码 123456", Body: "验证码：123456\n.点开头\n下一行"}
	if err := (smtpMailer{}).Send(context.Background(), cfg, want); err != nil {
		t.Fatal(err)
	}
	awaitMailServer(t, done)
	parsed, err := mail.ReadMessage(strings.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	subject, err := new(mime.WordDecoder).DecodeHeader(parsed.Header.Get("Subject"))
	if err != nil || subject != want.Subject {
		t.Fatalf("subject = %q, %v", subject, err)
	}
	from, err := parsed.Header.AddressList("From")
	if err != nil || len(from) != 1 || from[0].Name != cfg.SenderName || from[0].Address != cfg.SenderEmail {
		t.Fatalf("from = %+v, %v", from, err)
	}
	if parsed.Header.Get("Content-Transfer-Encoding") != "quoted-printable" {
		t.Fatal("body not encoded for SMTP without 8BITMIME")
	}
	body, err := io.ReadAll(quotedprintable.NewReader(parsed.Body))
	if err != nil || strings.TrimSuffix(strings.ReplaceAll(string(body), "\r\n", "\n"), "\n") != want.Body {
		t.Fatalf("body = %q, %v", body, err)
	}
	joined := strings.Join(commands, "\n")
	if !strings.Contains(joined, "MAIL FROM:<sender@example.test>") || !strings.Contains(joined, "RCPT TO:<player@example.test>") {
		t.Fatalf("envelope: %s", joined)
	}
}

func TestSMTPMailerRequiresSTARTTLS(t *testing.T) {
	var commands []string
	cfg, done := localSMTP(t, func(conn net.Conn) {
		fmt.Fprint(conn, "220 localhost ESMTP\r\n")
		reader := bufio.NewScanner(conn)
		for reader.Scan() {
			commands = append(commands, reader.Text())
			fmt.Fprint(conn, "250 localhost\r\n")
		}
	})
	cfg.Security, cfg.Username, cfg.Password = "starttls", "test-user", "test-password"
	err := (smtpMailer{}).Send(context.Background(), cfg, MailMessage{To: "player@example.test", Subject: "test", Body: "test"})
	if err == nil || !strings.Contains(err.Error(), "STARTTLS") {
		t.Fatalf("expected mandatory STARTTLS failure, got %v", err)
	}
	awaitMailServer(t, done)
	for _, cmd := range commands {
		if strings.HasPrefix(cmd, "AUTH") || strings.HasPrefix(cmd, "MAIL") {
			t.Fatalf("sent sensitive command without TLS: %s", cmd)
		}
	}
}

func TestSMTPMailerRejectsUntrustedTLS(t *testing.T) {
	certServer := httptest.NewTLSServer(nil)
	certificates := certServer.TLS.Certificates
	certServer.Close()
	for _, mode := range []string{"tls", "starttls"} {
		t.Run(mode, func(t *testing.T) {
			cfg, done := localSMTP(t, func(conn net.Conn) {
				if mode == "starttls" {
					reader := textproto.NewReader(bufio.NewReader(conn))
					fmt.Fprint(conn, "220 localhost ESMTP\r\n")
					if _, err := reader.ReadLine(); err != nil {
						return
					}
					fmt.Fprint(conn, "250-localhost\r\n250 STARTTLS\r\n")
					line, err := reader.ReadLine()
					if err != nil || line != "STARTTLS" {
						t.Errorf("STARTTLS command = %q, %v", line, err)
						return
					}
					fmt.Fprint(conn, "220 upgrade\r\n")
				}
				secure := tls.Server(conn, &tls.Config{Certificates: certificates, MinVersion: tls.VersionTLS12})
				defer secure.Close()
				if err := secure.Handshake(); err == nil {
					t.Error("client trusted test certificate")
				}
			})
			cfg.Security = mode
			if err := (smtpMailer{}).Send(context.Background(), cfg, MailMessage{To: "player@example.test"}); err == nil {
				t.Fatal("untrusted certificate accepted")
			}
			awaitMailServer(t, done)
		})
	}
}

func TestSMTPMailerCancellationInterruptsEstablishedExchange(t *testing.T) {
	for _, stage := range []string{"greeting", "data", "tls"} {
		t.Run(stage, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			cfg, done := localSMTP(t, func(conn net.Conn) {
				if stage == "data" {
					fmt.Fprint(conn, "220 localhost ESMTP\r\n")
					reader := textproto.NewReader(bufio.NewReader(conn))
					for {
						line, err := reader.ReadLine()
						if err != nil {
							return
						}
						if line == "DATA" {
							break
						}
						fmt.Fprint(conn, "250 localhost\r\n")
					}
				}
				cancel()
				_, _ = io.Copy(io.Discard, conn)
			})
			if stage == "tls" {
				cfg.Security = "tls"
			}
			start := time.Now()
			err := (smtpMailer{}).Send(ctx, cfg, MailMessage{To: "player@example.test", Body: "test"})
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("cancellation = %v", err)
			}
			if time.Since(start) > 2*time.Second {
				t.Error("cancellation did not interrupt SMTP promptly")
			}
			awaitMailServer(t, done)
		})
	}
}

func TestSMTPMailerRejectsHeaderInjectionBeforeDial(t *testing.T) {
	for _, field := range []string{"from", "name", "to", "subject"} {
		t.Run(field, func(t *testing.T) {
			cfg := SMTPConfig{Host: "invalid.invalid", Port: 25, Security: "none", SenderEmail: "sender@example.test"}
			msg := MailMessage{To: "player@example.test", Subject: "test"}
			injection := "\r\nBcc: other@example.test"
			switch field {
			case "from":
				cfg.SenderEmail += injection
			case "name":
				cfg.SenderName += injection
			case "to":
				msg.To += injection
			case "subject":
				msg.Subject += injection
			}
			err := (smtpMailer{}).Send(context.Background(), cfg, msg)
			if err == nil || !strings.Contains(err.Error(), "邮件头") {
				t.Fatalf("header injection = %v", err)
			}
		})
	}
}

func TestSMTPMailerPropagatesRecipientRejection(t *testing.T) {
	var sentData bool
	cfg, done := localSMTP(t, func(conn net.Conn) {
		fmt.Fprint(conn, "220 localhost ESMTP\r\n")
		reader := bufio.NewScanner(conn)
		for reader.Scan() {
			line := reader.Text()
			switch {
			case strings.HasPrefix(line, "RCPT TO:"):
				fmt.Fprint(conn, "550 recipient rejected\r\n")
			case line == "DATA":
				sentData = true
				return
			case line == "QUIT":
				fmt.Fprint(conn, "221 bye\r\n")
				return
			default:
				fmt.Fprint(conn, "250 localhost\r\n")
			}
		}
	})
	err := (smtpMailer{}).Send(context.Background(), cfg, MailMessage{To: "player@example.test", Body: "test"})
	if err == nil || !strings.Contains(err.Error(), "550") {
		t.Fatalf("recipient rejection = %v", err)
	}
	awaitMailServer(t, done)
	if sentData {
		t.Fatal("sent body after recipient rejection")
	}
}

func TestSMTPMailerHonorsDeadlineWhileWaitingForGreeting(t *testing.T) {
	cfg, done := localSMTP(t, func(conn net.Conn) { _, _ = io.Copy(io.Discard, conn) })
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	err := (smtpMailer{}).Send(ctx, cfg, MailMessage{To: "player@example.test"})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("deadline = %v", err)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatal("deadline did not interrupt greeting")
	}
	awaitMailServer(t, done)
}
