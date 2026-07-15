# frozen_string_literal: true

require "fileutils"
require "minitest/autorun"
require "open3"
require "tmpdir"

class PrivacyCheckTest < Minitest::Test
  CHECKER = File.expand_path("privacy-check.rb", __dir__)

  def test_rejects_private_runtime_ip_precise_default_coordinate_and_unclassified_serial
    Dir.mktmpdir("helio-privacy") do |root|
      FileUtils.mkdir_p(File.join(root, "web/src"))
      File.write(File.join(root, "web/src/app.ts"), "const host='10.8.4.8'; const latitude='-23.55'; const x={loggerSerial:'987654321'}\n")
      _out, err, status = Open3.capture3("ruby", CHECKER, "--root", root)
      refute status.success?
      assert_match(/private|coordinate|serial/i, err)
    end
  end

  def test_rejects_sensitive_content_and_runtime_tools_in_an_image_archive
    Dir.mktmpdir("helio-image-privacy") do |root|
      filesystem = File.join(root, "filesystem")
      FileUtils.mkdir_p(File.join(filesystem, "usr/local/bin"))
      FileUtils.mkdir_p(File.join(filesystem, "data"))
      FileUtils.touch(File.join(filesystem, "000-empty"))
      File.write(File.join(filesystem, "usr/local/bin/node"), "node runtime")
      File.write(File.join(filesystem, "data/capture.trace"), "logger=172.20.4.8 latitude=-23.55")
      archive = File.join(root, "image.tar")
      system("tar", "-cf", archive, "-C", filesystem, ".", exception: true)

      _out, err, status = Open3.capture3("ruby", CHECKER, "--root", filesystem, "--archive", archive)
      refute status.success?
      assert_match(/private IPv4/i, err)
      assert_match(/coordinate/i, err)
      assert_match(/Node runtime/i, err)
      assert_match(/runtime capture/i, err)
    end
  end
end
