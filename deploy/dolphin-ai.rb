# typed: false
# frozen_string_literal: true
#
# Dolphin AI Agent — Homebrew formula
#
# Installed by goreleaser during release to:
#   https://github.com/dolphinZzv/homebrew-dolphin
#
# Manual install:
#   brew tap dolphinZzv/dolphin
#   brew install dolphin-ai
#
# Or from this local file:
#   brew install --formula deploy/dolphin.rb

class DolphinAi < Formula
  desc "AI agent platform with multi-agent coordination, MCP tool integration, and multi-provider LLM support"
  homepage "https://github.com/dolphinZzv/dolphin"
  license "MIT"

  on_macos do
    on_arm do
      url "https://github.com/dolphinZzv/dolphin/releases/latest/download/dolphin-ai_macOS_arm64.tar.gz"
    end
    on_intel do
      url "https://github.com/dolphinZzv/dolphin/releases/latest/download/dolphin-ai_macOS_x86_64.tar.gz"
    end
  end

  depends_on "git"

  def install
    bin.install "dolphin-ai"
  end

  def caveats
    <<~EOS
      dolphin-ai is installed. Run `dolphin-ai setup` to get started.

      For CDP browser automation, install chromium or google-chrome.
      See https://github.com/dolphinZzv/dolphin for documentation.
    EOS
  end

  test do
    assert_match "dev", shell_output("#{bin}/dolphin-ai --version")
  end
end
