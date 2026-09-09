package webhook

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/mail"
	"net/smtp"
	"strings"
	"time"

	"github.com/aislopware/slopscale/hscontrol/types"
)

// Mailer sends an email endpoint's messages. [SMTPMailer] is the one the
// server uses; tests substitute their own.
type Mailer interface {
	Send(ctx context.Context, to []string, subject, body string) error
}

// ErrNoMailer is returned for an email endpoint when no mail server is
// configured.
var ErrNoMailer = errors.New("no mail server is configured")

// ErrMailRejected wraps a permanent rejection by the mail server, which a
// retry will not change.
var ErrMailRejected = errors.New("mail server rejected the message")

// mail renders the event as a message and hands it to the mailer.
func (d *Dispatcher) mail(ctx context.Context, endpoint types.Webhook, event types.WebhookEvent) error {
	if d.mailer == nil {
		return ErrNoMailer
	}

	return d.mailer.Send(ctx, endpoint.Recipients(), mailSubject(event), mailBody(event))
}

// mailSubject is the event's message with the tailnet in front, cut to
// one line.
func mailSubject(event types.WebhookEvent) string {
	subject := event.Message
	if i := strings.IndexByte(subject, '\n'); i >= 0 {
		subject = subject[:i]
	}

	if event.Tailnet != "" {
		subject = "[" + event.Tailnet + "] " + subject
	}

	return subject
}

// mailBody is the message followed by the event's data as JSON, so a
// reader has the same facts a receiver gets.
func mailBody(event types.WebhookEvent) string {
	var b strings.Builder

	b.WriteString(event.Message)
	b.WriteString("\r\n\r\n")
	b.WriteString("Event: " + string(event.Type) + "\r\n")
	b.WriteString("Time: " + event.Timestamp.UTC().Format(time.RFC3339) + "\r\n")

	if event.Tailnet != "" {
		b.WriteString("Tailnet: " + event.Tailnet + "\r\n")
	}

	if event.Data != nil {
		data, err := json.MarshalIndent(event.Data, "", "  ")
		if err == nil {
			b.WriteString("\r\n")
			b.WriteString(strings.ReplaceAll(string(data), "\n", "\r\n"))
			b.WriteString("\r\n")
		}
	}

	return b.String()
}

// ntfyTitle is the notification title for an ntfy topic: the tailnet, or
// the server.
func ntfyTitle(event types.WebhookEvent) string {
	if event.Tailnet != "" {
		return "slopscale " + event.Tailnet
	}

	return "slopscale"
}

// SMTPMailer sends through one SMTP server, per [types.SMTPConfig].
type SMTPMailer struct {
	cfg types.SMTPConfig
}

// NewSMTPMailer returns a mailer for the server, or nil when none is
// configured, so a nil check is the only feature switch.
func NewSMTPMailer(cfg types.SMTPConfig) *SMTPMailer {
	if !cfg.Configured() {
		return nil
	}

	return &SMTPMailer{cfg: cfg}
}

// Send delivers one message. A connection or authentication failure is
// returned as is, so the dispatcher retries it; a server rejecting the
// sender, a recipient or the data is wrapped in [ErrMailRejected].
func (m *SMTPMailer) Send(ctx context.Context, to []string, subject, body string) error {
	client, err := m.dial(ctx)
	if err != nil {
		return err
	}
	defer client.Close()

	err = m.authenticate(client)
	if err != nil {
		return err
	}

	// Stop the exchange when the context ends; a finished send closes
	// the client first, which makes the stop a no-op.
	stop := context.AfterFunc(ctx, func() { _ = client.Close() })
	defer stop()

	return m.submit(client, to, subject, body)
}

