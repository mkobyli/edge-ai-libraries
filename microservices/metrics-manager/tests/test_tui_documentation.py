# SPDX-FileCopyrightText: (C) 2026 Intel Corporation
# SPDX-License-Identifier: Apache-2.0

"""Guard screenshot links in the published guide and packaged README."""

import re
from pathlib import Path

import yaml

ROOT = Path(__file__).resolve().parent.parent
GUIDE = ROOT / "docs/user-guide/get-started/bare-metal-package.md"


def test_dashboard_screenshots_exist_and_have_licenses():
    links = re.findall(r"!\[[^\]]*\]\(\./(images/mm-tui-[^)]+\.png)\)", GUIDE.read_text())
    assert set(links) == {
        "images/mm-tui-overview.png",
        "images/mm-tui-charts.png",
        "images/mm-tui-alerts.png",
    }
    for relative in links:
        image = GUIDE.parent / relative
        assert image.read_bytes().startswith(b"\x89PNG\r\n\x1a\n")
        license_text = image.with_suffix(".png.license").read_text()
        assert "SPDX-License-Identifier: Apache-2.0" in license_text
        assert "SPDX-FileCopyrightText:" in license_text


def test_packaged_guide_keeps_relative_image_links():
    spec = yaml.safe_load((ROOT / "native/packaging/nfpm.yaml").read_text())
    contents = {entry.get("src"): entry["dst"] for entry in spec["contents"]}
    readme = Path(contents["dist/doc/README.md"])
    images = Path(contents["dist/doc/images/*"])
    assert images == readme.parent / "images"
    build = (ROOT / "native/build.sh").read_text()
    assert 'cp -R "$COMPONENT_DIR/docs/user-guide/get-started/images" "$STAGE_DIR/doc/images"' in build
