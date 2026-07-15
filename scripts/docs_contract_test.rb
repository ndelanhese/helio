# frozen_string_literal: true

require "minitest/autorun"

class DocsContractTest < Minitest::Test
  ROOT = File.expand_path("..", __dir__)

  def read(path)
    File.read(File.join(ROOT, path))
  end

  def test_required_operator_contracts_are_documented
    install = read("docs/install.md")
    operations = read("docs/operations.md")
    hardware = read("docs/hardware-testing.md")
    api = read("docs/api.md")
    assert_match(/Docker Desktop.*macOS/im, install)
    assert_match(/Linux.*Raspberry Pi/im, install)
    assert_includes install, "docker compose up -d"
    assert_includes install, "HELIO_BIND_IP"
    assert_match(/never.*public internet/i, install)
    assert_match(/UID.*65532/i, operations)
    assert_match(/retention.*730/i, operations)
    assert_match(/health\/ready.*database/im, operations)
    assert_match(/stop.*restore/im, operations)
    assert_match(/HELIO_HARDWARE_TEST=1/, hardware)
    assert_match(/read-only/i, hardware)
    assert_match(/192\.0\.2\.1/, api)
    refute_match(/192\.168\./, install + operations + hardware + api)
    assert_match(/no write endpoints/i, api)
    assert_match(/X-CSRF-Token/, api)
    assert_match(/text\/event-stream/, api)
    assert_match(/timestamp,power_w,energy_today_wh,status/, api)
  end

  def test_readme_is_truthful_before_first_tag
    readme = read("README.md")
    refute_match(/design phase/i, readme)
    assert_match(/release candidate|source/i, readme)
    assert_match(/after.*release|future release/i, readme)
  end

  def test_smoke_honors_the_release_gate_image_variable
    makefile = read("Makefile")
    assert_includes makefile, "IMAGE ?= $(if $(HELIO_IMAGE),$(HELIO_IMAGE),helio:local)"
  end
end