func (m *SMTPMailer) dial(ctx context.Context) (*smtp.Client, error) {
	dialer := &net.Dialer{Timeout: deliveryTimeout}

	if m.cfg.Encryption == types.SMTPImplicitTLS {
		tlsDialer := &tls.Dialer{
			NetDialer: dialer,
			Config:    &tls.Config{ServerName: m.cfg.Host, MinVersion: tls.VersionTLS12},
		}

		conn, err := tlsDialer.DialContext(ctx, "tcp", m.cfg.Addr())
		if err != nil {
			return nil, fmt.Errorf("connecting to mail server: %w", err)
		}

		return newClient(conn, m.cfg.Host)
	}

	conn, err := dialer.DialContext(ctx, "tcp", m.cfg.Addr())
	if err != nil {
		return nil, fmt.Errorf("connecting to mail server: %w", err)
	}

	client, err := newClient(conn, m.cfg.Host)
	if err != nil {
		return nil, err
	}

	if m.cfg.Encryption == types.SMTPStartTLS {
		err = client.StartTLS(&tls.Config{ServerName: m.cfg.Host, MinVersion: tls.VersionTLS12})
		if err != nil {
			client.Close()

			return nil, fmt.Errorf("starting TLS with mail server: %w", err)
		}
	}

	return client, nil
}

func newClient(conn net.Conn, host string) (*smtp.Client, error) {
	client, err := smtp.NewClient(conn, host)
	if err != nil {
		conn.Close()

		return nil, fmt.Errorf("greeting mail server: %w", err)
	}

	return client, nil
}

func (m *SMTPMailer) authenticate(client *smtp.Client) error {
	if m.cfg.Username == "" {
		return nil
	}

	// PLAIN refuses an unencrypted connection to a remote host, which is
	// the right refusal; LOGIN is not offered by net/smtp, so a server
	// that only speaks it needs CRAM-MD5 or an unauthenticated relay.
	offered, kinds := client.Extension("AUTH")
	if !offered {
		return fmt.Errorf("%w: server offers no authentication", ErrMailRejected)
	}

	auth := smtp.PlainAuth("", m.cfg.Username, m.cfg.Password, m.cfg.Host)
	if strings.Contains(kinds, "CRAM-MD5") && !strings.Contains(kinds, "PLAIN") {
		auth = smtp.CRAMMD5Auth(m.cfg.Username, m.cfg.Password)
	}

	err := client.Auth(auth)
	if err != nil {
		return fmt.Errorf("authenticating with mail server: %w", err)
	}

	return nil
}

func (m *SMTPMailer) submit(client *smtp.Client, to []string, subject, body string) error {
	from, err := mail.ParseAddress(m.cfg.From)
	if err != nil {
		return fmt.Errorf("%w: sender %q: %w", ErrMailRejected, m.cfg.From, err)
	}

	err = client.Mail(from.Address)
	if err != nil {
		return fmt.Errorf("%w: sender: %w", ErrMailRejected, err)
	}

	for _, rcpt := range to {
		err = client.Rcpt(rcpt)
		if err != nil {
			return fmt.Errorf("%w: recipient %s: %w", ErrMailRejected, rcpt, err)
		}
	}

	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("%w: data: %w", ErrMailRejected, err)
	}

	_, err = w.Write([]byte(message(from, to, subject, body)))
	if err != nil {
		return fmt.Errorf("writing message: %w", err)
	}

	err = w.Close()
	if err != nil {
		return fmt.Errorf("%w: message: %w", ErrMailRejected, err)
	}

	err = client.Quit()
	if err != nil {
		return fmt.Errorf("closing mail session: %w", err)
	}

	return nil
}

// message renders the RFC 5322 message: the headers, a blank line and
// the body. The subject is MIME-encoded so any language survives.
func message(from *mail.Address, to []string, subject, body string) string {
	var b strings.Builder

	b.WriteString("From: " + from.String() + "\r\n")
	b.WriteString("To: " + strings.Join(to, ", ") + "\r\n")
	b.WriteString("Subject: " + mime.QEncoding.Encode("utf-8", subject) + "\r\n")
	b.WriteString("Date: " + time.Now().UTC().Format(time.RFC1123Z) + "\r\n")
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=utf-8\r\n")
	b.WriteString("Content-Transfer-Encoding: 8bit\r\n")
	b.WriteString("X-Mailer: slopscale\r\n")
	b.WriteString("\r\n")
	b.WriteString(body)

	return b.String()
}
