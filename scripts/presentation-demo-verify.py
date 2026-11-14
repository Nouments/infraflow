#!/usr/bin/env python3
"""Verify the retained output of presentation-demo-launch.sh."""

from __future__ import annotations

import hashlib
import json
import os
import re
import sys
from pathlib import Path, PurePosixPath
from typing import Any


def fail(message: str) -> None:
    raise SystemExit(f"FAIL: {message}")


def require(condition: bool, message: str) -> None:
    if not condition:
        fail(message)


def read_json(path: Path) -> dict[str, Any]:
    require(path.is_file() and not path.is_symlink(), f"missing regular JSON file: {path}")
    try:
        value = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, UnicodeError, json.JSONDecodeError) as error:
        fail(f"cannot decode JSON file {path}: {error}")
    require(isinstance(value, dict), f"expected JSON object in {path}")
    return value


def confined_file(root: Path, relative: str) -> Path:
    posix = PurePosixPath(relative)
    require(
        relative and not posix.is_absolute() and ".." not in posix.parts and "\\" not in relative,
        f"unsafe manifest path: {relative!r}",
    )
    target = root.joinpath(*posix.parts)
    require(target.is_file() and not target.is_symlink(), f"missing regular artifact: {relative}")
    try:
        target.resolve(strict=True).relative_to(root.resolve(strict=True))
    except (OSError, ValueError):
        fail(f"artifact escapes generated output root: {relative}")
    return target


