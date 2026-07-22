package auth

import (
	"bufio"
	"context"
	"crypto/tls"
	"fmt"
	"mime"
	"net"
	"net/mail"
	"net/smtp"
	"strings"
	"time"
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

func (smtpMailer) Send(ctx context.Context, cfg SMTPConfig, message MailMessage) error {
	address := net.JoinHostPort(cfg.Host, fmt.Sprintf("%d", cfg.Port))
	dialer := &net.Dialer{Timeout: 15 * time.Second}
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: cfg.Host}

	var client *smtp.Client
	if cfg.Security == "tls" {
		conn, err := tls.DialWithDialer(dialer, "tcp", address, tlsConfig)
		if err != nil {
			return fmt.Errorf("连接 SMTP TLS 失败: %w", err)
		}
		client, err = smtp.NewClient(conn, cfg.Host)
		if err != nil {
			_ = conn.Close()
			return err
		}
	} else {
		conn, err := dialer.DialContext(ctx, "tcp", address)
		if err != nil {
			return fmt.Errorf("连接 SMTP 失败: %w", err)
		}
		client, err = smtp.NewClient(conn, cfg.Host)
		if err != nil {
			_ = conn.Close()
			return err
		}
		if cfg.Security == "starttls" {
			if ok, _ := client.Extension("STARTTLS"); !ok {
				_ = client.Close()
				return fmt.Errorf("SMTP 服务器不支持 STARTTLS")
			}
			if err := client.StartTLS(tlsConfig); err != nil {
				_ = client.Close()
				return fmt.Errorf("SMTP STARTTLS 失败: %w", err)
			}
		}
	}
	defer client.Close()

	if cfg.Username != "" {
		if err := client.Auth(smtp.PlainAuth("", cfg.Username, cfg.Password, cfg.Host)); err != nil {
			return fmt.Errorf("SMTP 认证失败: %w", err)
		}
	}
	if err := client.Mail(cfg.SenderEmail); err != nil {
		return err
	}
	if err := client.Rcpt(message.To); err != nil {
		return err
	}
	w, err := client.Data()
	if err != nil {
		return err
	}
	writer := bufio.NewWriter(w)
	from := (&mail.Address{Name: cfg.SenderName, Address: cfg.SenderEmail}).String()
	headers := []string{
		"From: " + from,
		"To: " + message.To,
		"Subject: " + mime.QEncoding.Encode("UTF-8", message.Subject),
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=UTF-8",
		"Content-Transfer-Encoding: 8bit",
		"",
		strings.ReplaceAll(strings.ReplaceAll(message.Body, "\r\n", "\n"), "\n", "\r\n"),
	}
	if _, err := writer.WriteString(strings.Join(headers, "\r\n")); err != nil {
		_ = w.Close()
		return err
	}
	if err := writer.Flush(); err != nil {
		_ = w.Close()
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return client.Quit()
}
