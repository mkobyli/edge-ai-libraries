# SPDX-FileCopyrightText: (C) 2026 Intel Corporation
# SPDX-License-Identifier: Apache-2.0

"""Regression coverage for the upstream GPU registry and inference branches."""

import re

import pytest

from app.capabilities import (
    _GPU_MODEL_FALLBACK_BY_PCI_ID,
    _INTEL_GPU_DEVICE_REGISTRY,
    _get_device_sw_capabilities,
    _get_media_codecs,
    _get_precision_support,
)

KNOWN_ARCHES = {"Xe3p", "Xe3", "Xe2", "Xe-LPG", "Xe-HPG", "Xe", "Gen12", "Gen11", "Gen9.5"}


@pytest.mark.parametrize("pci_id", sorted(_INTEL_GPU_DEVICE_REGISTRY))
def test_registry_entry(pci_id: str) -> None:
    entry = _INTEL_GPU_DEVICE_REGISTRY[pci_id]
    assert re.fullmatch(r"8086:[0-9a-f]{4}", pci_id)
    assert set(entry) == {"name", "arch", "category", "min_kernel"}
    assert entry["name"]
    assert entry["arch"] in KNOWN_ARCHES
    assert entry["category"] in ("igpu", "dgpu")
    assert re.fullmatch(r"\d+\.\d+", entry["min_kernel"])
    assert pci_id in _GPU_MODEL_FALLBACK_BY_PCI_ID


@pytest.mark.parametrize("arch", sorted(KNOWN_ARCHES))
def test_architecture_capabilities(arch: str) -> None:
    pci_id = next(pid for pid, entry in _INTEL_GPU_DEVICE_REGISTRY.items() if entry["arch"] == arch)
    category = _INTEL_GPU_DEVICE_REGISTRY[pci_id]["category"]
    assert _get_media_codecs(category, pci_id, None)
    assert _get_precision_support(category, pci_id, None)
    if arch.startswith("Xe"):
        assert "openvino_gpu_inference" in _get_device_sw_capabilities(
            category, pci_id, available_runtimes={}, openvino_devices=set()
        )


@pytest.mark.parametrize(
    "pci_id,arch",
    [
        ("8086:b080", "Xe3"),
        ("8086:fd80", "Xe3"),
        ("8086:d740", "Xe3"),
        ("8086:d750", "Xe3p"),
        ("8086:674c", "Xe3p"),
        ("8086:e20b", "Xe2"),
        ("8086:64a0", "Xe2"),
        ("8086:7dd1", "Xe-LPG"),
        ("8086:b640", "Xe-LPG"),
    ],
)
def test_platform_architecture(pci_id: str, arch: str) -> None:
    assert _INTEL_GPU_DEVICE_REGISTRY[pci_id]["arch"] == arch


@pytest.mark.parametrize("pci_id", ["8086:6422", "8086:b0ff", "8086:d744"])
def test_unregistered_ids(pci_id: str) -> None:
    assert pci_id not in _INTEL_GPU_DEVICE_REGISTRY
