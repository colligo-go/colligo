import json
import re
from dataclasses import dataclass
from enum import Enum, IntEnum

SUPPORTED_SCHEMA_VERSIONS = frozenset({2})

BLOCKING_SEVERITIES = frozenset({"fatal", "blocking"})
COUNT_ASSERTIONS = {"count_mismatch": "base_count", "read_count_unexplained": "read_count"}


class State(IntEnum):
    VERIFIED = 0
    VERIFIED_GAPS = 1
    ADVISORY = 2
    NOT_VERIFIED = 3
    UNKNOWN = 4
    FAILED = 5


SYMBOLS = {
    State.VERIFIED: "✓",
    State.VERIFIED_GAPS: "✓",
    State.ADVISORY: "!",
    State.NOT_VERIFIED: "–",
    State.UNKNOWN: "–",
    State.FAILED: "✗",
}

WORDS = {
    State.VERIFIED: "verified",
    State.VERIFIED_GAPS: "verified, with gaps",
    State.ADVISORY: "advisory",
    State.NOT_VERIFIED: "not verified",
    State.UNKNOWN: "unknown",
    State.FAILED: "failed",
}


def label(state):
    return f"{SYMBOLS[state]} {WORDS[state]}"


class Outcome(Enum):
    PASSED = "passed"
    SKIPPED = "skipped"
    BLOCKING = "blocking"
    ADVISORY = "advisory"
    UNRECOGNISED = "unrecognised"


def outcome_of(assertion):
    if not isinstance(assertion, dict):
        return Outcome.UNRECOGNISED
    if assertion.get("skipped") is True:
        return Outcome.SKIPPED
    if assertion.get("passed") is True:
        return Outcome.PASSED
    severity = assertion.get("severity")
    if severity in BLOCKING_SEVERITIES:
        return Outcome.BLOCKING
    if severity == "advisory":
        return Outcome.ADVISORY
    return Outcome.UNRECOGNISED


def state_of_outcomes(outcomes):
    state = State.VERIFIED
    if Outcome.BLOCKING in outcomes:
        state = State.FAILED
    elif Outcome.UNRECOGNISED in outcomes:
        state = State.UNKNOWN
    elif Outcome.ADVISORY in outcomes:
        state = State.ADVISORY
    elif Outcome.SKIPPED in outcomes:
        state = State.VERIFIED_GAPS
    return state


STATUS_STATES = {
    "verified": State.VERIFIED,
    "valid": State.NOT_VERIFIED,
    "quick": State.NOT_VERIFIED,
    "invalid": State.FAILED,
    "unreadable": State.FAILED,
}

STATUS_NOTES = {
    "valid": "structure checked only, never verified",
    "quick": "partial read, never verified",
}


def file_state(status, outcomes):
    from_status = STATUS_STATES.get(status, State.UNKNOWN)
    from_assertions = state_of_outcomes(outcomes)
    if from_status == State.VERIFIED:
        return from_assertions
    return max(from_status, from_assertions)


@dataclass(frozen=True)
class Issue:
    id: str
    severity: str
    observation: str
    probable_cause: str
    suggested_action: str
    declared: str
    observed: str
    subjects: tuple


@dataclass(frozen=True)
class NotChecked:
    subject: str
    what: str
    why: str


@dataclass
class FileView:
    path: str
    status: str
    state: State
    size: object
    reads: object
    note: str
    advisories: int
    not_checked: int


@dataclass
class SourceFile:
    name: str
    local: object
    linked_by: str
    size: object
    md5: str


@dataclass
class Comparison:
    id: str
    severity: str
    declared: str
    observed: str
    result: str
    state: State
    reason: str


@dataclass
class RunView:
    accession: str
    layout: str
    status: str
    declared_reads: object
    declared_bases: object
    observed_bases: object
    files: list
    comparisons: list


@dataclass
class SourceView:
    id: str
    accession: str
    accession_from: str
    runs: list
    comparisons: list


@dataclass
class ReportView:
    name: str
    exit_code: object
    state: State
    note: str
    files: list
    source: object
    issues: list
    not_checked: list
    failures: int
    advisories: int


@dataclass
class Rejected:
    name: str
    reason: str


@dataclass
class Parsed:
    view: object = None
    rejected: object = None


def text(value):
    if value is None:
        return ""
    return str(value)


def whole_number(value):
    if isinstance(value, bool) or not isinstance(value, int):
        return None
    return value


def dicts(value):
    if not isinstance(value, list):
        return []
    return [item for item in value if isinstance(item, dict)]


def assertions_of(owner):
    raw = owner.get("assertions")
    if not isinstance(raw, list):
        return []
    return raw


