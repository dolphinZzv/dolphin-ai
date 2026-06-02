package main

import (
	"os"

	"github.com/spf13/cobra"
)

func main() {
	var cfg Config

	rootCmd := &cobra.Command{
		Use:   "mail",
		Short: "Email CLI — send and read emails via IMAP/SMTP",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			return cfg.Load()
		},
	}

	rootCmd.PersistentFlags().StringVar(&cfg.IMAPServer, "imap-server", "", "IMAP server address")
	rootCmd.PersistentFlags().StringVar(&cfg.IMAPPort, "imap-port", "993", "IMAP server port")
	rootCmd.PersistentFlags().StringVar(&cfg.SMTPServer, "smtp-server", "", "SMTP server address")
	rootCmd.PersistentFlags().StringVar(&cfg.SMTPPort, "smtp-port", "465", "SMTP server port")
	rootCmd.PersistentFlags().StringVar(&cfg.Email, "email", "", "Email address")
	rootCmd.PersistentFlags().StringVar(&cfg.Password, "password", "", "Email password")

	rootCmd.AddCommand(newSendCmd(&cfg))
	rootCmd.AddCommand(newReadCmd(&cfg))

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
