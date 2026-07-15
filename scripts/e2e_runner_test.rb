# frozen_string_literal: true

require "minitest/autorun"

class E2ERunnerTest < Minitest::Test
  ROOT = File.expand_path("..", __dir__)

  def test_runner_covers_every_spec_once_per_project_without_retries
    runner = File.read(File.join(ROOT, "scripts/run-e2e.sh"))
    specs = Dir.glob(File.join(ROOT, "web/e2e/*.spec.ts")).map { |path| File.basename(path) }.sort
    assert_equal 1, runner.scan(/--project=desktop-chromium/).length
    assert_operator runner.scan(/--project=mobile-webkit/).length, :>=, 2
    specs.each { |spec| assert_equal 1, runner.scan(/\b#{Regexp.escape(spec)}\b/).length, spec }
    assert_includes runner, "--max-failures=1"
    assert_includes runner, "--retries=0"
  end
end
