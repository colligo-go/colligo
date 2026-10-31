import json
import shutil
import subprocess
import sys
from pathlib import Path

import pytest

from conftest import EXAMPLES

MULTIQC = shutil.which("multiqc", path=str(Path(sys.executable).parent)) or shutil.which("multiqc")


def run_multiqc(folder, out):
    done = subprocess.run(
        [MULTIQC, str(folder), "-o", str(out), "-n", "report", "--no-ansi", "--force"],
        capture_output=True, text=True, timeout=300,
    )
    assert done.returncode == 0, done.stdout + done.stderr
    return done


@pytest.fixture(scope="module")
def full_report(tmp_path_factory):
    out = tmp_path_factory.mktemp("out")
    run_multiqc(EXAMPLES, out)
    return out


def general_stats(out):
    lines = (out / "report_data" / "multiqc_general_stats.txt").read_text().splitlines()
    header = lines[0].split("\t")
    return {row.split("\t")[0]: dict(zip(header[1:], row.split("\t")[1:])) for row in lines[1:]}


def test_report_has_the_colligo_sections(full_report):
    html = (full_report / "report.html").read_text()
    for anchor in ["colligo-files", "colligo-problems", "colligo-not-checked", "colligo-source-1"]:
        assert f'id="{anchor}"' in html


def test_general_statistics_columns(full_report):
    rows = general_stats(full_report)
    status = "colligo-colligo_status"
    failures = "colligo-colligo_failures"
    assert rows["verified"][status] == "✓ verified"
    assert rows["source-verified"][status] == "✓ verified"
    assert rows["blocking"][status] == "✗ failed"
    assert rows["blocking"][failures] == "2"
    assert rows["advisory"][status] == "! advisory"
    assert rows["quick"][status] == "– not verified"
    assert rows["validate"][status] == "– not verified"
    assert rows["permissive-invalid"][status] == "✗ failed"
    assert rows["unreadable"][status] == "✗ failed"


def test_nothing_unverified_is_shown_as_verified_in_the_html(full_report):
    html = (full_report / "report.html").read_text()
    assert "✓ not checked" not in html
    assert "✓ not verified" not in html
    assert "✓ failed" not in html


def test_unsupported_schema_is_reported_and_left_out(tmp_path):
    folder = tmp_path / "in"
    folder.mkdir()
    shutil.copy(EXAMPLES / "verified.colligo.json", folder / "good.colligo.json")
    future = json.loads((EXAMPLES / "verified.colligo.json").read_text())
    future["schema_version"] = 3
    (folder / "future.colligo.json").write_text(json.dumps(future))
    out = tmp_path / "out"
    done = run_multiqc(folder, out)
    html = (out / "report.html").read_text()
    assert "schema_version 3 is not supported" in html
    assert "future.colligo.json" in html
    rows = general_stats(out)
    assert list(rows) == ["good"]
    assert "future.colligo.json" in done.stdout + done.stderr


def test_only_unsupported_files_still_leaves_a_visible_warning(tmp_path):
    folder = tmp_path / "in"
    folder.mkdir()
    (folder / "only.colligo.json").write_text(json.dumps({"schema_version": 9, "files": [], "exit_code": 0}))
    out = tmp_path / "out"
    run_multiqc(folder, out)
    html = (out / "report.html").read_text()
    assert "schema_version 9 is not supported" in html
    assert "✓ verified" not in html.split('id="colligo-files"')[1]


def test_a_json_that_is_not_a_colligo_report_is_not_used(tmp_path):
    folder = tmp_path / "in"
    folder.mkdir()
    shutil.copy(EXAMPLES / "verified.colligo.json", folder / "good.colligo.json")
    (folder / "other.colligo.json").write_text(json.dumps({"hello": "world"}))
    out = tmp_path / "out"
    run_multiqc(folder, out)
    assert list(general_stats(out)) == ["good"]
    assert "not a Colligo report" in (out / "report.html").read_text()


def test_same_name_in_two_folders_is_not_merged(tmp_path):
    for sub, name in [("a", "verified"), ("b", "blocking")]:
        (tmp_path / "in" / sub).mkdir(parents=True)
        shutil.copy(EXAMPLES / f"{name}.colligo.json", tmp_path / "in" / sub / "sample.colligo.json")
    out = tmp_path / "out"
    run_multiqc(tmp_path / "in", out)
    rows = general_stats(out)
    assert len(rows) == 2
    assert sorted(r["colligo-colligo_status"] for r in rows.values()) == ["✓ verified", "✗ failed"]


def test_no_colligo_file_means_no_colligo_section(tmp_path):
    folder = tmp_path / "in"
    folder.mkdir()
    (folder / "x.txt").write_text("nothing")
    out = tmp_path / "out"
    done = subprocess.run([MULTIQC, str(folder), "-o", str(out), "--no-ansi"], capture_output=True, text=True, timeout=300)
    assert "Colligo reports" not in done.stdout + done.stderr
    assert not list(out.glob("*.html"))
