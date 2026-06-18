package transport

import (
	"context"
	"encoding/base64"
	"os"
	"strings"
	"testing"
	"time"

	. "github.com/smartystreets/goconvey/convey"
	"go.uber.org/zap"

	"dolphin/internal/i18n"
)

func TestNewEmail(t *testing.T) {
	Convey("NewEmail", t, func() {
		Convey("creates email transport with IMAP config", func() {
			e := NewEmail(EmailConfig{
				IMAPServer:   "imap.example.com",
				IMAPPort:     "993",
				SMTPServer:   "smtp.example.com",
				SMTPPort:     "465",
				EmailAddress: "test@example.com",
				Password:     "secret",
			}, nil, "Dolphin")
			So(e, ShouldNotBeNil)
			So(e.ID(), ShouldEqual, "email")
			So(e.sendOnly, ShouldBeFalse)
		})

		Convey("creates send-only email transport with Key", func() {
			e := NewEmail(EmailConfig{
				SMTPServer:   "smtp.example.com",
				SMTPPort:     "465",
				EmailAddress: "test@example.com",
				Key:          "send-key",
			}, nil, "Dolphin")
			So(e, ShouldNotBeNil)
			So(e.sendOnly, ShouldBeTrue)
		})

		Convey("creates send-only email when IMAP not configured", func() {
			e := NewEmail(EmailConfig{
				SMTPServer:   "smtp.example.com",
				SMTPPort:     "465",
				EmailAddress: "test@example.com",
				Password:     "secret",
			}, nil, "Dolphin")
			So(e.sendOnly, ShouldBeTrue)
		})
	})
}

func TestEmailID(t *testing.T) {
	Convey("Email.ID", t, func() {
		e := NewEmail(EmailConfig{}, nil, "")
		So(e.ID(), ShouldEqual, "email")
	})
}

func TestEmailCapability(t *testing.T) {
	Convey("Email.Capability", t, func() {
		e := NewEmail(EmailConfig{}, nil, "")
		c := e.Capability()
		So(c.Interactive, ShouldBeFalse)
		So(c.Streamable, ShouldBeFalse)
		So(c.NestRead, ShouldBeFalse)
	})
}

func TestEmailReadSendOnly(t *testing.T) {
	Convey("Email.Read in send-only mode", t, func() {
		e := NewEmail(EmailConfig{
			SMTPServer:   "smtp.example.com",
			SMTPPort:     "465",
			EmailAddress: "test@example.com",
			Key:          "send-key",
		}, nil, "")
		ctx := context.Background()

		_, err := e.Read(ctx)
		So(err, ShouldEqual, ErrSendOnly)
	})
}

func TestEmailBuildMessage(t *testing.T) {
	Convey("Email.buildMessage", t, func() {
		Convey("includes agent name in From header", func() {
			e := NewEmail(EmailConfig{
				EmailAddress: "bot@example.com",
			}, nil, "MyBot")

			msg := e.buildMessage("user@example.com", "Test Subject", "Hello", "", "")
			So(msg, ShouldContainSubstring, "From: MyBot <bot@example.com>")
			So(msg, ShouldContainSubstring, "To: user@example.com")
			So(msg, ShouldContainSubstring, "Subject: Test Subject")
			So(msg, ShouldContainSubstring, "Hello")
		})

		Convey("uses plain email when no agent name", func() {
			e := NewEmail(EmailConfig{
				EmailAddress: "bot@example.com",
			}, nil, "")

			msg := e.buildMessage("user@example.com", "Test", "Body", "", "")
			So(msg, ShouldContainSubstring, "From: bot@example.com")
		})

		Convey("includes MIME headers", func() {
			e := NewEmail(EmailConfig{
				EmailAddress: "bot@example.com",
			}, nil, "")

			msg := e.buildMessage("to@example.com", "Subj", "Body", "", "")
			So(msg, ShouldContainSubstring, "MIME-Version: 1.0")
			So(msg, ShouldContainSubstring, "Content-Type: text/plain; charset=\"UTF-8\"")
		})

		Convey("includes In-Reply-To and References when messageID provided", func() {
			e := NewEmail(EmailConfig{
				EmailAddress: "bot@example.com",
			}, nil, "Bot")

			msg := e.buildMessage("user@example.com", "Re: Test", "Reply", "msg-123", "")
			So(msg, ShouldContainSubstring, "In-Reply-To: <msg-123>")
			So(msg, ShouldContainSubstring, "References: <msg-123>")
		})

		Convey("quotes original body in text/plain when provided", func() {
			e := NewEmail(EmailConfig{
				EmailAddress: "bot@example.com",
			}, nil, "")

			msg := e.buildMessage("user@example.com", "Re: Test", "Reply", "", "Original\nmessage\nbody")
			So(msg, ShouldContainSubstring, "> Original")
			So(msg, ShouldContainSubstring, "> message")
			So(msg, ShouldContainSubstring, "> body")
		})

		Convey("quotes original body in HTML blockquote when provided", func() {
			e := NewEmail(EmailConfig{
				EmailAddress: "bot@example.com",
			}, nil, "")

			msg := e.buildMessage("user@example.com", "Re: Test", "Reply", "", "Quoted text")
			So(msg, ShouldContainSubstring, "<blockquote")
			So(msg, ShouldContainSubstring, "Quoted text")
			So(msg, ShouldContainSubstring, "</blockquote>")
		})
	})
}