def reads_of(assertions):
    for assertion in assertions:
        if not isinstance(assertion, dict) or assertion.get("id") != "pair_count":
            continue
        if outcome_of(assertion) is not Outcome.PASSED:
            continue
        observed = text(assertion.get("observed"))
        if re.fullmatch(r"\d+", observed):
            return int(observed)
    return None


class IssueCollector:
    def __init__(self):
        self.groups = {}
        self.not_checked = []

    def add(self, subject, assertion):
        outcome = outcome_of(assertion)
        if outcome is Outcome.SKIPPED:
            self.not_checked.append(NotChecked(subject, text(assertion.get("id")), skip_reason(assertion)))
            return
        if outcome is Outcome.PASSED:
            return
        if not isinstance(assertion, dict):
            assertion = {}
        severity = text(assertion.get("severity")) if outcome is not Outcome.UNRECOGNISED else "unrecognised"
        key = (
            text(assertion.get("id")),
            severity,
            text(assertion.get("observation")),
            text(assertion.get("probable_cause")),
            text(assertion.get("suggested_action")),
            text(assertion.get("declared")),
            text(assertion.get("observed")),
        )
        self.groups.setdefault(key, []).append(subject)

    def issues(self):
        found = [Issue(*key, subjects=tuple(subjects)) for key, subjects in self.groups.items()]
        rank = {"fatal": 0, "blocking": 1, "unrecognised": 2, "advisory": 3}
        found.sort(key=lambda issue: rank.get(issue.severity, 2))
        return found


NOT_DECLARED = "the source does not declare it"


def skip_reason(assertion):
    by = text(assertion.get("skipped_by"))
    if by == "not_declared":
        return NOT_DECLARED
    if by:
        return f"skipped because {by} did not pass"
    return "skipped by Colligo"


def comparison_of(assertion, run_declared):
    outcome = outcome_of(assertion)
    assertion = assertion if isinstance(assertion, dict) else {}
    ident = text(assertion.get("id"))
    severity = text(assertion.get("severity"))
    declared = text(assertion.get("declared"))
    observed = text(assertion.get("observed"))
    counted = COUNT_ASSERTIONS.get(ident)
    undeclared = counted is not None and run_declared.get(counted) is None
    if counted is not None:
        declared = "not declared" if undeclared else (declared or text(run_declared.get(counted)))
    if outcome is Outcome.SKIPPED:
        reason = NOT_DECLARED if undeclared else skip_reason(assertion)
        return Comparison(ident, severity, declared, observed, "not checked", State.NOT_VERIFIED, reason)
    if outcome is Outcome.PASSED and undeclared:
        return Comparison(ident, severity, declared, observed, "not checked", State.NOT_VERIFIED, NOT_DECLARED)
    if outcome is Outcome.PASSED:
        return Comparison(ident, severity, declared, observed, "passed", State.VERIFIED, "")
    if outcome is Outcome.BLOCKING:
        return Comparison(ident, severity, declared, observed, "failed", State.FAILED, text(assertion.get("observation")))
    if outcome is Outcome.ADVISORY:
        return Comparison(ident, severity, declared, observed, "advisory", State.ADVISORY, text(assertion.get("observation")))
    return Comparison(ident, severity, declared, observed, "unrecognised", State.UNKNOWN, "Colligo reported an assertion this module cannot read")


def run_view(run, collector):
    accession = text(run.get("accession"))
    declared = run.get("declared") if isinstance(run.get("declared"), dict) else {}
    raw_assertions = assertions_of(run)
    for assertion in raw_assertions:
        collector.add(f"run {accession}", assertion)
    observed_bases = None
    for assertion in raw_assertions:
        if isinstance(assertion, dict) and assertion.get("id") == "count_mismatch":
            if re.fullmatch(r"\d+", text(assertion.get("observed"))):
                observed_bases = int(assertion["observed"])
    files = [
        SourceFile(
            text(f.get("name")),
            f.get("local") if isinstance(f.get("local"), str) else None,
            text(f.get("linked_by")),
            whole_number(f.get("size")),
            text(f.get("md5")),
        )
        for f in dicts(run.get("files"))
    ]
    return RunView(
        accession,
        text(run.get("layout")),
        text(run.get("status")),
        whole_number(declared.get("read_count")),
        whole_number(declared.get("base_count")),
        observed_bases,
        files,
        [comparison_of(a, declared) for a in raw_assertions],
    )


