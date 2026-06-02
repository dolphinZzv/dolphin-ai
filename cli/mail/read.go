package main

import (
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"io"
	"mime/multipart"
	"mime/quotedprintable"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/spf13/cobra"
)

func newReadCmd(cfg *Config) *cobra.Command {
	var limit int

	cmd := &cobra.Command{
		Use:   "read",
		Short: "Read emails from IMAP inbox",
		RunE: func(cmd *cobra.Command, args []string) error {
			return readMails(cfg, limit)
		},
	}

	cmd.Flags().IntVarP(&limit, "limit", "n", 10, "Number of recent emails to show")

	return cmd
}

func readMails(cfg *Config, limit int) error {
	client, err := imapclient.DialTLS(cfg.IMAPAddr(), &imapclient.Options{
		TLSConfig: &tls.Config{ServerName: cfg.IMAPServer},
	})
	if err != nil {
		return fmt.Errorf("imap dial: %w", err)
	}
	defer client.Close()

	if err := client.Login(cfg.Email, cfg.Password).Wait(); err != nil {
		return fmt.Errorf("imap login: %w", err)
	}
	defer client.Logout().Wait()

	_, err = client.Select("INBOX", nil).Wait()
	if err != nil {
		return fmt.Errorf("imap select: %w", err)
	}

	searchRes, err := client.Search(&imap.SearchCriteria{}, nil).Wait()
	if err != nil {
		return fmt.Errorf("imap search: %w", err)
	}

	all := searchRes.AllSeqNums()
	if len(all) == 0 {
		fmt.Println("inbox is empty")
		return nil
	}

	// Fetch recent N messages (newest first).
	start := 0
	if len(all) > limit {
		start = len(all) - limit
	}
	seqSet := imap.SeqSetNum(all[start:]...)

	fetchRes, err := client.Fetch(seqSet, &imap.FetchOptions{
		Envelope:    true,
		Flags:       true,
		BodySection: []*imap.FetchItemBodySection{{}},
	}).Collect()
	if err != nil {
		return fmt.Errorf("imap fetch: %w", err)
	}

	// Print table.
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintf(tw, "ID\tDate\tFrom\tSubject\tSeen\n")
	fmt.Fprintf(tw, "--\t----\t----\t-------\t----\n")

	// Reverse so newest is on top.
	for i := len(fetchRes) - 1; i >= 0; i-- {
		msg := fetchRes[i]
		if msg.Envelope == nil {
			continue
		}
		from := addrString(msg.Envelope.From)
		date := dateString(msg.Envelope.Date)
		seen := " "
		for _, f := range msg.Flags {
			if f == imap.FlagSeen {
				seen = "✓"
				break
			}
		}
		fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\n", msg.SeqNum, date, from, msg.Envelope.Subject, seen)
	}
	tw.Flush()

	// Show first unseen message body.
	for i := len(fetchRes) - 1; i >= 0; i-- {
		msg := fetchRes[i]
		if msg.Envelope == nil {
			continue
		}
		seen := false
		for _, f := range msg.Flags {
			if f == imap.FlagSeen {
				seen = true
				break
			}
		}
		if seen {
			continue
		}

		var body string
		for _, bs := range msg.BodySection {
			body += string(bs.Bytes)
		}
		body = mimeText(body)
		if body != "" {
			fmt.Printf("\n--- %s ---\n%s\n", msg.Envelope.Subject, body)
		}
		break
	}

	return nil
}

func addrString(addrs []imap.Address) string {
	if len(addrs) == 0 {
		return ""
	}
	a := addrs[0]
	if a.Host != "" {
		return a.Mailbox + "@" + a.Host
	}
	return a.Mailbox
}

func dateString(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format("2006-01-02 15:04")
}

// mimeText extracts readable text from a MIME message body.
func mimeText(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}

	// Multipart.
	if strings.HasPrefix(s, "--") {
		idx := strings.Index(s, "\n")
		if idx <= 2 {
			return s
		}
		boundary := strings.TrimRight(s[2:idx], "\r")
		if boundary == "" {
			return s
		}
		mr := multipart.NewReader(strings.NewReader(s), boundary)
		for {
			part, err := mr.NextPart()
			if err != nil {
				break
			}
			ct := part.Header.Get("Content-Type")
			if !strings.HasPrefix(ct, "text/plain") && !strings.HasPrefix(ct, "text/html") {
				continue
			}
			data, _ := io.ReadAll(part)
			text := string(data)
			text = decodeTransfer(part.Header.Get("Content-Transfer-Encoding"), text)
			return strings.TrimSpace(text)
		}
		return ""
	}

	// Single part: try base64 decode, fall back to raw.
	if decoded, err := base64.StdEncoding.DecodeString(s); err == nil {
		return strings.TrimSpace(string(decoded))
	}
	return s
}

func decodeTransfer(enc, text string) string {
	switch strings.ToLower(enc) {
	case "base64":
		data, err := base64.StdEncoding.DecodeString(text)
		if err != nil {
			return text
		}
		return string(data)
	case "quoted-printable":
		reader := quotedprintable.NewReader(strings.NewReader(text))
		data, err := io.ReadAll(reader)
		if err != nil {
			return text
		}
		return string(data)
	default:
		return text
	}
}
