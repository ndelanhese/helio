# frozen_string_literal: true

require "minitest/autorun"
require "yaml"

class WorkflowContractTest < Minitest::Test
  ROOT = File.expand_path("..", __dir__)
  WORKFLOWS = %w[ci.yml codeql.yml release.yml].freeze

  def read(relative)
    path = File.join(ROOT, relative)
    assert File.file?(path), "missing #{relative}"
    File.read(path)
  end

  def workflow(name)
    YAML.safe_load(read(".github/workflows/#{name}"), aliases: false)
  rescue Psych::SyntaxError => e
    flunk(".github/workflows/#{name} is invalid YAML: #{e.message}")
  end

  def trigger(document)
    document.fetch("on") { document.fetch(true) }
  end

  def steps(document, job)
    document.fetch("jobs").fetch(job).fetch("steps")
  end

  def run_commands(document, job)
    steps(document, job).map { |step| step["run"] }.compact.join("\n")
  end

  def test_workflow_yaml_parses
    WORKFLOWS.each do |name|
      document = workflow(name)
      assert_kind_of Hash, document
      assert_kind_of Hash, document["jobs"]
    end
  end

  def test_every_action_is_pinned_to_a_documented_commit
    WORKFLOWS.each do |name|
      read(".github/workflows/#{name}").each_line.with_index(1) do |line, line_number|
        next unless line.match?(/^\s*-?\s*uses:/)

        assert_match(
          %r{uses:\s+[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+(?:/[A-Za-z0-9_.-]+)?@[0-9a-f]{40}\s+#\s+v\d+\.\d+\.\d+\s*$},
          line,
          "#{name}:#{line_number} must pin an action commit and document its exact version"
        )
      end
    end
  end

  def test_ci_has_only_fork_safe_required_jobs
    ci = workflow("ci.yml")
    events = trigger(ci)

    assert_equal "ci", ci["name"]
    assert events.key?("pull_request")
    assert_equal ["main"], events.fetch("push").fetch("branches")
    assert_equal({ "contents" => "read" }, ci["permissions"])
    assert_equal %w[backend container e2e frontend], ci.fetch("jobs").keys.sort

    raw = read(".github/workflows/ci.yml")
    refute_includes raw, "pull_request_target"
    refute_match(/secrets\s*\./, raw)
    refute_match(/permissions:\s*(?:write-all|[^\n]*write)/, raw)
  end

  def test_ci_matches_local_backend_and_frontend_parity
    ci = workflow("ci.yml")
    backend = run_commands(ci, "backend")
    frontend = run_commands(ci, "frontend")

    assert_includes backend, "go test -race ./..."
    assert_includes backend, "go vet ./..."
    assert_includes backend, "ruby scripts/workflow_contract_test.rb"
    assert_includes frontend, "npm ci"
    assert_includes frontend, "npm test -- --run"
    assert_includes frontend, "npm run typecheck"
    assert_includes frontend, "npm run lint"
    assert_includes frontend, "npm run build"
  end

  def test_e2e_artifacts_require_failure_and_a_successful_privacy_scan
    ci = workflow("ci.yml")
    e2e_steps = steps(ci, "e2e")
    commands = run_commands(ci, "e2e")

    assert_includes commands, "playwright install --with-deps chromium webkit"
    assert_includes commands, "make test-e2e"
    scan_index = e2e_steps.index { |step| step["id"] == "privacy-scan" }
    upload_index = e2e_steps.index { |step| step["uses"]&.start_with?("actions/upload-artifact@") }
    refute_nil scan_index, "E2E failures must be scanned before upload"
    refute_nil upload_index, "E2E failure artifacts must be uploaded"
    assert_operator scan_index, :<, upload_index
    assert_equal "failure()", e2e_steps.fetch(scan_index).fetch("if")
    assert_equal "failure() && steps.privacy-scan.outcome == 'success'", e2e_steps.fetch(upload_index).fetch("if")
    scan = e2e_steps.fetch(scan_index).fetch("run")
    assert_match(/password|cookie|csrf|token|serial|192\\\.168\\\./i, scan)
    refute_match(/unzip[^\n]*\|[^\n]*grep/, scan, "pipefail can turn a matching trace into a false-negative via SIGPIPE")
    assert_equal "web/test-results", e2e_steps.fetch(upload_index).fetch("with").fetch("path")
  end

  def test_container_job_builds_and_smoke_tests_the_image
    container = run_commands(workflow("ci.yml"), "container")

    assert_match(/HELIO_IMAGE=.*make smoke/, container)
    assert_match(/^smoke: container$/, read("Makefile"))
  end

  def test_codeql_is_separate_least_privilege_and_covers_go_and_typescript
    codeql = workflow("codeql.yml")
    events = trigger(codeql)

    assert_equal "codeql", codeql["name"]
    assert_equal ["main"], events.fetch("push").fetch("branches")
    assert events.key?("pull_request")
    assert events.key?("schedule")
    assert_equal({ "contents" => "read", "security-events" => "write", "actions" => "read" }, codeql["permissions"])
    assert_equal ["codeql"], codeql.fetch("jobs").keys
    init = steps(codeql, "codeql").find { |step| step["uses"]&.start_with?("github/codeql-action/init@") }
    refute_nil init
    assert_equal "go,javascript-typescript", init.dig("with", "languages")
  end

  def test_release_is_strict_ordered_multiarch_and_keyless
    release = workflow("release.yml")
    events = trigger(release)
    jobs = release.fetch("jobs")
    raw = read(".github/workflows/release.yml")

    assert_equal "release", release["name"]
    assert_equal ["v*"], events.fetch("push").fetch("tags")
    assert_equal({}, release["permissions"])
    assert_equal ["verify"], Array(jobs.dig("publish", "needs"))
    assert_equal ["publish"], Array(jobs.dig("github-release", "needs"))
    assert_equal({ "contents" => "read" }, jobs.dig("verify", "permissions"))
    assert_equal({ "contents" => "read", "packages" => "write", "id-token" => "write" }, jobs.dig("publish", "permissions"))
    assert_equal({ "contents" => "write" }, jobs.dig("github-release", "permissions"))

    verify = run_commands(release, "verify")
    assert_match(/\^v\(0\|\[1-9\]\[0-9\]\*\)\\\.\(0\|\[1-9\]\[0-9\]\*\)\\\.\(0\|\[1-9\]\[0-9\]\*\)\$/, verify)
    assert_includes verify, "go test -race ./..."
    assert_includes verify, "go vet ./..."
    assert_includes verify, "npm ci"
    assert_includes verify, "npm test -- --run"
    %w[typecheck lint build].each { |command| assert_includes verify, "npm run #{command}" }
    assert_includes verify, "make test-e2e"
    assert_match(/HELIO_IMAGE=.*make smoke/, verify)

    assert_includes raw, "ghcr.io/ndelanhese/helio"
    assert_includes raw, "linux/amd64,linux/arm64"
    assert_includes raw, "sbom: true"
    assert_match(/provenance:\s*(?:mode=max|true)/, raw)
    assert_match(/digest:\s*\$\{\{\s*steps\.build\.outputs\.digest\s*\}\}/, raw)
    assert_includes raw, "sigstore/cosign-installer@"
    assert_match(/cosign sign[^\n]*\$\{\{\s*steps\.build\.outputs\.digest\s*\}\}/, raw)
    assert_match(/gh release create[^\n]*--notes-file CHANGELOG\.md/, raw)
    refute_includes raw, "pull_request"
  end

  def test_dependabot_updates_each_ecosystem_weekly_in_bounded_groups
    dependabot = YAML.safe_load(read(".github/dependabot.yml"), aliases: false)
    updates = dependabot.fetch("updates")
    expected_directories = {
      "gomod" => "/",
      "npm" => "/web",
      "docker" => "/",
      "github-actions" => "/"
    }

    assert_equal expected_directories.keys.sort, updates.map { |update| update["package-ecosystem"] }.sort
    updates.each do |update|
      ecosystem = update.fetch("package-ecosystem")
      assert_equal expected_directories.fetch(ecosystem), update["directory"]
      assert_equal "weekly", update.dig("schedule", "interval")
      assert_equal 5, update["open-pull-requests-limit"]
      assert_equal ["*"], update.dig("groups", ecosystem, "patterns")
    end
  end

  def test_local_actionlint_is_digest_pinned
    makefile = read("Makefile")

    assert_includes makefile, "workflow-check:"
    assert_match(%r{rhysd/actionlint:1\.7\.12@sha256:b1934ee5f1c509618f2508e6eb47ee0d3520686341fec936f3b79331f9315667}, makefile)
    refute_match(/actionlint:latest/, makefile)
  end

  def test_contribution_and_security_policies_are_operational
    contributing = read("CONTRIBUTING.md")
    security = read("SECURITY.md")

    %w[backend frontend e2e container codeql].each { |check| assert_includes contributing, "`#{check}`" }
    assert_includes contributing, "go test -race ./..."
    assert_includes contributing, "npm --prefix web ci"
    assert_match(/commit/i, contributing)
    assert_match(/pull request/i, contributing)
    assert_match(/private vulnerability reporting/i, security)
    assert_match(/logger serials|hardware identifiers/i, security)
    assert_match(/acknowledge.*business days/i, security)
    assert_match(/status update.*business days/i, security)
    assert_match(/Supported versions/i, security)
  end

  def test_workflows_do_not_download_and_execute_shell_code
    WORKFLOWS.each do |name|
      raw = read(".github/workflows/#{name}")
      refute_match(/(?:curl|wget)[^\n]*(?:\||bash|sh)/, raw, "#{name} must not download and execute shell code")
    end
  end
end