def sha256(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def relative_files(root: Path) -> set[str]:
    found: set[str] = set()
    for current, directories, filenames in os.walk(root, followlinks=False):
        current_path = Path(current)
        for name in list(directories):
            entry = current_path / name
            require(not entry.is_symlink(), f"unexpected symlink directory: {entry}")
        for name in filenames:
            entry = current_path / name
            require(entry.is_file() and not entry.is_symlink(), f"unexpected non-regular file: {entry}")
            found.add(entry.relative_to(root).as_posix())
    return found


def verify_catalog(
    label: str, root: Path, expected_site_names: set[str]
) -> dict[str, dict[str, Any]]:
    root = root.resolve(strict=True)
    require(root.is_dir(), f"{label} output is not a directory: {root}")
    global_manifest = read_json(root / "manifest.json")
    global_report = read_json(root / "generation-report.json")
    require(global_manifest.get("schema_version") == 1, f"{label}: unexpected global manifest schema")
    require(global_manifest.get("status") == "succeeded", f"{label}: global manifest did not succeed")
    require(global_report.get("schema_version") == 1, f"{label}: unexpected global report schema")
    require(global_report.get("status") == "succeeded", f"{label}: generation report did not succeed")
    require(
        global_manifest.get("input_hash") == global_report.get("input_hash"),
        f"{label}: global manifest/report input hashes disagree",
    )
    require(
        global_manifest.get("generation_report") == "generation-report.json",
        f"{label}: unexpected global report path",
    )
    summaries = global_manifest.get("sites")
    report_summaries = global_report.get("sites")
    require(isinstance(summaries, list) and isinstance(report_summaries, list), f"{label}: missing site summaries")
    require(summaries == report_summaries, f"{label}: global manifest/report site summaries disagree")
    require(
        {summary.get("site_name") for summary in summaries} == expected_site_names,
        f"{label}: expected sites {sorted(expected_site_names)}, got "
        f"{sorted(summary.get('site_name') for summary in summaries)}",
    )
    require(len(summaries) == len(expected_site_names), f"{label}: duplicate site summary")

    all_files = {"manifest.json", "generation-report.json"}
    sites: dict[str, dict[str, Any]] = {}
    for summary in summaries:
        name = summary.get("site_name")
        require(isinstance(name, str) and name not in sites, f"{label}: invalid/duplicate site name")
        require(summary.get("status") == "succeeded", f"{label}/{name}: site generation failed")
        catalog = read_json(confined_file(root, f"{name}/manifest.json"))
        site_manifest_path = summary.get("site_manifest_path")
        site_report_path = summary.get("generation_report_path")
        require(site_manifest_path == f"{name}/site-manifest.json", f"{label}/{name}: unexpected site manifest path")
        require(site_report_path == f"{name}/generation-report.json", f"{label}/{name}: unexpected site report path")
        site_manifest_file = confined_file(root, site_manifest_path)
        site_report_file = confined_file(root, site_report_path)
        all_files.update({site_manifest_path, site_report_path})
        site_manifest = read_json(site_manifest_file)
        site_report = read_json(site_report_file)

        require(catalog.get("status") == "succeeded", f"{label}/{name}: catalog did not succeed")
        require(catalog.get("site_id") == summary.get("site_id"), f"{label}/{name}: catalog site ID mismatch")
        require(site_manifest.get("status") == "succeeded", f"{label}/{name}: site manifest did not succeed")
        require(site_manifest.get("site_id") == summary.get("site_id"), f"{label}/{name}: site manifest ID mismatch")
        require(site_manifest.get("site_name") == name, f"{label}/{name}: site manifest name mismatch")
        require(site_report.get("status") == "succeeded", f"{label}/{name}: site report did not succeed")
        local_summaries = site_report.get("sites")
        require(isinstance(local_summaries, list) and len(local_summaries) == 1,
                f"{label}/{name}: site report summary missing")
        expected_local_summary = {
            key: value for key, value in summary.items() if key != "site_manifest_sha256"
        }
        require(local_summaries[0] == expected_local_summary,
                f"{label}/{name}: site report summary mismatch")
        require(
            catalog.get("input_hash") == site_manifest.get("input_hash") == site_report.get("input_hash"),
            f"{label}/{name}: site input hashes disagree",
        )
        require(
            sha256(site_manifest_file) == summary.get("site_manifest_sha256"),
            f"{label}/{name}: site-manifest SHA-256 mismatch",
        )

        artifacts = catalog.get("artifacts")
        details = site_manifest.get("artifacts")
        require(isinstance(artifacts, list) and isinstance(details, list), f"{label}/{name}: missing artifacts")
        require(
            len(artifacts) == summary.get("artifact_count"),
            f"{label}/{name}: summary/catalog artifact count mismatch",
        )
        by_detail: dict[str, dict[str, Any]] = {}
        for detail in details:
            path = detail.get("path")
            require(isinstance(path, str) and path not in by_detail, f"{label}/{name}: duplicate site detail")
            by_detail[path] = detail

        seen_paths: set[str] = set()
        for artifact in artifacts:
            path = artifact.get("path")
            require(
                isinstance(path, str) and path.startswith(f"{name}/") and path not in seen_paths,
                f"{label}/{name}: duplicate or out-of-site artifact path {path!r}",
            )
            seen_paths.add(path)
            file_path = confined_file(root, path)
            actual_hash = sha256(file_path)
            require(
                re.fullmatch(r"[0-9a-f]{64}", str(artifact.get("output_hash", ""))) is not None
                and actual_hash == artifact.get("output_hash"),
                f"{label}/{name}: catalog SHA-256 mismatch for {path}",
            )
            if artifact.get("type") == "site_manifest":
                require(path == site_manifest_path, f"{label}/{name}: unexpected site-manifest artifact path")
                require(actual_hash == summary.get("site_manifest_sha256"), f"{label}/{name}: manifest hash mismatch")
                all_files.add(path)
                continue
            detail = by_detail.get(path)
            require(detail is not None, f"{label}/{name}: no site-manifest detail for {path}")
            require(detail.get("sha256") == actual_hash, f"{label}/{name}: detail SHA-256 mismatch for {path}")
            require(
                detail.get("size_bytes") == file_path.stat().st_size,
                f"{label}/{name}: detail size mismatch for {path}",
            )
            all_files.add(path)
        require(
            {path for path in by_detail} == seen_paths - {site_manifest_path},
            f"{label}/{name}: site details and catalog entries differ",
        )
        require(
            len([item for item in artifacts if item.get("type") == "site_manifest"]) == 1,
            f"{label}/{name}: catalog must contain exactly one site-manifest record",
        )
        all_files.add(f"{name}/manifest.json")
        sites[name] = {
            "catalog": catalog,
            "manifest": site_manifest,
            "summary": summary,
            "artifacts": artifacts,
            "details": by_detail,
        }

    disk_files = relative_files(root)
    require(disk_files == all_files, f"{label}: disk/catalog mismatch; extra={sorted(disk_files-all_files)}, "
            f"missing={sorted(all_files-disk_files)}")
    count = sum(len(site["artifacts"]) for site in sites.values())
    print(f"PASS: {label}: {len(sites)} sites; {count} catalog artifacts; JSON/manifests, paths, sizes, and SHA-256 agree")
    return sites


def artifact_for(site: dict[str, Any], relative: str) -> dict[str, Any]:
    matches = [item for item in site["artifacts"] if item.get("path") == relative]
    require(len(matches) == 1, f"expected exactly one catalog entry for {relative}")
    return matches[0]


def verify_native(
    root: Path,
    site: dict[str, Any],
    relative: str,
    expected: dict[str, str],
    contents: tuple[str, ...],
) -> tuple[Path, str]:
    artifact = artifact_for(site, relative)
    require(artifact.get("type") == "native_device_config", f"{relative}: wrong artifact type")
    details = site["details"].get(relative)
    require(details is not None, f"{relative}: missing artifact provenance")
    for field, value in expected.items():
        require(details.get(field) == value, f"{relative}: expected {field}={value!r}, got {details.get(field)!r}")
    native_manifest_relative = (PurePosixPath(relative).parent / "native-manifest.json").as_posix()
    native_manifest = read_json(confined_file(root, native_manifest_relative))
    require(native_manifest.get("template_version") == "1", f"{relative}: unexpected native template version")
    require(native_manifest.get("status") == "rendered_unverified", f"{relative}: unexpected render status")
    records = [item for item in native_manifest.get("devices", []) if item.get("path") == relative]
    require(len(records) == 1, f"{relative}: native provenance record missing or duplicated")
    record = records[0]
    for field, value in expected.items():
        require(record.get(field) == value, f"{relative}: native manifest {field} mismatch")
    require(record.get("template_version") == "1", f"{relative}: device template version mismatch")
    require(record.get("compatibility_status") == "unknown", f"{relative}: compatibility must remain unknown")
    require(record.get("verification_status") == "not_performed", f"{relative}: device verification must remain unperformed")

    path = confined_file(root, relative)
    text = path.read_text(encoding="utf-8")
    for snippet in contents:
        require(snippet in text, f"{relative}: expected rendered text not found: {snippet!r}")
    actual_hash = sha256(path)
    require(record.get("output_hash") == actual_hash, f"{relative}: native-manifest SHA-256 mismatch")
    require(details.get("sha256") == actual_hash, f"{relative}: site-manifest SHA-256 mismatch")
    size = path.stat().st_size
    print(f"PASS: {relative}: {size} bytes, SHA-256 {actual_hash}")
    return path, text


def main() -> None:
    if len(sys.argv) != 2:
        fail(f"usage: {Path(sys.argv[0]).name} <fresh-demo-root>")
    demo_root = Path(sys.argv[1]).resolve(strict=True)
    require(demo_root.is_dir(), f"demo root is not a directory: {demo_root}")
    require(
        {entry.name for entry in demo_root.iterdir()} == {
            "provisioning", "bootstrap-enabled", "multisite-disabled", "unsupported-ospf.log"
        },
        "demo root contains missing or unexpected entries",
    )
    require(not (demo_root / "unsupported-ospf").exists(), "unsupported OSPF published output")

    provisioning = verify_catalog(
        "provisioning", demo_root / "provisioning", {"cisco-render-demo", "routeros-auto-rsc-demo"}
    )
    enabled = verify_catalog("bootstrap-enabled", demo_root / "bootstrap-enabled", {"lab"})
    disabled = verify_catalog(
        "multisite-disabled", demo_root / "multisite-disabled", {"site-a", "site-b", "site-c", "site-d"}
    )

    cisco_path, cisco_text = verify_native(
        demo_root / "provisioning",
        provisioning["cisco-render-demo"],
        "cisco-render-demo/native/device-cisco-r1.cfg",
        {
            "vendor": "cisco",
            "family": "iosxe",
            "template_id": "infraflow.cisco.iosxe.native",
            "template_version": "1",
        },
        ("ip address 192.0.2.1 255.255.255.0",),
    )
    routeros_path, routeros_text = verify_native(
        demo_root / "provisioning",
        provisioning["routeros-auto-rsc-demo"],
        "routeros-auto-rsc-demo/native/device-mikrotik-r1.auto.rsc",
        {
            "vendor": "mikrotik",
            "family": "routeros",
            "os_version": "7.x",
            "provisioning_method": "auto-rsc",
            "template_id": "infraflow.mikrotik.routeros7.auto-rsc",
            "template_version": "1",
        },
        (
            '/system identity set name="MikroTikR1"',
            '/ip address add address=198.51.100.1/24 interface="ether1"',
        ),
    )
    require(
        not any("/bootstrap/" in item.get("path", "") for site in provisioning.values()
                for item in site["artifacts"]),
        "provisioning fixture unexpectedly emitted bootstrap artifacts",
    )

    enabled_artifacts = {item.get("path") for item in enabled["lab"]["artifacts"]}
    required_bootstrap = {
        "lab/bootstrap/dhcp/config.json",
        "lab/bootstrap/tftp/config.json",
        "lab/bootstrap/pxe/ipxe/bootstrap.ipxe",
        "lab/bootstrap/pxe/ipxe/menu.ipxe",
    }
    require(required_bootstrap <= enabled_artifacts, "explicitly enabled bootstrap outputs are incomplete")
    require("lab/bootstrap/dns/config.json" not in enabled_artifacts,
            "bootstrap DNS output appeared although the fixture does not enable DNS")
    for name, site in disabled.items():
        require(
            not any("/bootstrap/" in item.get("path", "") for item in site["artifacts"]),
            f"{name}: bootstrap artifact emitted despite explicit service opt-out",
        )
        require(not (demo_root / "multisite-disabled" / name / "bootstrap").exists(),
                f"{name}: bootstrap directory exists despite explicit service opt-out")
    print("PASS: bootstrap files are emitted for explicit opt-in and absent for explicit opt-out")

    rejection = (demo_root / "unsupported-ospf.log").read_text(encoding="utf-8")
    expected_error = (
        "infraflow-provider: spec.devices[0].routing.ospf: unsupported; "
        "no matching OSPF template is registered"
    )
    require(rejection.splitlines().count(expected_error) == 1, "unsupported OSPF rejection text differs")
    require(not (demo_root / "unsupported-ospf").exists(), "unsupported OSPF produced output")
    print(f"PASS: unsupported OSPF rejected without output: {expected_error}")

    print("\n--- Cisco native configuration (render-only) ---")
    print(f"Path: {cisco_path}")
    print(cisco_text, end="" if cisco_text.endswith("\n") else "\n")
    print("\n--- MikroTik RouterOS .auto.rsc (render-only) ---")
    print(f"Path: {routeros_path}")
    print(routeros_text, end="" if routeros_text.endswith("\n") else "\n")
    print("\nPASS: demo verification complete; artifacts were not applied or device-verified.")


if __name__ == "__main__":
    main()
