# frozen_string_literal: true

require "fileutils"
require "minitest/autorun"
require "open3"
require "tmpdir"

class CheckDocLinksTest < Minitest::Test
  CHECKER = File.expand_path("check-doc-links.rb", __dir__)

  def run_checker(files, extra: {})
    Dir.mktmpdir("helio-doc-links") do |root|
      files.each do |name, body|
        path = File.join(root, name)
        FileUtils.mkdir_p(File.dirname(path))
        File.write(path, body)
      end
      outside = File.join(File.dirname(root), "existing-host-file.md")
      File.write(outside, "# Host\n")
      begin
        return Open3.capture3("ruby", CHECKER, "--root", root, *extra.fetch(:args, []))
      ensure
        FileUtils.rm_f(outside)
      end
    end
  end

  def test_rejects_existing_file_outside_root
    _out, err, status = run_checker({ "README.md" => "[escape](../existing-host-file.md)\n" })
    refute status.success?
    assert_match(/outside|escape|root/i, err)
  end

  def test_rejects_percent_decoded_traversal
    _out, err, status = run_checker({ "README.md" => "[escape](%2e%2e/existing-host-file.md)\n" })
    refute status.success?
    assert_match(/outside|escape|root/i, err)
  end

  def test_fenced_heading_cannot_satisfy_fragment_and_fenced_link_is_ignored
    _out, err, status = run_checker({
      "README.md" => "[missing](guide.md#fake)\n~~~~ruby\n[ignored](missing.md)\n~~~~\n",
      "guide.md" => "```md\n# Fake\n```\n# Real\n"
    })
    refute status.success?
    assert_match(/anchor/i, err)
    refute_match(/missing\.md/, err)
  end

  def test_valid_duplicate_anchors_use_github_suffixes
    out, err, status = run_checker({ "README.md" => "[second](guide.md#same-1)\n", "guide.md" => "# Same\n# Same\n" })
    assert status.success?, err
    assert_match(/links: ok/i, out)
  end
end
