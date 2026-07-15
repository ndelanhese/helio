# frozen_string_literal: true

require "cgi"
require "optparse"

options = { root: File.expand_path("..", __dir__) }
OptionParser.new { |parser| parser.on("--root PATH") { |value| options[:root] = value } }.parse!
root = File.realpath(options[:root])
files = Dir.glob(File.join(root, "**/*.md")).select do |path|
  relative = path.delete_prefix(root + File::SEPARATOR)
  File.file?(path) && !relative.split(File::SEPARATOR).any? { |part| %w[node_modules .git .worktrees playwright-report test-results].include?(part) }
end
failures = []
slug = lambda do |text|
  CGI.unescapeHTML(text).downcase.gsub(/<[^>]+>/, "").gsub(/[^\p{Alnum}\s-]/, "").strip.gsub(/\s+/, "-").gsub(/-+/, "-")
end

def outside_root?(path, root)
  path != root && !path.start_with?(root + File::SEPARATOR)
end

def markdown_lines(path)
  fenced = nil
  File.readlines(path).each_with_object([]) do |line, lines|
    if fenced
      fenced = nil if line =~ /^ {0,3}#{Regexp.escape(fenced[0])}{#{fenced[1]},}\s*$/
      next
    end
    if line =~ /^ {0,3}(`{3,}|~{3,})/
      marker = Regexp.last_match(1)
      fenced = [marker[0], marker.length]
      next
    end
    lines << line
  end
end

anchors = {}
files.each do |file|
  seen = Hash.new(0)
  anchors[file] = markdown_lines(file).each_with_object([]) do |line, result|
    next unless line =~ /^[#]{1,6}\s+(.+?)\s*#*$/
    base = slug.call(Regexp.last_match(1))
    count = seen[base]
    seen[base] += 1
    result << (count.zero? ? base : "#{base}-#{count}")
  end
end

files.each do |file|
  markdown_lines(file).join.scan(/\[[^\]]*\]\(([^)]+)\)/).flatten.each do |raw|
    target = raw.split(/\s+["']/, 2).first
    next if target =~ %r{^(https?://|mailto:)}
    path, fragment = target.split("#", 2)
    decoded_path = CGI.unescape(path.to_s)
    candidate = decoded_path.empty? ? file : File.expand_path(decoded_path, File.dirname(file))
    if outside_root?(candidate, root)
      failures << "#{file.delete_prefix(root + "/")}: target escapes documentation root: #{target}"
      next
    end
    unless File.file?(candidate)
      failures << "#{file.delete_prefix(root + "/")}: missing #{target}"
      next
    end
    destination = File.realpath(candidate)
    if outside_root?(destination, root)
      failures << "#{file.delete_prefix(root + "/")}: target resolves outside documentation root: #{target}"
      next
    end
    if fragment && !anchors.fetch(destination, []).include?(CGI.unescape(fragment))
      failures << "#{file.delete_prefix(root + "/")}: missing anchor #{target}"
    end
  end
end
abort failures.join("\n") unless failures.empty?
puts "documentation links: ok (#{files.length} files)"
