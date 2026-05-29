#!/usr/bin/env ruby

require 'fileutils'
require 'tmpdir'

dolphin_bin = File.join(__dir__, "..", "..", "dolphin")
config_files = Dir.glob(File.join(__dir__, "config", "*_config.yaml"))

x = ["5", "3", "7", "10", "100"].sample
x_val = x.to_i
expected = (x_val + x_val).to_s
prompt = "shell calculate #{x}+#{x}"

config_files.each do |config_path|
  puts "=== Testing #{File.basename(config_path)} ==="
  puts "  x=#{x}, expected=#{expected}"

  tmpdir = Dir.mktmpdir
  begin
    output = `DOLPHIN_SESSION_DIR=#{tmpdir} printf "#{prompt}\\nexit\\n" | #{dolphin_bin} -c #{config_path} 2>&1`
    puts "  Output: #{output}"

    if output.include?(expected)
      puts "    PASS"
    else
      puts "    FAIL: Expected #{expected} not found"
      exit 1
    end
  ensure
    FileUtils.rm_rf(tmpdir) if tmpdir
  end
end

puts "\nAll tests passed!"