func TestEmailWriteBuffer(t *testing.T) {
	Convey("Email.Write buffer behavior", t, func() {
		e := NewEmail(EmailConfig{
			EmailAddress: "bot@example.com",
		}, nil, "Bot")

		Convey("requires lastFrom to be set", func() {
			// Without lastFrom, Write fails
			err := e.Write(context.Background(), "reply text")
			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "no sender to reply to")
		})
	})
}

func TestEmailFlushNoOp(t *testing.T) {
	Convey("Email.Flush with no pending message", t, func() {
		logger, _ := zap.NewProduction()
		e := NewEmail(EmailConfig{
			EmailAddress: "bot@example.com",
		}, logger, "Bot")

		err := e.Flush()
		So(err, ShouldBeNil)
	})
}

func TestEmailRejectMessage(t *testing.T) {
	Convey("Email.rejectMessage", t, func() {
		Convey("does not panic with empty allowSenders", func() {
			e := NewEmail(EmailConfig{EmailAddress: "bot@example.com"}, nil, "Bot")
			So(func() {
				e.rejectMessage(context.Background(), "user@example.com", "Test", "msg-1")
			}, ShouldNotPanic)
		})

		Convey("does not panic with allowSenders set", func() {
			e := NewEmail(EmailConfig{
				EmailAddress: "bot@example.com",
				AllowSenders: "user@example.com",
			}, nil, "Bot")
			So(func() {
				e.rejectMessage(context.Background(), "user@example.com", "Test", "msg-1")
			}, ShouldNotPanic)
		})

		Convey("does not panic with empty messageID", func() {
			e := NewEmail(EmailConfig{EmailAddress: "bot@example.com"}, nil, "Bot")
			So(func() {
				e.rejectMessage(context.Background(), "user@example.com", "Test", "")
			}, ShouldNotPanic)
		})
	})
}

func TestEmailClose(t *testing.T) {
	Convey("Email.Close", t, func() {
		e := NewEmail(EmailConfig{}, nil, "")
		err := e.Close()
		So(err, ShouldBeNil)

		// Double close should be safe
		err = e.Close()
		So(err, ShouldBeNil)
	})
}

func TestEmailValOr(t *testing.T) {
	Convey("valOr helper", t, func() {
		cfg := map[string]any{
			"key1": "value1",
			"key2": "",
		}

		Convey("returns value when present", func() {
			So(valOr(cfg, "key1", "default"), ShouldEqual, "value1")
		})

		Convey("returns default when value is empty string", func() {
			So(valOr(cfg, "key2", "default"), ShouldEqual, "default")
		})

		Convey("returns default when key missing", func() {
			So(valOr(cfg, "missing", "default"), ShouldEqual, "default")
		})
	})
}

func TestEmailLoggerDefault(t *testing.T) {
	Convey("Email logger default", t, func() {
		e := NewEmail(EmailConfig{}, nil, "")
		So(e.logger, ShouldNotBeNil)
	})
}

