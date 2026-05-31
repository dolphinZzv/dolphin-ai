#!/usr/bin/env ruby

dolphin_bin = File.join(__dir__, "..", "..", "dolphin")
config_files = Dir.glob(File.join(__dir__, "config", "*_config.yaml"))

x = ["5", "3", "7", "10", "100"].sample
x_val = x.to_i
expected = (x_val + x_val).to_s
prompt = "shell calculate #{x}+#{x}"

config_files.each do |config_path|
  puts "=== Testing #{File.basename(config_path)} ==="
  puts "  x=#{x}, expected=#{expected}"

  script = "spawn #{dolphin_bin} -c #{config_path}\n"
  script += "expect \"Dolphin> \"\n"
  script += "send \"#{prompt}\\r\"\n"
  script += "expect \"Dolphin> \"\n"
  script += "send \"exit\\r\"\n"
  script += "expect eof\n"

  result = `expect -c '#{script}' 2>&1`
  puts "  Output: #{result}"

  if result.include?(expected)
    puts "    PASS"
  else
    puts "    FAIL: Expected #{expected} not found"
    exit 1
  end
end

puts "\nAll tests passed!"