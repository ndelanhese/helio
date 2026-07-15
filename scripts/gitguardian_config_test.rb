# frozen_string_literal: true

require "minitest/autorun"
require "yaml"

class GitGuardianConfigTest < Minitest::Test
  ROOT = File.expand_path("..", __dir__)
  SYNTHETIC_FIXTURE_SHA256 = "7791bf656e4a4c51d7e51759812db80c9d5d57b8c77c115495c2a83a9274c587"

  def config
    path = File.join(ROOT, ".gitguardian.yaml")
    assert File.file?(path), "missing narrow GitGuardian fixture policy"
    YAML.safe_load(File.read(path), aliases: false)
  end

  def test_only_the_exact_synthetic_fixture_match_is_ignored
    document = config
    assert_equal 2, document.fetch("version")
    assert_equal false, document.fetch("exit_zero")
    secret = document.fetch("secret")
    refute secret.key?("ignored_paths"), "path exclusions could hide real secrets"
    refute secret.key?("ignored_detectors"), "detector exclusions could hide real secrets"
    assert_equal false, secret.fetch("show_secrets")
    assert_equal false, secret.fetch("ignore_known_secrets")
    assert_equal [
      { "name" => "internal/fakeapp and browser fixture credential", "match" => SYNTHETIC_FIXTURE_SHA256 }
    ], secret.fetch("ignored_matches")
  end

  def test_policy_does_not_ignore_an_unrelated_secret_hash
    matches = config.fetch("secret").fetch("ignored_matches").map { |entry| entry.fetch("match") }
    refute_includes matches, "a" * 64
  end
end