func TestEmailWrite_AddsRePrefix(t *testing.T) {
	Convey("Email.Write adds Re: prefix to subject", t, func() {
		e := NewEmail(EmailConfig{EmailAddress: "bot@example.com"}, nil, "Bot")
		// Set lastFrom and lastSubject to simulate receiving a message.
		e.mu.Lock()
		e.lastFrom = "user@example.com"
		e.lastSubject = "Hello there"
		e.mu.Unlock()

		err := e.Write(context.Background(), "reply text")
		So(err, ShouldBeNil)

		// Verify the pending message has the Re: prefix.
		e.mu.Lock()
		So(e.pendingMsg, ShouldContainSubstring, "Subject: Re: Hello there")
		So(e.pendingTo, ShouldEqual, "user@example.com")
		e.mu.Unlock()
	})
}

func TestEmailWrite_NoDoubleRe(t *testing.T) {
	Convey("Email.Write does not double Re:", t, func() {
		e := NewEmail(EmailConfig{EmailAddress: "bot@example.com"}, nil, "Bot")
		e.mu.Lock()
		e.lastFrom = "user@example.com"
		e.lastSubject = "Re: Hello"
		e.mu.Unlock()

		err := e.Write(context.Background(), "reply")
		So(err, ShouldBeNil)

		e.mu.Lock()
		So(e.pendingMsg, ShouldContainSubstring, "Subject: Re: Hello")
		So(strings.Count(e.pendingMsg, "Re:"), ShouldEqual, 1)
		e.mu.Unlock()
	})
}

func TestEmailWrite_EmptySubject(t *testing.T) {
	Convey("Email.Write with empty subject", t, func() {
		e := NewEmail(EmailConfig{EmailAddress: "bot@example.com"}, nil, "Bot")
		e.mu.Lock()
		e.lastFrom = "user@example.com"
		e.lastSubject = ""
		e.mu.Unlock()

		err := e.Write(context.Background(), "reply")
		So(err, ShouldBeNil)

		e.mu.Lock()
		So(e.pendingMsg, ShouldNotContainSubstring, "Subject: Re:")
		So(e.pendingMsg, ShouldContainSubstring, "Subject: \r\n")
		e.mu.Unlock()
	})
}

func TestEmailWrite_FlushRoundTrip(t *testing.T) {
	Convey("Email.Write + Flush round-trip without sending", t, func() {
		e := NewEmail(EmailConfig{EmailAddress: "bot@example.com"}, nil, "Bot")
		e.mu.Lock()
		e.lastFrom = "user@example.com"
		e.lastSubject = "Hello"
		e.mu.Unlock()

		err := e.Write(context.Background(), "reply body")
		So(err, ShouldBeNil)

		e.mu.Lock()
		So(e.pendingMsg, ShouldNotBeBlank)
		e.mu.Unlock()

		// Now clear lastFrom to verify later that pending is preserved.
		e.mu.Lock()
		e.lastFrom = ""
		e.mu.Unlock()

		// Flush should attempt SMTP and fail because we have no real server.
		err = e.Flush()
		So(err, ShouldNotBeNil)

		// After Flush, pending should be cleared.
		e.mu.Lock()
		So(e.pendingMsg, ShouldBeBlank)
		So(e.pendingTo, ShouldBeBlank)
		e.mu.Unlock()
	})
}

// Ensure Email implements IO.
var _ IO = (*Email)(nil)

func TestEmailIMAPIntegration(t *testing.T) {
	imapSrv := os.Getenv("TEST_IMAP")
	smtpSrv := os.Getenv("TEST_SMTP")
	email := os.Getenv("TEST_EMAIL")
	password := os.Getenv("TEST_EMAIL_PASSWORD")
	if imapSrv == "" || smtpSrv == "" || email == "" || password == "" {
		t.Skip("set TEST_IMAP, TEST_SMTP, TEST_EMAIL, TEST_EMAIL_PASSWORD for integration test")
	}

	logger, _ := zap.NewDevelopment()
	e := NewEmail(EmailConfig{
		IMAPServer:   imapSrv,
		IMAPPort:     "993",
		SMTPServer:   smtpSrv,
		SMTPPort:     "465",
		EmailAddress: email,
		Password:     password,
		AllowSenders: "*",
	}, logger, "TestBot")
	defer e.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// 1. Verify IMAP connection and envelope parsing via fetchUnseen.
	content, from, subject, messageID, err := e.fetchUnseen(ctx)
	if err != nil {
		t.Fatalf("IMAP connection or fetch failed: %v", err)
	}

	if from == "" {
		t.Log("No unseen messages — IMAP connectivity verified")
		return
	}

	t.Logf("Unseen message: from=%q subject=%q messageID=%q", from, subject, messageID)
	t.Logf("Content length: %d bytes", len(content))
	t.Logf("Content:\n%s", content)

	// 2. Verify from format — MUST contain @.
	if !strings.Contains(from, "@") {
		t.Errorf("from=%q should be a valid email address containing @", from)
	}

	// 3. The extracted from must pass isSenderAllowed with wildcard "*".
	if !e.isSenderAllowed(from) {
		t.Errorf("isSenderAllowed(%q) should be true with AllowSenders=*", from)
	}

	// 4. Verify exact address matching: isSenderAllowed must match the configured email.
	e2 := NewEmail(EmailConfig{AllowSenders: email}, logger, "TestBot")
	if !e2.isSenderAllowed(from) {
		t.Errorf("isSenderAllowed(%q) should match AllowSenders=%q", from, email)
	}

	t.Log("Envelope parsing verification PASSED")
}

