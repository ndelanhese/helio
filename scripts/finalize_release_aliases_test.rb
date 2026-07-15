# frozen_string_literal: true

require "fileutils"
require "json"
require "minitest/autorun"
require "open3"
require "tmpdir"

class FinalizeReleaseAliasesTest < Minitest::Test
  ROOT = File.expand_path("..", __dir__)
  SCRIPT = File.join(ROOT, "scripts/finalize-release-aliases.sh")
  DIGEST = "sha256:" + ("c" * 64)
  IMAGE = "ghcr.io/ndelanhese/helio"

  def release(tag, draft: false, prerelease: false, published: true)
    {
      tagName: tag,
      isDraft: draft,
      isPrerelease: prerelease,
      publishedAt: published ? "2026-07-15T12:00:00Z" : nil
    }
  end

  def run_finalizer(current:, releases:, fail_docker: false, log_path: nil)
    assert File.executable?(SCRIPT), "missing executable scripts/finalize-release-aliases.sh"
    Dir.mktmpdir("helio-release-aliases") do |directory|
      log_path ||= File.join(directory, "docker.log")
      write_fake(directory, "gh", <<~SH)
        printf '%s\n' "${FAKE_RELEASES}"
      SH
      write_fake(directory, "docker", <<~SH)
        printf '%s\n' "$*" >> "${FAKE_DOCKER_LOG}"
        if [ "${FAKE_DOCKER_FAIL:-false}" = true ]; then
          echo 'alias update failed' >&2
          exit 1
        fi
      SH
      env = {
        "PATH" => "#{directory}:#{ENV.fetch("PATH")}",
        "GITHUB_REPOSITORY" => "ndelanhese/helio",
        "GITHUB_REF_NAME" => current,
        "IMAGE_NAME" => IMAGE,
        "IMMUTABLE_DIGEST" => DIGEST,
        "FAKE_RELEASES" => JSON.generate(releases),
        "FAKE_DOCKER_LOG" => log_path,
        "FAKE_DOCKER_FAIL" => fail_docker.to_s
      }
      stdout, stderr, status = Open3.capture3(env, SCRIPT, unsetenv_others: false)
      log = File.file?(log_path) ? File.read(log_path) : ""
      [stdout, stderr, status, log]
    end
  end

  def write_fake(directory, name, body)
    path = File.join(directory, name)
    File.write(path, "#!/bin/sh\nset -eu\n#{body}")
    FileUtils.chmod(0o755, path)
  end

  def test_newest_release_updates_minor_and_latest_aliases
    _stdout, stderr, status, log = run_finalizer(
      current: "v0.2.0",
      releases: [release("v0.1.2"), release("v0.2.0")]
    )

    assert status.success?, stderr
    assert_includes log, "--tag #{IMAGE}:0.2"
    assert_includes log, "--tag #{IMAGE}:latest"
    assert_includes log, "#{IMAGE}@#{DIGEST}"
  end

  def test_older_global_backfill_updates_only_its_minor_line
    _stdout, stderr, status, log = run_finalizer(
      current: "v0.1.1",
      releases: [release("v0.1.1"), release("v0.2.0")]
    )

    assert status.success?, stderr
    assert_includes log, "--tag #{IMAGE}:0.1"
    refute_includes log, "#{IMAGE}:latest"
  end

  def test_older_line_backfill_does_not_regress_any_alias
    stdout, stderr, status, log = run_finalizer(
      current: "v0.1.1",
      releases: [release("v0.1.1"), release("v0.1.2"), release("v0.2.0")]
    )

    assert status.success?, stderr
    assert_empty log
    assert_includes stdout, "aliases=unchanged"
  end

  def test_equal_rerun_idempotently_ensures_both_aliases
    _stdout, stderr, status, log = run_finalizer(
      current: "v1.0.0",
      releases: [release("v1.0.0")]
    )

    assert status.success?, stderr
    assert_includes log, "--tag #{IMAGE}:1.0"
    assert_includes log, "--tag #{IMAGE}:latest"
  end

  def test_draft_prerelease_unpublished_and_non_semver_releases_are_ignored
    _stdout, stderr, status, log = run_finalizer(
      current: "v1.0.0",
      releases: [
        release("v1.0.0"),
        release("v9.0.0", draft: true),
        release("v8.0.0", prerelease: true),
        release("v7.0.0", published: false),
        release("v99.0.0-beta")
      ]
    )

    assert status.success?, stderr
    assert_includes log, "#{IMAGE}:1.0"
    assert_includes log, "#{IMAGE}:latest"
  end

  def test_semver_order_is_numeric_not_lexical
    _stdout, stderr, status, log = run_finalizer(
      current: "v1.10.0",
      releases: [release("v1.9.9"), release("v1.10.0")]
    )

    assert status.success?, stderr
    assert_includes log, "#{IMAGE}:1.10"
    assert_includes log, "#{IMAGE}:latest"
  end

  def test_partial_alias_failure_after_release_is_resumed_on_rerun
    Dir.mktmpdir("helio-alias-resume") do |directory|
      log_path = File.join(directory, "docker.log")
      releases = [release("v2.0.0")]
      _stdout, _stderr, failed, first_log = run_finalizer(
        current: "v2.0.0", releases: releases, fail_docker: true, log_path: log_path
      )
      refute failed.success?
      assert_equal 1, first_log.lines.length

      _stdout, stderr, resumed, final_log = run_finalizer(
        current: "v2.0.0", releases: releases, log_path: log_path
      )
      assert resumed.success?, stderr
      assert_equal 2, final_log.lines.length
      assert final_log.lines.all? { |line| line.include?("#{IMAGE}:2.0") && line.include?("#{IMAGE}:latest") }
    end
  end

  def test_current_tag_must_be_a_published_stable_release
    _stdout, stderr, status, log = run_finalizer(
      current: "v3.0.0",
      releases: [release("v2.0.0")]
    )

    refute status.success?
    assert_match(/current.*published stable release/i, stderr)
    assert_empty log
  end
end
