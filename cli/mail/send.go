package main

import (
	"crypto/tls"
	"fmt"
	"io"
	"net/smtp"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/yuin/goldmark"
)

var tlsDialer = tls.Dial

func newSendCmd(cfg *Config) *cobra.Command {
	var to, subject, bodyFile string

	cmd := &cobra.Command{
		Use:   "send",
		Short: "Send an email via SMTP",
		RunE: func(cmd *cobra.Command, args []string) error {
			body := strings.Join(args, " ")
			if body == "" && bodyFile != "" {
				data, err := os.ReadFile(bodyFile)
				if err != nil {
					return fmt.Errorf("read body file: %w", err)
				}
				body = string(data)
			}
			if body == "" {
				return fmt.Errorf("body is required (provide as argument or via --file)")
			}
			if to == "" {
				return fmt.Errorf("recipient (--to) is required")
			}

			return sendMail(cfg, to, subject, body)
		},
	}

	cmd.Flags().StringVar(&to, "to", "", "Recipient email address")
	cmd.Flags().StringVar(&subject, "subject", "", "Email subject")
	cmd.Flags().StringVar(&bodyFile, "file", "", "Read body from file")

	return cmd
}

func sendMail(cfg *Config, to, subject, body string) error {
	from := cfg.Email

	// Build MIME message with markdown-to-HTML rendering.
	msg := buildMessage(from, to, subject, body)

	tlsCfg := &tls.Config{ServerName: cfg.SMTPServer}
	conn, err := tlsDialer("tcp", cfg.SMTPAddr(), tlsCfg)
	if err != nil {
		return fmt.Errorf("smtp connect: %w", err)
	}

	client, err := smtp.NewClient(conn, cfg.SMTPServer)
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("smtp client: %w", err)
	}
	defer func() { _ = client.Close() }()

	auth := smtp.PlainAuth("", from, cfg.Password, cfg.SMTPServer)
	if err := client.Auth(auth); err != nil {
		return fmt.Errorf("smtp auth: %w", err)
	}
	if err := client.Mail(from); err != nil {
		return fmt.Errorf("smtp mail from: %w", err)
	}
	if err := client.Rcpt(to); err != nil {
		return fmt.Errorf("smtp rcpt: %w", err)
	}

	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("smtp data: %w", err)
	}
	if _, err := io.WriteString(w, msg); err != nil {
		_ = w.Close()
		return fmt.Errorf("smtp write: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("smtp close: %w", err)
	}

	fmt.Fprintf(os.Stderr, "email sent to %s\n", to)
	return nil
}

// buildMessage constructs a MIME message with markdown body rendered to HTML.
func buildMessage(from, to, subject, body string) string {
	var buf strings.Builder

	// Headers
	buf.WriteString("From: " + from + "\r\n")
	buf.WriteString("To: " + to + "\r\n")
	buf.WriteString("Subject: " + subject + "\r\n")

	// Render markdown to HTML
	htmlBuf := new(strings.Builder)
	if err := goldmark.Convert([]byte(body), htmlBuf); err != nil {
		// fallback to plain text
		buf.WriteString("MIME-Version: 1.0\r\n")
		buf.WriteString("Content-Type: text/plain; charset=\"UTF-8\"\r\n")
		buf.WriteString("\r\n")
		buf.WriteString(body)
		return buf.String()
	}

	// Multipart/alternative
	boundary := "=_mail_cli_boundary"
	buf.WriteString("MIME-Version: 1.0\r\n")
	fmt.Fprintf(&buf, "Content-Type: multipart/alternative; boundary=%q\r\n", boundary)
	buf.WriteString("\r\n")
	buf.WriteString("--" + boundary + "\r\n")
	buf.WriteString("Content-Type: text/plain; charset=\"UTF-8\"\r\n")
	buf.WriteString("\r\n")
	buf.WriteString(body)
	buf.WriteString("\r\n")
	buf.WriteString("--" + boundary + "\r\n")
	buf.WriteString("Content-Type: text/html; charset=\"UTF-8\"\r\n")
	buf.WriteString("\r\n")
	buf.WriteString("<html><body>\n")
	buf.WriteString(htmlBuf.String())
	buf.WriteString("\n</body></html>\r\n")
	buf.WriteString("\r\n")
	buf.WriteString("--" + boundary + "--\r\n")

	return buf.String()
}
