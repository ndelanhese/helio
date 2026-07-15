#!/usr/bin/env ruby
# frozen_string_literal: true

require "json"
require "zlib"

ROOT = File.expand_path("..", __dir__)
ICON_DIR = File.join(ROOT, "web/public/icons")
SPEC = JSON.parse(File.read(File.join(ICON_DIR, "helio-mark.json")))

def rgb(value)
  value.delete_prefix("#").scan(/../).map { |pair| pair.to_i(16) }
end

def png(size)
  canvas, ink, sun = %w[canvas ink sun].map { |key| rgb(SPEC.fetch(key)) }
  rows = size.times.map do |y|
    line = +"\x00".b
    size.times do |x|
      nx = (x + 0.5) / size.to_f
      ny = (y + 0.5) / size.to_f
      distance = Math.hypot(nx - 0.5, ny - 0.405)
      color = canvas
      # Eight compact rays make the small icon read as solar without clip-art detail.
      angle = Math.atan2(ny - 0.405, nx - 0.5)
      ray = ((angle / (Math::PI / 4)).round * Math::PI / 4)
      projection = (nx - 0.5) * Math.cos(ray) + (ny - 0.405) * Math.sin(ray)
      perpendicular = ((nx - 0.5) * Math.sin(ray) - (ny - 0.405) * Math.cos(ray)).abs
      color = sun if distance <= 0.145 || (projection.between?(0.19, 0.245) && perpendicular <= 0.018)
      # An open, asymmetric horizon: two rising arms, not a generic enclosing badge.
      left_curve = Math.hypot(nx - 0.50, ny - 0.53)
      color = ink if ny >= 0.53 && left_curve.between?(0.255, 0.315) && nx <= 0.50
      color = ink if ny >= 0.53 && left_curve.between?(0.255, 0.315) && nx >= 0.50
      color = ink if ny.between?(0.69, 0.75) && nx.between?(0.28, 0.72)
      line << color.pack("C3") << "\xFF".b
    end
    line
  end.join
  signature = "\x89PNG\r\n\x1a\n".b
  chunk = lambda do |type, data|
    payload = type.b + data
    [data.bytesize].pack("N") + payload + [Zlib.crc32(payload)].pack("N")
  end
  signature + chunk.call("IHDR", [size, size, 8, 6, 0, 0, 0].pack("NNCCCCC")) +
    chunk.call("IDAT", Zlib::Deflate.deflate(rows, Zlib::BEST_COMPRESSION)) + chunk.call("IEND", "".b)
end

def svg
  <<~SVG
    <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 512 512" role="img" aria-labelledby="title desc">
      <title id="title">Helio</title>
      <desc id="desc">#{SPEC.fetch("description")}</desc>
      <rect width="512" height="512" fill="#{SPEC.fetch("canvas")}"/>
      <g fill="#{SPEC.fetch("sun")}" transform="translate(256 207)">
        <circle r="74"/>
        <g id="ray"><rect x="-9" y="-125" width="18" height="29" rx="9"/></g>
        <use href="#ray" transform="rotate(45)"/><use href="#ray" transform="rotate(90)"/>
        <use href="#ray" transform="rotate(135)"/><use href="#ray" transform="rotate(180)"/>
        <use href="#ray" transform="rotate(225)"/><use href="#ray" transform="rotate(270)"/>
        <use href="#ray" transform="rotate(315)"/>
      </g>
      <path d="M113 384h286M113 384a143 143 0 0 1 286 0" fill="none" stroke="#{SPEC.fetch("ink")}" stroke-width="31"/>
    </svg>
  SVG
end

outputs = {
  "helio-mark.svg" => svg,
  "icon-192.png" => png(192),
  "icon-512.png" => png(512),
  "maskable-512.png" => png(512)
}
if ARGV == ["--check"]
  stale = outputs.reject { |name, data| File.binread(File.join(ICON_DIR, name)) == data rescue false }.keys
  abort "regenerate icons: #{stale.join(', ')}" unless stale.empty?
  puts "icons: reproducible"
else
  Dir.mkdir(ICON_DIR) unless Dir.exist?(ICON_DIR)
  outputs.each { |name, data| File.binwrite(File.join(ICON_DIR, name), data) }
end
