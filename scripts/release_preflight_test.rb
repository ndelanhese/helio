# frozen_string_literal: true

require "fileutils"
require "minitest/autorun"
require "open3"
require "tmpdir"

class ReleasePreflightTest < Minitest::Test
  ROOT = File.expand_path("..", __dir__)
  SCRIPT = File.join(ROOT, "scripts/release-preflight.sh")
  DIGEST = "sha256:" + ("a" * 64)
  OTHER_DIGEST = "sha256:" + ("b" * 64)

  def run_preflight(release:, image:)
    assert File.executable?(SCRIPT), "missing executable scripts/release-preflight.sh"
    Dir.mktmpdir("helio-release-preflight") do |directory|
      write_fake(directory, "gh", <<~SH)
        case "${FAKE_RELEASE:-missing}" in
          missing) echo 'gh: release not found (HTTP 404)' >&2; exit 1 ;;
          present) printf '%s\n' "${FAKE_RELEASE_BODY}" ;;
          *) echo 'gh: network failure' >&2; exit 1 ;;
        esac
      SH
      write_fake(directory, "docker", <<~SH)
        case "${FAKE_IMAGE:-missing}" in
          missing) echo 'manifest unknown' >&2; exit 1 ;;
          present) printf '%s\n' "${FAKE_IMAGE_DIGEST}" ;;
          dns) echo 'dial tcp: lookup ghcr.io: host not found' >&2; exit 1 ;;
          *) echo 'registry unavailable' >&2; exit 1 ;;
        esac
      SH
      env = {
        "PATH" => "#{directory}:#{ENV.fetch("PATH")}",
        "GITHUB_REPOSITORY" => "ndelanhese/helio",
        "GITHUB_REF_NAME" => "v1.2.3",
        "IMAGE_NAME" => "ghcr.io/ndelanhese/helio",
        "FAKE_RELEASE" => release.fetch(:state).to_s,
        "FAKE_RELEASE_BODY" => release.fetch(:body, ""),
        "FAKE_IMAGE" => image.fetch(:state).to_s,
        "FAKE_IMAGE_DIGEST" => image.fetch(:digest, "")
      }
      Open3.capture3(env, SCRIPT, unsetenv_others: false)
    end
  end

  def write_fake(directory, name, body)
    path = File.join(directory, name)
    File.write(path, "#!/bin/sh\nset -eu\n#{body}")
    FileUtils.chmod(0o755, path)
  end

  def test_neither_release_nor_image_allows_a_new_release
    stdout, stderr, status = run_preflight(
      release: { state: :missing },
      image: { state: :missing }
    )

    assert status.success?, stderr
    assert_includes stdout, "state=new"
    refute_includes stdout, "digest="
  end

  def test_matching_release_and_image_are_an_idempotent_rerun
    stdout, stderr, status = run_preflight(
      release: { state: :present, body: "Helio-Image-Digest: #{DIGEST}" },
      image: { state: :present, digest: DIGEST }
    )

    assert status.success?, stderr
    assert_includes stdout, "state=existing"
    assert_includes stdout, "digest=#{DIGEST}"
  end

  def test_release_without_image_is_inconsistent
    _stdout, stderr, status = run_preflight(
      release: { state: :present, body: "Helio-Image-Digest: #{DIGEST}" },
      image: { state: :missing }
    )

    refute status.success?
    assert_match(/inconsistent/i, stderr)
  end

  def test_image_without_release_is_inconsistent
    _stdout, stderr, status = run_preflight(
      release: { state: :missing },
      image: { state: :present, digest: DIGEST }
    )

    refute status.success?
    assert_match(/inconsistent/i, stderr)
  end

  def test_existing_release_digest_must_match_registry
    _stdout, stderr, status = run_preflight(
      release: { state: :present, body: "Helio-Image-Digest: #{OTHER_DIGEST}" },
      image: { state: :present, digest: DIGEST }
    )

    refute status.success?
    assert_match(/digest mismatch/i, stderr)
  end

  def test_existing_release_requires_exactly_one_digest_record
    _stdout, stderr, status = run_preflight(
      release: { state: :present, body: "no digest metadata" },
      image: { state: :present, digest: DIGEST }
    )

    refute status.success?
    assert_match(/digest metadata/i, stderr)
  end

  def test_registry_transport_failure_is_not_treated_as_absent
    _stdout, stderr, status = run_preflight(
      release: { state: :missing },
      image: { state: :dns }
    )

    refute status.success?
    assert_match(/registry query failed/i, stderr)
  end
end
