# frozen_string_literal: true

require "json"
require "minitest/autorun"
require "open3"
require "zlib"

class PWAContractTest < Minitest::Test
  ROOT = File.expand_path("..", __dir__)

  def test_manifest_is_installable_without_offline_claims
    manifest = JSON.parse(File.read(File.join(ROOT, "web/public/manifest.webmanifest")))
    assert_equal "Helio", manifest["name"]
    assert_equal "Helio", manifest["short_name"]
    assert_equal "/", manifest["start_url"]
    assert_equal "standalone", manifest["display"]
    assert_equal "#F3F1E8", manifest["theme_color"]
    assert_equal "#F3F1E8", manifest["background_color"]
    assert manifest.fetch("icons").any? { |icon| icon["sizes"] == "512x512" && icon["purpose"].split.include?("maskable") }
    authored = %w[web/src web/public].flat_map { |path| Dir.glob(File.join(ROOT, path, "**/*service*worker*"), File::FNM_CASEFOLD) }
    refute authored.any?
  end

  def test_html_has_manifest_and_light_dark_theme_metadata
    html = File.read(File.join(ROOT, "web/index.html"))
    assert_match(/rel="manifest" href="\/manifest\.webmanifest"/, html)
    assert_match(/name="theme-color" media="\(prefers-color-scheme: light\)" content="#F3F1E8"/, html)
    assert_match(/name="theme-color" media="\(prefers-color-scheme: dark\)" content="#101714"/, html)
  end

  def test_icons_are_rgba_at_declared_dimensions_and_reproducible
    { "icon-192.png" => 192, "icon-512.png" => 512, "maskable-512.png" => 512 }.each do |name, size|
      data = File.binread(File.join(ROOT, "web/public/icons", name))
      assert_equal "\x89PNG\r\n\x1a\n".b, data.byteslice(0, 8), name
      width, height, bit_depth, color_type = data.byteslice(16, 10).unpack("NNCC")
      assert_equal [size, size, 8, 6], [width, height, bit_depth, color_type], name
    end
    stdout, stderr, status = Open3.capture3("ruby", "scripts/generate-icons.rb", "--check", chdir: ROOT)
    assert status.success?, "#{stdout}\n#{stderr}"
  end

  def test_runtime_ui_contains_only_reserved_documentation_ip_examples
    production = %w[
      web/src/features/onboarding/schema.ts
      web/src/features/onboarding/steps.tsx
      web/src/features/settings/settings-model.ts
    ].map { |path| File.read(File.join(ROOT, path)) }.join("\n")
    refute_match(/192\.168\./, production)
    assert_match(/192\.0\.2\.1/, production)
  end
end
