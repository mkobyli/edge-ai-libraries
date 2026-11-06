# Copyright (C) 2026 Intel Corporation
# SPDX-License-Identifier: Apache-2.0

import os
import shutil
import subprocess
import tempfile
import unittest
from pathlib import Path


class TestSetupEnvImageSelection(unittest.TestCase):
    def setUp(self) -> None:
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        component = Path(__file__).resolve().parents[3]
        shutil.copy(component / "setup_env.sh", self.root)
        shutil.copy(component / "Makefile", self.root)
        self.environment = dict(os.environ)
        for name in ("METRICS_MANAGER_IMAGE", "VIPPET_UI_IMAGE", "DOCKER_TAG"):
            self.environment.pop(name, None)

    def run_setup(self) -> str:
        subprocess.run(
            ["bash", "setup_env.sh"],
            cwd=self.root,
            env=self.environment,
            check=True,
            capture_output=True,
            text=True,
        )
        return (self.root / ".env").read_text()

    def test_without_pins_preserves_compose_defaults(self) -> None:
        generated = self.run_setup()
        self.assertIn("DOCKER_TAG=2026.2.0-rc2\n", generated)
        self.assertNotIn("METRICS_MANAGER_IMAGE=", generated)
        self.assertNotIn("VIPPET_UI_IMAGE=", generated)

    def test_image_selection_survives_repeated_generation(self) -> None:
        pins = (
            "METRICS_MANAGER_IMAGE=metrics-manager:npu-test\n"
            "VIPPET_UI_IMAGE=vippet-ui:npu-test\n"
        )
        selection = self.root / ".env.images"
        selection.write_text(pins)
        for _ in range(2):
            generated = self.run_setup()
            self.assertTrue(generated.endswith(pins))
            self.assertEqual(selection.read_text(), pins)
            self.assertEqual(generated.count("METRICS_MANAGER_IMAGE="), 1)
            self.assertEqual(generated.count("VIPPET_UI_IMAGE="), 1)

    def test_removing_pins_restores_defaults(self) -> None:
        selection = self.root / ".env.images"
        selection.write_text("METRICS_MANAGER_IMAGE=metrics-manager:npu-test\n")
        self.run_setup()
        selection.unlink()
        self.assertNotIn("METRICS_MANAGER_IMAGE=", self.run_setup())

    def test_make_run_regenerates_environment_and_uses_persisted_images(self) -> None:
        pins = (
            "METRICS_MANAGER_IMAGE=metrics-manager:npu-test\n"
            "VIPPET_UI_IMAGE=vippet-ui:npu-test\n"
        )
        (self.root / ".env.images").write_text(pins)
        (self.root / ".env").write_text("METRICS_MANAGER_IMAGE=old:image\n")
        binary_dir = self.root / "bin"
        binary_dir.mkdir()
        docker = binary_dir / "docker"
        docker.write_text(
            "#!/bin/bash\n"
            "[[ \"$*\" == *'up -d'* ]] || exit 1\n"
            ". ./.env\n"
            'printf \'%s\\n\' "$METRICS_MANAGER_IMAGE" "$VIPPET_UI_IMAGE"\n'
        )
        docker.chmod(0o755)
        environment = dict(self.environment)
        environment["PATH"] = str(binary_dir) + os.pathsep + environment["PATH"]
        for _ in range(2):
            result = subprocess.run(
                ["make", "--no-print-directory", "-o", "shared/.env-dirs.done", "run"],
                cwd=self.root,
                env=environment,
                check=True,
                capture_output=True,
                text=True,
            )
            self.assertIn(
                "metrics-manager:npu-test\nvippet-ui:npu-test\n", result.stdout
            )
            self.assertFalse((self.root / "shared").exists())