func TestEmailSMTPIntegration(t *testing.T) {
	smtpSrv := os.Getenv("TEST_SMTP")
	email := os.Getenv("TEST_EMAIL")
	password := os.Getenv("TEST_EMAIL_PASSWORD")
	if smtpSrv == "" || email == "" || password == "" {
		t.Skip("set TEST_SMTP, TEST_EMAIL, TEST_EMAIL_PASSWORD for integration test")
	}

	logger, _ := zap.NewDevelopment()
	e := NewEmail(EmailConfig{
		SMTPServer:   smtpSrv,
		SMTPPort:     "465",
		EmailAddress: email,
		Password:     password,
	}, logger, "TestBot")
	defer e.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	msg := e.buildMessage(email, "Test from Dolphin", "Hello, this is a test email from Dolphin integration test.", "", "")
	err := e.sendSMTP(ctx, email, msg)
	if err != nil {
		t.Fatalf("SMTP send failed: %v", err)
	}
	t.Log("SMTP test email sent OK — check inbox for message from", email)
}

// TestEmailBuilder tests the registered email builder.
func TestEmailBuilder(t *testing.T) {
	Convey("Email builder from registry", t, func() {
		// The email transport is registered in init()
		tio, err := Build(context.Background(), "email", map[string]any{
			"key":        "send-key",
			"logger":     func() *zap.Logger { l, _ := zap.NewProduction(); return l }(),
			"agent_name": "Test",
		})
		So(err, ShouldBeNil)
		So(tio, ShouldNotBeNil)
		So(tio.ID(), ShouldEqual, "email")
	})
}

func TestValOrListValue(t *testing.T) {
	Convey("valOr with []any value", t, func() {
		cfg := map[string]any{
			"senders": []any{"alice@example.com", "bob@example.com"},
		}
		So(valOr(cfg, "senders", ""), ShouldEqual, "alice@example.com,bob@example.com")
	})
}

func TestIsSenderAllowed(t *testing.T) {
	Convey("isSenderAllowed", t, func() {
		Convey("empty whitelist denies all", func() {
			e := NewEmail(EmailConfig{}, nil, "")
			So(e.isSenderAllowed("any@example.com"), ShouldBeFalse)
		})

		Convey("exact match is allowed", func() {
			e := NewEmail(EmailConfig{AllowSenders: "alice@example.com"}, nil, "")
			So(e.isSenderAllowed("alice@example.com"), ShouldBeTrue)
		})

		Convey("non-matching sender is denied", func() {
			e := NewEmail(EmailConfig{AllowSenders: "alice@example.com,bob@example.com"}, nil, "")
			So(e.isSenderAllowed("mallory@evil.com"), ShouldBeFalse)
		})

		Convey("wildcard glob matches", func() {
			e := NewEmail(EmailConfig{AllowSenders: "*@example.com"}, nil, "")
			So(e.isSenderAllowed("anyone@example.com"), ShouldBeTrue)
			So(e.isSenderAllowed("outsider@other.com"), ShouldBeFalse)
		})
	})
}

