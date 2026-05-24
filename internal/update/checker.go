package update

import (
	"context"
	"runtime"
	"time"

	"dolphin/internal/metrics"

	"go.uber.org/zap"
)

var (
	metricUpdateAvailable = metrics.NewGauge(
		"dolphin_update_available",
		"1 if a newer version is available, 0 otherwise",
		nil,
	)
	metricUpdateCheckTotal = metrics.NewCounter(
		"dolphin_update_check_total",
		"Total number of update checks performed",
		nil,
	)
	metricUpdateCheckErrors = metrics.NewCounter(
		"dolphin_update_check_errors_total",
		"Total number of update check errors",
		nil,
	)
	metricUpdateInstallSuccess = metrics.NewCounter(
		"dolphin_update_install_success_total",
		"Total number of successful silent updates installed",
		nil,
	)
	metricUpdateInstallFailure = metrics.NewCounter(
		"dolphin_update_install_failure_total",
		"Total number of failed silent update installations",
		nil,
	)
)

// CheckerConfig holds configuration for the background update checker.
type CheckerConfig struct {
	Enabled       bool
	CheckInterval time.Duration
	Channel       string
	AutoInstall   bool
	CheckTimeout  time.Duration // per-check HTTP timeout, 0 = default 30s
}

// StartChecker runs the background update check loop.
// It blocks until ctx is cancelled, then returns nil.
// Errors during individual checks are logged as warnings and do not stop the loop.
func StartChecker(ctx context.Context, cfg CheckerConfig, currentVersion string) error {
	if currentVersion == "dev" {
		zap.S().Infow("update checker: skipped (dev version)")
		<-ctx.Done()
		return nil
	}

	client := NewGitHubClient()

	zap.S().Infow("update checker started",
		"interval", cfg.CheckInterval,
		"channel", cfg.Channel,
		"auto_install", cfg.AutoInstall,
		"current_version", currentVersion,
	)

	performCheck := func() {
		metricUpdateCheckTotal.Inc()

		timeout := cfg.CheckTimeout
		if timeout <= 0 {
			timeout = 30 * time.Second
		}
		checkCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()

		release, err := client.FetchLatest(checkCtx, cfg.Channel)
		if err != nil {
			zap.S().Warnw("update check failed", "error", err)
			metricUpdateCheckErrors.Inc()
			return
		}

		if !IsNewer(currentVersion, release.TagName) {
			zap.S().Debugw("update checker: already at latest", "current", currentVersion, "latest", release.TagName)
			metricUpdateAvailable.Set(0)
			return
		}

		zap.S().Infow("update available", "current", currentVersion, "latest", release.TagName)
		metricUpdateAvailable.Set(1)

		if !cfg.AutoInstall {
			return
		}

		asset, archiveName := FindAsset(release)
		if asset == nil {
			zap.S().Warnw("update: no matching asset for platform",
				"os", runtime.GOOS, "arch", runtime.GOARCH,
				"expected", archiveName,
			)
			return
		}

		zap.S().Infow("update: downloading and installing", "version", release.TagName)
		if err := DownloadAndInstall(asset.BrowserDownloadURL, archiveName); err != nil {
			zap.S().Errorw("update: install failed", "error", err)
			metricUpdateInstallFailure.Inc()
			return
		}

		zap.S().Infow("update: installed successfully, restart to apply", "version", release.TagName)
		metricUpdateInstallSuccess.Inc()
		metricUpdateAvailable.Set(0)
	}

	performCheck()

	ticker := time.NewTicker(cfg.CheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			zap.S().Infow("update checker stopped")
			return nil
		case <-ticker.C:
			performCheck()
		}
	}
}
