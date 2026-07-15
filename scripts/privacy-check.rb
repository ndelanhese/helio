# frozen_string_literal: true

require "optparse"
require "open3"
require "rubygems/package"
require "tempfile"

options = { root: File.expand_path("..", __dir__) }
OptionParser.new do |parser|
  parser.on("--root PATH") { |value| options[:root] = value }
  parser.on("--archive PATH") { |value| options[:archive] = value }
  parser.on("--image NAME") { |value| options[:image] = value }
end.parse!
abort "choose only one of --archive or --image" if options[:archive] && options[:image]
root = File.expand_path(options[:root])
failures = []
private_ipv4 = /(?<![A-Za-z0-9.])(?:10(?:\.\d{1,3}){3}|192\.168(?:\.\d{1,3}){2}|172\.(?:1[6-9]|2\d|3[01])(?:\.\d{1,3}){2})(?![A-Za-z0-9.])/
readable = Dir.glob(File.join(root, "**/*"), File::FNM_DOTMATCH).select do |path|
  relative = path.delete_prefix(root + File::SEPARATOR)
  File.file?(path) && File.size(path) < 5 * 1024 * 1024 && !relative.end_with?("privacy_check_test.rb") && !relative.split(File::SEPARATOR).any? { |part| %w[.git .superpowers node_modules playwright-report test-results .worktrees].include?(part) }
end

runtime = readable.select do |path|
  relative = path.delete_prefix(root + File::SEPARATOR)
  go_runtime = (relative.start_with?("cmd/", "internal/") && relative.end_with?(".go") && !relative.end_with?("_test.go"))
  go_runtime || (relative.start_with?("web/src/") && relative !~ /(?:\.test\.|\/test\/)/) || relative.start_with?("internal/webui/dist/") || relative.start_with?("web/public/")
end
runtime.each do |path|
  content = File.binread(path)
  relative = path.delete_prefix(root + File::SEPARATOR)
  failures << "private IPv4 in runtime artifact: #{relative}" if content.match?(private_ipv4)
  failures << "precise default coordinate in runtime artifact: #{relative}" if content.match?(/-23\.55(?:05)?|-46\.63(?:33)?/)
end

allowed_serials = %w[000123 123 456 42424242 123456789 1234567890 4294967295 4294967296]
serial_pattern = /loggerSerial["']?\s*[:=]\s*["'](\d+)["']/
classified = []
readable.each do |path|
  content = File.binread(path).force_encoding("UTF-8").scrub
  content.scan(serial_pattern).flatten.each do |value|
    relative = path.delete_prefix(root + File::SEPARATOR)
    synthetic_path = relative.include?("test") || relative.start_with?("scripts/") || relative.start_with?("web/e2e/")
    if synthetic_path && allowed_serials.include?(value)
      classified << "#{relative}=#{value}"
    else
      failures << "unclassified logger serial value: #{relative}=#{value}"
    end
  end
end

def scan_image_archive(path, failures, private_ipv4)
  entries = 0
  File.open(path, "rb") do |io|
    Gem::Package::TarReader.new(io) do |tar|
      tar.each do |entry|
        relative = entry.full_name.to_s.sub(%r{\A\./}, "")
        next if relative.empty?
        entries += 1
        failures << "Node runtime present in image: #{relative}" if entry.file? && File.basename(relative) == "node"
        if entry.file? && relative.match?(/(?:\A|\/)(?:capture|raw[-_])[^\/]*|\.(?:db|sqlite3?|trace|har)\z/i)
          failures << "runtime capture or database present in image: #{relative}"
        end
        next unless entry.file?
        content = entry.read.to_s
        failures << "private IPv4 in image: #{relative}" if content.match?(private_ipv4)
        failures << "precise default coordinate in image: #{relative}" if content.match?(/-23\.55(?:05)?|-46\.63(?:33)?/)
      end
    end
  end
  entries
rescue StandardError => error
  failures << "cannot inspect image archive #{path}: #{error.message}"
  0
end

image_entries = nil
if options[:archive]
  image_entries = scan_image_archive(File.expand_path(options[:archive]), failures, private_ipv4)
elsif options[:image]
  container = nil
  Tempfile.create(["helio-image", ".tar"]) do |archive|
    archive.close
    output, status = Open3.capture2e("docker", "create", options[:image])
    abort "cannot create image container: #{output}" unless status.success?
    container = output.strip
    output, status = Open3.capture2e("docker", "export", "--output", archive.path, container)
    abort "cannot export image container: #{output}" unless status.success?
    image_entries = scan_image_archive(archive.path, failures, private_ipv4)
  ensure
    Open3.capture2e("docker", "rm", "-f", container) if container && !container.empty?
  end
end

abort failures.join("\n") unless failures.empty?
summary = "privacy structure: ok; #{classified.length} synthetic logger serial values classified"
summary += "; #{image_entries} image entries scanned" if image_entries
puts summary