func TestExtractText(t *testing.T) {
	Convey("extractText", t, func() {
		Convey("empty string returns empty", func() {
			So(extractText(""), ShouldEqual, "")
			So(extractText("  "), ShouldEqual, "")
		})

		Convey("non-multipart raw text returns as-is", func() {
			So(extractText("Hello World"), ShouldEqual, "Hello World")
		})

		Convey("non-multipart base64 decodes", func() {
			encoded := base64.StdEncoding.EncodeToString([]byte("decoded text"))
			So(extractText(encoded), ShouldEqual, "decoded text")
		})

		Convey("non-multipart invalid base64 returns raw", func() {
			So(extractText("not-base64!!!"), ShouldEqual, "not-base64!!!")
		})

		Convey("invalid boundary returns raw", func() {
			raw := "--\ncontent"
			So(extractText(raw), ShouldEqual, raw)

			raw2 := "--\r\ncontent"
			So(extractText(raw2), ShouldEqual, raw2)
		})

		Convey("multipart extracts text/plain part", func() {
			mime := "--boundary\r\n" +
				"Content-Type: text/plain\r\n" +
				"\r\n" +
				"Hello World\r\n" +
				"--boundary--\r\n"
			So(extractText(mime), ShouldEqual, "Hello World")
		})

		Convey("multipart decodes base64 part", func() {
			encoded := base64.StdEncoding.EncodeToString([]byte("decoded"))
			mime := "--boundary\r\n" +
				"Content-Type: text/plain\r\n" +
				"Content-Transfer-Encoding: base64\r\n" +
				"\r\n" +
				encoded + "\r\n" +
				"--boundary--\r\n"
			So(extractText(mime), ShouldEqual, "decoded")
		})

		Convey("multipart decodes quoted-printable part", func() {
			// "Hello W=6Frld" is "Hello World" in QP
			mime := "--boundary\r\n" +
				"Content-Type: text/plain\r\n" +
				"Content-Transfer-Encoding: quoted-printable\r\n" +
				"\r\n" +
				"Hello W=6Frld\r\n" +
				"--boundary--\r\n"
			So(extractText(mime), ShouldEqual, "Hello World")
		})

		Convey("multipart skips non-text parts", func() {
			mime := "--boundary\r\n" +
				"Content-Type: application/json\r\n" +
				"\r\n" +
				`{"key":"value"}` + "\r\n" +
				"--boundary--\r\n"
			So(extractText(mime), ShouldEqual, "")
		})

		Convey("multipart with bad base64 data returns raw content", func() {
			mime := "--boundary\r\n" +
				"Content-Type: text/plain\r\n" +
				"Content-Transfer-Encoding: base64\r\n" +
				"\r\n" +
				"not-valid-base64!!!\r\n" +
				"--boundary--\r\n"
			// base64 decode fails, returns raw bytes
			So(extractText(mime), ShouldEqual, "not-valid-base64!!!")
		})
	})
}

func TestEmailRequestPermission(t *testing.T) {
	Convey("Email.RequestPermission", t, func() {
		e := NewEmail(EmailConfig{}, nil, "")
		result, err := e.RequestPermission(context.Background(), "test")
		So(err, ShouldNotBeNil)
		So(result, ShouldEqual, PermissionDenied)
	})
}

func TestEmailGetters(t *testing.T) {
	Convey("Email.Context", t, func() {
		e := NewEmail(EmailConfig{}, nil, "")
		So(e.Context(), ShouldEqual, i18n.T("transport.context_email"))
	})

	Convey("Email.Tools", t, func() {
		e := NewEmail(EmailConfig{}, nil, "")
		So(e.Tools(), ShouldBeNil)
	})

	Convey("Email.Start", t, func() {
		e := NewEmail(EmailConfig{}, nil, "")
		err := e.Start(context.Background())
		So(err, ShouldBeNil)
	})
}

func TestEmailCloseIMAP(t *testing.T) {
	Convey("Email.closeIMAP", t, func() {
		e := NewEmail(EmailConfig{}, nil, "")

		Convey("nil client is safe", func() {
			// closeIMAP should not panic when imapClient is nil
			e.closeIMAP()
		})

		Convey("closes and clears client", func() {
			// Can't easily create a real IMAP client, but verify
			// that closeIMAP handles nil gracefully and sets it to nil.
			e.imapClient = nil
			e.closeIMAP()
			So(e.imapClient, ShouldBeNil)
		})
	})
}