def source_view(source, collector):
    runs = [run_view(run, collector) for run in dicts(source.get("runs"))]
    raw_assertions = assertions_of(source)
    for assertion in raw_assertions:
        collector.add("source response", assertion)
    return SourceView(
        text(source.get("id")),
        text(source.get("accession")),
        text(source.get("accession_from")),
        runs,
        [comparison_of(a, {}) for a in raw_assertions],
    )


def states_of_source(source):
    states = []
    for run in source.runs:
        states.extend(State.VERIFIED_GAPS if c.state is State.NOT_VERIFIED else c.state for c in run.comparisons)
        if run.declared_reads is None or run.declared_bases is None:
            states.append(State.VERIFIED_GAPS)
    states.extend(State.VERIFIED_GAPS if c.state is State.NOT_VERIFIED else c.state for c in source.comparisons)
    return states


def failures_count(files, issues):
    blocking_issues = sum(1 for issue in issues if issue.severity in BLOCKING_SEVERITIES)
    unexplained = sum(
        1
        for f in files
        if f.status in ("invalid", "unreadable") and not any(
            issue.severity in BLOCKING_SEVERITIES and f.path in issue.subjects for issue in issues
        )
    )
    return blocking_issues + unexplained


def view_of(name, doc):
    collector = IssueCollector()
    files = []
    for raw in dicts(doc.get("files")):
        path = text(raw.get("path"))
        status = text(raw.get("status"))
        raw_assertions = assertions_of(raw)
        outcomes = [outcome_of(a) for a in raw_assertions]
        for assertion in raw_assertions:
            collector.add(path, assertion)
        files.append(
            FileView(
                path,
                status,
                file_state(status, outcomes),
                whole_number(raw.get("size")),
                reads_of(raw_assertions),
                STATUS_NOTES.get(status, "" if status in STATUS_STATES else f"status {status!r} is not one this module knows"),
                sum(1 for o in outcomes if o is Outcome.ADVISORY),
                sum(1 for o in outcomes if o is Outcome.SKIPPED),
            )
        )
    source = None
    if isinstance(doc.get("source"), dict):
        source = source_view(doc["source"], collector)
    else:
        collector.not_checked.append(
            NotChecked("report", "comparison with a source", "not made: the report has no source section")
        )
    for run in source.runs if source else []:
        subject = f"run {run.accession}"
        for field_, declared, assertion_id in (
            ("read_count", run.declared_reads, "read_count_unexplained"),
            ("base_count", run.declared_bases, "count_mismatch"),
        ):
            already = any(n.subject == subject and n.what == assertion_id for n in collector.not_checked)
            if declared is None and not already:
                collector.not_checked.append(NotChecked(subject, field_, NOT_DECLARED))
    exit_code = doc.get("exit_code")
    states = [f.state for f in files]
    if source:
        states.extend(states_of_source(source))
    note = ""
    if not files and not (source and source.runs):
        state = State.NOT_VERIFIED
        note = "nothing was checked"
    else:
        state = max(states) if states else State.VERIFIED
        if state in (State.VERIFIED, State.VERIFIED_GAPS) and exit_code != 0:
            state = State.UNKNOWN
            note = f"exit code {text(exit_code)} is not explained by anything in the report"
    issues = collector.issues()
    return ReportView(
        name,
        exit_code,
        state,
        note,
        files,
        source,
        issues,
        collector.not_checked,
        failures_count(files, issues),
        sum(1 for issue in issues if issue.severity == "advisory"),
    )


def parse(name, content):
    try:
        doc = json.loads(content)
    except (ValueError, TypeError) as err:
        return Parsed(rejected=Rejected(name, f"not valid JSON ({err})"))
    if not isinstance(doc, dict) or "schema_version" not in doc:
        return Parsed(rejected=Rejected(name, "not a Colligo report: no schema_version"))
    version = doc.get("schema_version")
    if isinstance(version, bool) or version not in SUPPORTED_SCHEMA_VERSIONS:
        supported = ", ".join(str(v) for v in sorted(SUPPORTED_SCHEMA_VERSIONS))
        return Parsed(
            rejected=Rejected(
                name,
                f"schema_version {text(version)} is not supported (this module reads {supported}); "
                "nothing from this file is shown. Update multiqc-colligo",
            )
        )
    if not isinstance(doc.get("files"), list) or not isinstance(doc.get("exit_code"), int) or isinstance(doc.get("exit_code"), bool):
        return Parsed(rejected=Rejected(name, "not a Colligo check report: no files list or exit_code"))
    if len(dicts(doc["files"])) != len(doc["files"]):
        return Parsed(rejected=Rejected(name, "malformed: an entry of files is not an object"))
    return Parsed(view=view_of(name, doc))
