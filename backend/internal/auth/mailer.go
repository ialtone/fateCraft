package auth

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"strings"
	"time"

	mail "github.com/wneessen/go-mail"
)

type SMTPConfig struct {
	Host        string
	Port        int
	Security    string
	Username    string
	Password    string
	SenderEmail string
	SenderName  string
}

type MailMessage struct {
	To      string
	Subject string
	Body    string
}

type Mailer interface {
	Send(context.Context, SMTPConfig, MailMessage) error
}

type smtpMailer struct{}

const mailSendTimeout = 15 * time.Second

func (smtpMailer) Send(ctx context.Context, cfg SMTPConfig, message MailMessage) error {
	// Bound the entire exchange, including greeting, TLS, AUTH, DATA and QUIT.
	ctx, cancel := context.WithTimeout(ctx, mailSendTimeout)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return err
	}
	for _, header := range []string{cfg.SenderEmail, cfg.SenderName, message.To, message.Subject} {
		if strings.ContainsAny(header, "\r\n") {
			return fmt.Errorf("邮件头不能包含换行符")
		}
	}
	msg := mail.NewMsg()
	if err := msg.FromFormat(cfg.SenderName, cfg.SenderEmail); err != nil {
		return fmt.Errorf("发件地址无效: %w", err)
	}
	if err := msg.To(message.To); err != nil {
		return fmt.Errorf("收件地址无效: %w", err)
	}
	msg.Subject(message.Subject)
	msg.SetBodyString(mail.TypeTextPlain, message.Body)

	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: cfg.Host}
	options := []mail.Option{mail.WithPort(cfg.Port), mail.WithTimeout(mailSendTimeout), mail.WithTLSConfig(tlsConfig)}
	switch cfg.Security {
	case "tls":
		options = append(options, mail.WithSSL())
	case "starttls":
		options = append(options, mail.WithTLSPolicy(mail.TLSMandatory))
	case "none":
		options = append(options, mail.WithTLSPolicy(mail.NoTLS))
	default:
		return fmt.Errorf("SMTP 安全模式无效")
	}
	if cfg.Username != "" {
		// Preserve PLAIN authentication's refusal to send credentials over an
		// unencrypted remote connection; never select the NoEnc variant.
		options = append(options, mail.WithSMTPAuth(mail.SMTPAuthPlain), mail.WithUsername(cfg.Username), mail.WithPassword(cfg.Password))
	}

	// go-mail's context controls dialing, while later commands use I/O deadlines.
	// Track the connection so request cancellation also interrupts an established
	// SMTP exchange and failed handshakes always release their socket.
	var connection net.Conn
	var stopCancel func() bool
	defer func() {
		if stopCancel != nil {
			stopCancel()
		}
		if connection != nil {
			_ = connection.Close()
		}
	}()
	options = append(options, mail.WithDialContextFunc(func(dialCtx context.Context, network, address string) (net.Conn, error) {
		dialer := &net.Dialer{Timeout: mailSendTimeout}
		var conn net.Conn
		var err error
		if cfg.Security == "tls" {
			conn, err = (&tls.Dialer{NetDialer: dialer, Config: tlsConfig}).DialContext(dialCtx, network, address)
		} else {
			conn, err = dialer.DialContext(dialCtx, network, address)
		}
		if err != nil {
			return nil, err
		}
		connection = conn
		stopCancel = context.AfterFunc(ctx, func() { _ = conn.Close() })
		return conn, nil
	}))
	client, err := mail.NewClient(cfg.Host, options...)
	if err != nil {
		return fmt.Errorf("SMTP 配置无效: %w", err)
	}
	if err := client.DialAndSendWithContext(ctx, msg); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("SMTP 发送失败: %w", err)
	}
	return nil
}
