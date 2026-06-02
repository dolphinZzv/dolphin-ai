package main

import (
	"fmt"
	"os"
)

// Config holds IMAP/SMTP connection settings.
type Config struct {
	IMAPServer string
	IMAPPort   string
	SMTPServer string
	SMTPPort   string
	Email      string
	Password   string
}

// Load populates config from flags, env vars, or prompts as fallback.
func (c *Config) Load() error {
	if c.IMAPServer == "" {
		c.IMAPServer = os.Getenv("MAIL_IMAP_SERVER")
	}
	if c.SMTPServer == "" {
		c.SMTPServer = os.Getenv("MAIL_SMTP_SERVER")
	}
	if c.SMTPServer == "" {
		c.SMTPServer = c.IMAPServer
	}
	if c.Email == "" {
		c.Email = os.Getenv("MAIL_EMAIL")
	}
	if c.Password == "" {
		c.Password = os.Getenv("MAIL_PASSWORD")
	}

	var missing []string
	if c.IMAPServer == "" {
		missing = append(missing, "imap-server")
	}
	if c.Email == "" {
		missing = append(missing, "email")
	}
	if c.Password == "" {
		missing = append(missing, "password")
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required config: %v (use flags or %s env vars)", missing, "MAIL_*")
	}
	return nil
}

func (c *Config) IMAPAddr() string {
	return c.IMAPServer + ":" + c.IMAPPort
}

func (c *Config) SMTPAddr() string {
	return c.SMTPServer + ":" + c.SMTPPort
}
