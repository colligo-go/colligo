import json

import pytest

from multiqc_colligo.report import Outcome, State, label, outcome_of, parse


def view_of(doc, name="r"):
    parsed = parse(name, json.dumps(doc))
    assert parsed.rejected is None, parsed.rejected
    return parsed.view


@pytest.mark.parametrize(
    "name, state, failures, exit_code",
    [
        ("verified", State.VERIFIED, 0, 0),
        ("source-verified", State.VERIFIED, 0, 0),
        ("source-link", State.VERIFIED, 0, 0),
        ("blocking", State.FAILED, 2, 3),
        ("advisory", State.ADVISORY, 0, 0),
        ("not-declared", State.ADVISORY, 0, 0),
        ("unreadable", State.FAILED, 1, 2),
        ("quick", State.NOT_VERIFIED, 0, 5),
        ("validate", State.NOT_VERIFIED, 0, 0),
        ("permissive-invalid", State.FAILED, 1, 0),
    ],
)
def test_examples(example, name, state, failures, exit_code):
    view = view_of(example(name))
    assert view.state is state
    assert view.failures == failures
    assert view.exit_code == exit_code


def test_verified_without_source_says_the_comparison_was_not_made(example):
    view = view_of(example("verified"))
    assert any(n.what == "comparison with a source" for n in view.not_checked)


def test_only_a_clean_report_is_verified(example):
    assert view_of(example("source-verified")).state is State.VERIFIED


def test_verified_status_with_a_failed_blocking_assertion_is_failed(example):
    doc = example("source-verified")
    assertion = doc["files"][0]["assertions"][0]
    assertion.update(passed=False, observation="x")
    assertion["severity"] = "blocking"
    assert view_of(doc).state is State.FAILED


def test_exit_code_that_the_report_does_not_explain_is_never_verified(example):
    doc = example("source-verified")
    doc["exit_code"] = 3
    view = view_of(doc)
    assert view.state is State.UNKNOWN
    assert "exit code 3" in view.note


def test_permissive_exit_zero_does_not_hide_an_invalid_file(example):
    view = view_of(example("permissive-invalid"))
    assert view.exit_code == 0
    assert view.files[0].state is State.FAILED


@pytest.mark.parametrize("status", ["valid", "quick"])
def test_valid_and_quick_are_not_verified(example, status):
    doc = example("source-verified")
    doc["files"][0]["status"] = status
    assert view_of(doc).files[0].state is State.NOT_VERIFIED


def test_unknown_status_is_unknown_not_ok(example):
    doc = example("source-verified")
    doc["files"][0]["status"] = "approved"
    view = view_of(doc)
    assert view.files[0].state is State.UNKNOWN
    assert view.state is State.UNKNOWN


def test_missing_status_is_unknown(example):
    doc = example("source-verified")
    del doc["files"][0]["status"]
    assert view_of(doc).files[0].state is State.UNKNOWN


def test_assertion_without_passed_is_a_failure():
    assert outcome_of({"id": "x", "severity": "blocking"}) is Outcome.BLOCKING
    assert outcome_of({"id": "x", "severity": "mystery"}) is Outcome.UNRECOGNISED
    assert outcome_of("garbage") is Outcome.UNRECOGNISED


def test_skipped_is_not_passed():
    assert outcome_of({"passed": False, "skipped": True, "severity": "blocking"}) is Outcome.SKIPPED
    assert outcome_of({"passed": True, "skipped": True}) is Outcome.SKIPPED


def test_stub_report_with_no_file_is_not_verified():
    view = view_of({"schema_version": 2, "files": [], "exit_code": 0})
    assert view.state is State.NOT_VERIFIED
    assert view.note == "nothing was checked"


def test_undeclared_count_that_the_assertion_passed_is_not_checked(example):
    doc = example("source-verified")
    run = doc["source"]["runs"][0]
    run["declared"]["base_count"] = None
    view = view_of(doc)
    comparison = next(c for c in view.source.runs[0].comparisons if c.id == "count_mismatch")
    assert comparison.result == "not checked"
    assert comparison.state is State.NOT_VERIFIED
    assert view.state is State.VERIFIED_GAPS
    assert any(n.what == "base_count" for n in view.not_checked)


def test_not_declared_example_marks_read_count_not_checked(example):
    view = view_of(example("not-declared"))
    comparison = next(c for c in view.source.runs[0].comparisons if c.id == "read_count_unexplained")
    assert comparison.result == "not checked"
    assert comparison.declared == "not declared"
    assert label(comparison.state).startswith("–")


