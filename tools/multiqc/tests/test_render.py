import json

from multiqc_colligo import render
from multiqc_colligo.report import State, label, parse

ALL = ["verified", "source-verified", "source-link", "blocking", "advisory", "not-declared",
       "unreadable", "quick", "validate", "permissive-invalid"]


def views_of(example, names):
    out = []
    for name in names:
        view = parse(name, json.dumps(example(name))).view
        view.name = name
        out.append(view)
    return out


def test_only_verified_states_get_a_check_mark(example):
    for view in views_of(example, ALL):
        marked = label(view.state).startswith("✓")
        assert marked == (view.state in (State.VERIFIED, State.VERIFIED_GAPS))
        for f in view.files:
            assert label(f.state).startswith("✓") == (f.state in (State.VERIFIED, State.VERIFIED_GAPS))


def test_general_stats_never_shows_a_failed_or_unverified_report_as_verified(example):
    views = views_of(example, ALL)
    data, headers = render.general_stats(views)
    for view in views:
        shown = data[view.name]["colligo_status"]
        if view.state not in (State.VERIFIED, State.VERIFIED_GAPS):
            assert not shown.startswith("✓"), (view.name, shown)
    assert data["blocking"] == {"colligo_status": "✗ failed", "colligo_source": "ena:DRR021374", "colligo_failures": 2}
    assert data["verified"]["colligo_source"] == "– not compared"
    assert data["quick"]["colligo_status"] == "– not verified"
    assert data["advisory"]["colligo_status"] == "! advisory"


def test_not_checked_comparisons_do_not_carry_a_check_mark(example):
    html = render.source_section(views_of(example, ["not-declared"])[0])
    assert "✓ not checked" not in html
    assert "– not checked" in html
    assert "not declared" in html


def test_every_state_in_the_html_has_a_word_next_to_its_symbol(example):
    html = render.files_table(views_of(example, ALL))
    for symbol, word in [("✓", "verified"), ("✗", "failed"), ("–", "not verified")]:
        assert f"{symbol} {word}" in html


def test_values_from_the_file_are_escaped(example):
    doc = example("blocking")
    doc["files"][0]["path"] = "<script>alert(1)</script>.fastq"
    doc["files"][0]["assertions"][0]["observation"] = '<img src=x onerror="boom()">'
    doc["source"]["runs"][0]["files"][0]["name"] = "<b>bold</b>"
    doc["source"]["accession"] = "<i>acc</i>"
    view = parse("n", json.dumps(doc)).view
    view.name = "<u>name</u>"
    page = (
        render.overview([view], [])
        + render.files_table([view])
        + render.issues_table([view])
        + render.source_section(view)
        + render.not_checked_table([view])
    )
    for raw in ["<script", "<img", "<b>bold", "<i>acc", "<u>name"]:
        assert raw not in page
    assert "&lt;script&gt;" in page


def test_rejected_files_are_listed_with_their_reason():
    from multiqc_colligo.report import Rejected

    html = render.overview([], [Rejected("old.colligo.json", "schema_version 9 is not supported")])
    assert "old.colligo.json" in html
    assert "schema_version 9 is not supported" in html
    assert "alert-warning" in html


def test_problems_table_is_absent_when_nothing_failed(example):
    assert render.issues_table(views_of(example, ["source-verified"])) is None


def test_problems_show_observation_cause_and_action(example):
    html = render.issues_table(views_of(example, ["blocking"]))
    assert "1747050 bases" in html
    assert "Likely (a hypothesis)" in html
    assert "Do: Extract or download the file again" in html


def test_source_section_lists_accession_origin_and_files(example):
    html = render.source_section(views_of(example, ["source-link"])[0])
    assert "taken from the link" in html
    assert "DRR021374_1.fastq.gz" in html
    assert "name" in html


def test_missing_local_file_is_flagged(example):
    doc = example("source-verified")
    doc["source"]["runs"][0]["files"][0]["local"] = None
    view = parse("n", json.dumps(doc)).view
    view.name = "n"
    assert "no local file" in render.source_section(view)


def test_an_md5_that_was_not_compared_is_never_shown_as_matching(example):
    view = views_of(example, ["blocking"])[0]
    files = view.source.runs[0].files
    assert all(f.md5_state is State.NOT_VERIFIED and "decompressed copy" in f.md5_check for f in files)
    html = render.source_section(view)
    assert "– not compared" in html and "✓ matched" not in html
    assert any(n.what.startswith("MD5 and size of DRR021374_1.fastq.gz") for n in view.not_checked)


def test_an_exact_name_that_passed_the_md5_comparison_is_matched(example):
    doc = example("source-verified")
    for raw in doc["files"]:
        raw["assertions"].append({"id": "checksum_mismatch", "severity": "fatal", "passed": True, "declared": "x", "observed": "x"})
    for f in doc["source"]["runs"][0]["files"]:
        f["local"] = f["local"][: -len(".fastq")] + ".fastq.gz"
    for raw, f in zip(doc["files"], doc["source"]["runs"][0]["files"]):
        raw["path"] = f["local"]
    view = parse("exact", json.dumps(doc)).view
    assert all(f.md5_state is State.VERIFIED and f.md5_check == "matched" for f in view.source.runs[0].files)
    assert not any(n.what.startswith("MD5 and size") for n in view.not_checked)


def test_a_failed_md5_comparison_differs(example):
    doc = example("source-verified")
    for f in doc["source"]["runs"][0]["files"]:
        f["local"] = f["local"][: -len(".fastq")] + ".fastq.gz"
    for raw, f in zip(doc["files"], doc["source"]["runs"][0]["files"]):
        raw["path"] = f["local"]
        raw["assertions"].append({"id": "checksum_mismatch", "severity": "fatal", "passed": False, "declared": "x", "observed": "y"})
    view = parse("differs", json.dumps(doc)).view
    assert all(f.md5_state is State.FAILED and f.md5_check == "differs" for f in view.source.runs[0].files)


def test_a_file_linked_by_md5_is_matched(example):
    doc = example("source-verified")
    for f in doc["source"]["runs"][0]["files"]:
        f["linked_by"] = "md5"
    view = parse("by-md5", json.dumps(doc)).view
    assert all(f.md5_state is State.VERIFIED for f in view.source.runs[0].files)


def test_data_files_are_flat(example):
    views = views_of(example, ALL)
    for table in (render.raw_data(views), render.raw_files(views)):
        for row in table.values():
            assert all(not isinstance(v, (dict, list)) for v in row.values()), row
