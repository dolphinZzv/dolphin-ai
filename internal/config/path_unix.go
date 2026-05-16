//go:build !windows

package config

func defaultSystemConfigDir() string {
	return "/etc/dolphin"
}
