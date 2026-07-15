# frozen_string_literal: true

require "base64"
require "fileutils"
require "json"
require "minitest/autorun"
require "open3"
require "tmpdir"

class ValidateE2EArtifactsTest < Minitest::Test
  ROOT = File.expand_path("..", __dir__)
  VALIDATOR = File.join(ROOT, "scripts/validate-e2e-artifacts.sh")

  def run_validator(directory)
    assert File.executable?(VALIDATOR), "missing executable scripts/validate-e2e-artifacts.sh"
    Open3.capture3(VALIDATOR, directory)
  end

  def create_trace(root, entries, symlink: nil)
    path = File.join(root, "browser-test", "trace.zip")
    FileUtils.mkdir_p(File.dirname(path))
    payload = entries.map { |name, content| [name, Base64.strict_encode64(content)] }
    python = <<~PY
      import base64, json, stat, sys, zipfile
      path, symlink = sys.argv[1:]
      payload = json.loads(base64.b64decode(sys.stdin.buffer.read()))
      with zipfile.ZipFile(path, "w", compression=zipfile.ZIP_DEFLATED) as archive:
          for name, content in payload:
              archive.writestr(name, base64.b64decode(content))
          if symlink:
              info = zipfile.ZipInfo(symlink)
              info.create_system = 3
              info.external_attr = (stat.S_IFLNK | 0o777) << 16
              archive.writestr(info, "trace.trace")
    PY
    encoded = Base64.strict_encode64(JSON.generate(payload))
    if entries.length > 5000
      assert_operator encoded.bytesize, :>, 131_072, "fixture must exceed Linux's common per-argument limit"
    end
    _stdout, stderr, status = Open3.capture3("python3", "-c", python, path, symlink.to_s, stdin_data: encoded)
    assert status.success?, stderr
    path
  end

  def assert_rejected(entries, message, symlink: nil)
    Dir.mktmpdir("helio-e2e-artifacts") do |directory|
      create_trace(directory, entries, symlink: symlink)
      stdout, stderr, status = run_validator(directory)
      refute status.success?, "#{message}: #{stdout}"
      assert_match(/reject|invalid|unsafe|forbidden/i, stderr, message)
      refute_includes stdout, "has_artifacts=true"
    end
  end

  def test_missing_results_directory_is_safe_and_has_no_upload
    Dir.mktmpdir("helio-e2e-artifacts") do |directory|
      missing = File.join(directory, "missing")
      stdout, stderr, status = run_validator(missing)
      assert status.success?, stderr
      assert_includes stdout, "has_artifacts=false"
    end
  end

  def test_clean_playwright_trace_is_accepted
    Dir.mktmpdir("helio-e2e-artifacts") do |directory|
      create_trace(directory, {
        "trace.trace" => "clean test event at http://127.0.0.1 and 192.0.2.1",
        "trace.network" => "synthetic network event",
        "resources/0123456789abcdef" => "safe resource"
      })
      stdout, stderr, status = run_validator(directory)
      assert status.success?, stderr
      assert_includes stdout, "has_artifacts=true"
    end
  end

  def test_known_secret_is_rejected
    assert_rejected({ "trace.trace" => 'request {"password":"real-secret-value"}' }, "password")
  end

  def test_corrupt_zip_is_rejected
    Dir.mktmpdir("helio-e2e-artifacts") do |directory|
      path = File.join(directory, "failed", "trace.zip")
      FileUtils.mkdir_p(File.dirname(path))
      File.binwrite(path, "not a zip")
      _stdout, stderr, status = run_validator(directory)
      refute status.success?
      assert_match(/invalid|corrupt/i, stderr)
    end
  end

  def test_path_traversal_is_rejected
    assert_rejected({ "../trace.trace" => "escape" }, "path traversal")
  end

  def test_symlink_member_is_rejected
    assert_rejected({ "trace.trace" => "safe" }, "symlink", symlink: "resources/link")
  end

  def test_nested_archive_is_rejected
    assert_rejected({ "trace.trace" => "safe", "resources/child.zip" => "PK\x03\x04nested" }, "nested archive")
  end

  def test_database_signature_is_rejected
    assert_rejected({ "trace.trace" => "safe", "resources/abcdef" => "SQLite format 3\x00private rows" }, "database")
  end

  def test_environment_file_reference_is_rejected
    assert_rejected({ "trace.trace" => "captured deployment file .env.production" }, "environment file")
  end

  def test_environment_file_member_name_is_rejected
    assert_rejected({ "trace.trace" => "safe", "resources/capture.env.production" => "safe" }, "environment filename")
  end

  def test_member_count_bound_is_enforced
    entries = { "trace.trace" => "safe" }
    5000.times { |index| entries["resources/#{format('%04d', index)}"] = "" }
    assert_rejected(entries, "member count")
  end

  def test_outer_screenshot_is_rejected
    Dir.mktmpdir("helio-e2e-artifacts") do |directory|
      create_trace(directory, { "trace.trace" => "safe" })
      File.binwrite(File.join(directory, "failure.png"), "image")
      _stdout, stderr, status = run_validator(directory)
      refute status.success?
      assert_match(/outer|forbidden|trace\.zip/i, stderr)
    end
  end

  def test_known_playwright_screenshot_outputs_do_not_block_a_valid_trace
    Dir.mktmpdir("helio-e2e-artifacts") do |directory|
      create_trace(directory, { "trace.trace" => "safe" })
      result = File.join(directory, "browser-test")
      png = "\x89PNG\r\n\x1a\n" + "synthetic pixels"
      %w[actual expected diff].each { |kind| File.binwrite(File.join(result, "history-gap-#{kind}.png"), png) }
      File.write(File.join(result, "error-context.md"), "# Synthetic Playwright context\n")
      stdout, stderr, status = run_validator(directory)
      assert status.success?, stderr
      assert_includes stdout, "has_artifacts=true"
    end
  end

  def test_malformed_known_playwright_screenshot_is_rejected
    Dir.mktmpdir("helio-e2e-artifacts") do |directory|
      create_trace(directory, { "trace.trace" => "safe" })
      File.binwrite(File.join(directory, "browser-test", "history-gap-expected.png"), "not a PNG")
      _stdout, stderr, status = run_validator(directory)
      refute status.success?
      assert_match(/invalid|screenshot|PNG/i, stderr)
    end
  end

  def test_directory_named_trace_zip_is_rejected_even_with_known_playwright_children
    Dir.mktmpdir("helio-e2e-artifacts") do |directory|
      create_trace(directory, { "trace.trace" => "safe" })
      disguised = File.join(directory, "evil", "trace.zip")
      FileUtils.mkdir_p(disguised)
      File.write(File.join(disguised, "error-context.md"), "# disguised directory payload\n")
      _stdout, stderr, status = run_validator(directory)
      refute status.success?
      assert_match(/directory|trace\.zip|reject/i, stderr)
    end
  end

  def test_playwright_last_run_metadata_is_validated_but_not_treated_as_an_artifact
    Dir.mktmpdir("helio-e2e-artifacts") do |directory|
      File.write(File.join(directory, ".last-run.json"), JSON.generate({ status: "failed", failedTests: ["0123456789abcdef-test-id"] }))
      stdout, stderr, status = run_validator(directory)
      assert status.success?, stderr
      assert_includes stdout, "has_artifacts=false"
    end
  end

  def test_malformed_playwright_last_run_metadata_is_rejected
    Dir.mktmpdir("helio-e2e-artifacts") do |directory|
      File.write(File.join(directory, ".last-run.json"), JSON.generate({ status: "failed", failedTests: ["password=not-safe"] }))
      _stdout, stderr, status = run_validator(directory)
      refute status.success?
      assert_match(/metadata|invalid|reject/i, stderr)
    end
  end
end
