# Copyright (C) 2025-2026 Intel Corporation
# SPDX-License-Identifier: Apache-2.0

"""
Guard tests for the third-party version pins.

``versions.env`` is the single source of truth for every artefact the Metrics
Manager pulls from a third party (Telegraf, qmassa/qmmd, toolchain images).
The Dockerfile carries mirrored ``ARG`` defaults so that a plain
``docker build .`` works without the Makefile.

Two copies of a version number will eventually drift, and the failure mode is
nasty: the container and the bare-metal .deb would ship *different* Telegraf
builds while claiming the same version. These tests turn that drift into a CI
failure. Run ``make sync-versions`` to fix a reported mismatch.
"""

import re
from pathlib import Path

import pytest

REPO_ROOT = Path(__file__).resolve().parent.parent
VERSIONS_ENV = REPO_ROOT / "versions.env"
DOCKERFILE = REPO_ROOT / "Dockerfile"

# Keys in versions.env that must be mirrored by an `ARG <KEY>=<value>` default.
MIRRORED_KEYS = (
    "TELEGRAF_VERSION",
    "TELEGRAF_SHA256",
    "QMASSA_VERSION",
    "QMMD_VERSION",
    "RUST_IMAGE",
)

# versions.env is deliberately restricted to bare `KEY=value` lines so that
# make, sh and Python can all parse it with the same trivial rules.
_ASSIGNMENT = re.compile(r"^(?P<key>[A-Z][A-Z0-9_]*)=(?P<value>\S*)$")


def _parse_versions_env(text: str) -> dict[str, str]:
    parsed: dict[str, str] = {}
    for lineno, raw in enumerate(text.splitlines(), start=1):
        line = raw.strip()
        if not line or line.startswith("#"):
            continue
        match = _ASSIGNMENT.match(line)
        assert match is not None, (
            f"versions.env:{lineno}: expected a bare `KEY=value` line "
            f"(no spaces, no quotes, no shell expansion), got: {raw!r}"
        )
        parsed[match.group("key")] = match.group("value")
    return parsed


@pytest.fixture(scope="module")
def versions() -> dict[str, str]:
    assert VERSIONS_ENV.is_file(), "versions.env is missing"
    return _parse_versions_env(VERSIONS_ENV.read_text(encoding="utf-8"))


@pytest.fixture(scope="module")
def dockerfile_text() -> str:
    return DOCKERFILE.read_text(encoding="utf-8")


@pytest.fixture(scope="module")
def dockerfile_args(dockerfile_text) -> dict[str, str]:
    """Every `ARG KEY=value` default declared in the Dockerfile."""
    pattern = re.compile(r"^ARG\s+([A-Z][A-Z0-9_]*)=(\S+)\s*$", re.MULTILINE)
    return dict(pattern.findall(dockerfile_text))


class TestVersionsEnv:
    """versions.env must be complete and well-formed."""

    def test_declares_every_mirrored_key(self, versions):
        missing = [key for key in MIRRORED_KEYS if key not in versions]
        assert not missing, f"versions.env is missing: {missing}"

    def test_no_value_is_empty(self, versions):
        empty = [key for key, value in versions.items() if not value]
        assert not empty, f"versions.env has empty values for: {empty}"

    def test_telegraf_version_is_a_release_number(self, versions):
        assert re.fullmatch(r"\d+\.\d+\.\d+", versions["TELEGRAF_VERSION"]), (
            "TELEGRAF_VERSION must be a bare release number such as 1.39.3 "
            "(no leading 'v'), because it is interpolated into both the "
            "download URL and the archive's top-level directory name"
        )

    def test_telegraf_digest_is_a_sha256(self, versions):
        assert re.fullmatch(r"[0-9a-f]{64}", versions["TELEGRAF_SHA256"]), (
            "TELEGRAF_SHA256 must be 64 lowercase hex characters. The build "
            "verifies the downloaded archive against it, so a malformed digest "
            "silently weakens supply-chain integrity"
        )


class TestDockerfileMirrorsVersionsEnv:
    """A forgotten `make sync-versions` must fail here, not in production."""

    @pytest.mark.parametrize("key", MIRRORED_KEYS)
    def test_arg_default_matches(self, key, versions, dockerfile_args):
        assert key in dockerfile_args, f"Dockerfile declares no `ARG {key}=<default>`"
        assert dockerfile_args[key] == versions[key], (
            f"{key} drifted: versions.env={versions[key]!r} but "
            f"Dockerfile ARG default={dockerfile_args[key]!r}. "
            f"Run `make sync-versions`."
        )


class TestTelegrafFetchIsVerified:
    """The Telegraf download must never be trusted without a digest check."""

    def test_download_uses_https_only(self, dockerfile_text):
        assert "https://dl.influxdata.com/telegraf/releases" in dockerfile_text
        assert "--proto '=https'" in dockerfile_text, (
            "curl must be restricted to https so a redirect cannot downgrade "
            "the transport to plain http"
        )

    def test_archive_digest_is_checked_before_extraction(self, dockerfile_text):
        assert "sha256sum --check --strict" in dockerfile_text, (
            "the downloaded Telegraf archive must be verified against "
            "TELEGRAF_SHA256 before it is extracted"
        )

    def test_curl_fails_on_http_errors(self, dockerfile_text):
        assert "--fail" in dockerfile_text, (
            "without curl --fail an HTTP error page is written to disk and the "
            "digest check becomes the only thing standing between an outage "
            "and a corrupt build"
        )
