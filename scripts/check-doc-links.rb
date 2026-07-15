# frozen_string_literal: true

require "cgi"

root = File.expand_path("..", __dir__)
files = Dir.glob(File.join(root, "{README,CHANGELOG,SUPPORT,CONTRIBUTING,SECURITY}.md")) + Dir.glob(File.join(root, "docs/**/*.md"))
failures = []
slug = ->(text) { CGI.unescapeHTML(text).downcase.gsub(/<[^>]+>/, "").gsub(/[^\p{Alnum}\s-]/, "").strip.gsub(/\s+/, "-").gsub(/-+/, "-") }
anchors = {}
files.each do |file|
  seen = Hash.new(0)
  anchors[file] = File.readlines(file).each_with_object([]) do |line, result|
    next unless line =~ /^[#]{1,6}\s+(.+?)\s*#*$/
    base = slug.call(Regexp.last_match(1))
    count = seen[base]
    seen[base] += 1
    result << (count.zero? ? base : "#{base}-#{count}")
  end
end
files.each do |file|
  File.read(file).scan(/\[[^\]]*\]\(([^)]+)\)/).flatten.each do |raw|
    target = raw.split(/\s+["']/, 2).first
    next if target =~ %r{^(https?://|mailto:)}
    path, fragment = target.split("#", 2)
    destination = path.nil? || path.empty? ? file : File.expand_path(CGI.unescape(path), File.dirname(file))
    unless File.file?(destination)
      failures << "#{file.delete_prefix(root + "/")}: missing #{target}"
      next
    end
    if fragment && !anchors.fetch(destination, []).include?(CGI.unescape(fragment))
      failures << "#{file.delete_prefix(root + "/")}: missing anchor #{target}"
    end
  end
end
abort failures.join("\n") unless failures.empty?
puts "documentation links: ok (#{files.length} files)"