def test_skipped_comparison_after_a_failure_keeps_the_declared_value(example):
    view = view_of(example("blocking"))
    comparison = next(c for c in view.source.runs[0].comparisons if c.id == "read_count_unexplained")
    assert comparison.declared == "13767"
    assert comparison.reason == "skipped because count_mismatch did not pass"


def test_run_failure_is_one_issue_for_all_its_files(example):
    view = view_of(example("blocking"))
    shared = [i for i in view.issues if i.id == "source_run_failed"]
    assert len(shared) == 1
    assert len(shared[0].subjects) == 2


def test_issues_are_ordered_blocking_before_advisory(example):
    doc = example("blocking")
    doc["source"]["runs"][0]["assertions"][3].update(passed=False, severity="advisory", observation="y")
    severities = [i.severity for i in view_of(doc).issues]
    assert severities.index("blocking") < severities.index("advisory")


def without_counts(doc):
    for f in doc["files"]:
        f.pop("reads", None)
        f.pop("bases", None)
    return doc


def test_reads_and_bases_come_from_the_fields_of_each_file(example):
    view = view_of(example("verified"))
    assert [(f.reads, f.bases) for f in view.files] == [(20, 200), (20, 200)]


def test_a_report_without_the_fields_falls_back_to_a_passed_pair_check(example):
    view = view_of(without_counts(example("verified")))
    assert [(f.reads, f.bases) for f in view.files] == [(20, None), (20, None)]
    doc = without_counts(example("verified"))
    for a in doc["files"][0]["assertions"]:
        if a["id"] == "pair_count":
            a.update(passed=False, severity="blocking", observed="19")
    assert view_of(doc).files[0].reads is None


def test_the_field_wins_over_the_pair_check(example):
    doc = example("verified")
    doc["files"][0]["reads"] = 7
    assert view_of(doc).files[0].reads == 7


@pytest.mark.parametrize("bad", ["12", 1.5, True, -3, None, [1]])
def test_a_count_that_is_not_a_whole_non_negative_number_is_not_shown(example, bad):
    doc = without_counts(example("verified"))
    doc["files"][0]["reads"] = bad
    doc["files"][0]["bases"] = bad
    for a in doc["files"][0]["assertions"]:
        if a["id"] == "pair_count":
            a.update(passed=False, severity="blocking")
    view = view_of(doc).files[0]
    assert view.reads is None
    assert view.bases is None


def test_zero_is_shown_as_zero_and_not_as_absent(example):
    doc = example("verified")
    doc["files"][0]["reads"] = 0
    doc["files"][0]["bases"] = 0
    view = view_of(doc).files[0]
    assert (view.reads, view.bases) == (0, 0)


def test_a_file_that_was_not_read_whole_has_neither(example):
    for f in view_of(example("quick")).files:
        assert (f.reads, f.bases) == (None, None)
    for f in view_of(example("unreadable")).files:
        if f.status == "unreadable":
            assert (f.reads, f.bases) == (None, None)
    for f in view_of(example("permissive-invalid")).files:
        assert f.status == "invalid"
        assert (f.reads, f.bases) == (None, None)


def test_unsupported_schema_version_is_rejected_with_a_reason(example):
    doc = example("verified")
    doc["schema_version"] = 3
    parsed = parse("x.colligo.json", json.dumps(doc))
    assert parsed.view is None
    assert "schema_version 3 is not supported" in parsed.rejected.reason


@pytest.mark.parametrize("version", [None, "2", True, 1.5, 0])
def test_odd_schema_versions_are_rejected(example, version):
    doc = example("verified")
    doc["schema_version"] = version
    assert parse("x", json.dumps(doc)).view is None


@pytest.mark.parametrize(
    "content",
    ["", "not json", "[]", "{}", '{"schema_version": 2}', '{"schema_version": 2, "files": {}, "exit_code": 0}',
     '{"schema_version": 2, "files": [], "exit_code": "0"}', '{"schema_version": 2, "files": [1], "exit_code": 0}'],
)
def test_malformed_documents_are_rejected(content):
    parsed = parse("x", content)
    assert parsed.view is None
    assert parsed.rejected.name == "x"


def test_identify_document_is_not_taken_for_a_check_report():
    parsed = parse("x", json.dumps({"schema_version": 1, "files": [], "exit_code": 0}))
    assert parsed.view is None


def test_fields_of_the_wrong_type_do_not_crash(example):
    doc = example("blocking")
    doc["files"][0]["size"] = "big"
    doc["files"][0]["assertions"] = "none"
    doc["source"]["runs"][0]["declared"] = []
    doc["source"]["runs"][0]["files"] = [None, 3]
    view = view_of(doc)
    assert view.state in (State.FAILED, State.UNKNOWN, State.NOT_VERIFIED)
