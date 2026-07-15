# frozen_string_literal: true

require "fileutils"
require "minitest/autorun"
require "open3"
require "tmpdir"
require "zlib"

class IconReproTest < Minitest::Test
  ROOT = File.expand_path("..", __dir__)

  def rewrite(path, mutate: false)
    data = File.binread(path)
    offset = 8
    chunks = []
    idat = +"".b
    while offset < data.bytesize
      length = data.byteslice(offset, 4).unpack1("N")
      type = data.byteslice(offset + 4, 4)
      body = data.byteslice(offset + 8, length)
      idat << body if type == "IDAT"
      chunks << [type, body] unless type == "IDAT"
      offset += 12 + length
    end
    pixels = Zlib::Inflate.inflate(idat)
    pixels.setbyte(8, pixels.getbyte(8) ^ 1) if mutate
    packed = Zlib::Deflate.deflate(pixels, Zlib::BEST_SPEED)
    output = data.byteslice(0, 8)
    chunks.insert(1, ["IDAT", packed])
    chunks.each do |type, body|
      payload = type + body
      output << [body.bytesize].pack("N") << payload << [Zlib.crc32(payload)].pack("N")
    end
    File.binwrite(path, output)
  end

  def check(directory)
    Open3.capture3("ruby", "scripts/generate-icons.rb", "--check-dir", directory, chdir: ROOT)
  end

  def test_recompression_is_accepted_but_pixel_corruption_is_rejected
    Dir.mktmpdir("helio-icons") do |directory|
      FileUtils.cp_r(Dir.glob(File.join(ROOT, "web/public/icons/*")), directory)
      path = File.join(directory, "icon-192.png")
      rewrite(path)
      _out, err, status = check(directory)
      assert status.success?, err
      rewrite(path, mutate: true)
      _out, err, status = check(directory)
      refute status.success?
      assert_match(/pixel|regenerate/i, err)
    end
  end
end
