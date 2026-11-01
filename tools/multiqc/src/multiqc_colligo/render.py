from html import escape

from .report import BLOCKING_SEVERITIES, SYMBOLS, WORDS, State, label

STYLE = (
    "<style>"
    ".colligo-ok{color:#2e7d32}.colligo-bad{color:#c62828;font-weight:bold}"
    ".colligo-warn{color:#a35f00}.colligo-dim{opacity:.7}"
    ".colligo-num{text-align:right;white-space:nowrap}"
    ".colligo-path{word-break:break-all}"
    ".colligo-table{width:100%;margin-bottom:1em}"
    ".colligo-table th,.colligo-table td{padding:2px 8px;vertical-align:top}"
    "</style>"
)

CSS_CLASS = {
    State.VERIFIED: "colligo-ok",
    State.VERIFIED_GAPS: "colligo-ok",
    State.ADVISORY: "colligo-warn",
    State.NOT_VERIFIED: "colligo-dim",
    State.UNKNOWN: "colligo-dim",
    State.FAILED: "colligo-bad",
}

GENERAL_STATS_LABELS = {state: label(state) for state in State}


def esc(value):
    return escape("" if value is None else str(value), quote=True)


def number(value):
    if isinstance(value, int) and not isinstance(value, bool):
        return f"{value:,}"
    return "–"


def badge(state):
    return f'<span class="{CSS_CLASS[state]}">{esc(label(state))}</span>'


def table(headers, rows, numeric=()):
    head = "".join(f"<th>{esc(h)}</th>" for h in headers)
    body = []
    for row in rows:
        cells = []
        for index, cell in enumerate(row):
            css = ' class="colligo-num"' if index in numeric else ""
            cells.append(f"<td{css}>{cell}</td>")
        body.append(f"<tr>{''.join(cells)}</tr>")
    return (
        f'<table class="table table-condensed colligo-table"><thead><tr>{head}</tr></thead>'
        f"<tbody>{''.join(body)}</tbody></table>"
    )


def path_cell(path):
    return f'<code class="colligo-path">{esc(path)}</code>'


def legend():
    items = [
        (State.VERIFIED, "Colligo verified it"),
        (State.ADVISORY, "worth a look"),
        (State.FAILED, "blocking: do not use"),
        (State.NOT_VERIFIED, "not verified: never counted as OK"),
    ]
    parts = [
        f'<span class="{CSS_CLASS[state]}">{esc(SYMBOLS[state])} {esc(text)}</span>' for state, text in items
    ]
    return f'<p class="colligo-dim">{" · ".join(parts)}</p>'


def overview(views, rejected):
    counts = {}
    for view in views:
        counts[view.state] = counts.get(view.state, 0) + 1
    lines = []
    for state in sorted(counts, reverse=True):
        lines.append(f"{badge(state)}: {counts[state]}")
    summary = f"<p><strong>{len(views)} Colligo report(s)</strong>: " + ", ".join(lines) + "</p>" if views else ""
    notices = ""
    if rejected:
        items = "".join(f"<li><code>{esc(r.name)}</code>: {esc(r.reason)}</li>" for r in rejected)
        notices = (
            '<div class="alert alert-warning"><strong>Not shown.</strong> '
            f"These files were found but not used:<ul>{items}</ul></div>"
        )
    return STYLE + notices + summary + legend()


def files_table(views):
    rows = []
    for view in views:
        for f in view.files:
            note = f' <span class="colligo-dim">({esc(f.note)})</span>' if f.note else ""
            rows.append(
                [
                    esc(view.name),
                    path_cell(f.path),
                    badge(f.state) + note,
                    esc("" if view.exit_code is None else view.exit_code),
                    badge(view.state),
                    number(f.reads),
                    number(f.bases),
                    number(f.advisories),
                    number(f.not_checked),
                ]
            )
        if not view.files:
            rows.append(
                [esc(view.name), '<span class="colligo-dim">no file</span>', badge(view.state), esc(view.exit_code), badge(view.state), "–", "–", "–", "–"]
            )
    return table(
        ["Report", "File", "File status", "Exit code", "Report result", "Reads", "Bases", "Advisory", "Not checked"],
        rows,
        numeric=(3, 5, 6, 7, 8),
    ) + (
        '<p class="colligo-dim">Reads and Bases are what Colligo counted in the one read of each FASTQ file. '
        "They are blank for a file that was not read whole (<code>--quick</code>), for FASTA and GTF, and for a file that was unreadable or invalid. "
        "A report written before the report carried them shows the reads of the pair check, for paired files only.</p>"
    )


def report_notes(views):
    notes = []
    for view in views:
        if view.note:
            notes.append(f"<li><code>{esc(view.name)}</code>: {badge(view.state)}, {esc(view.note)}</li>")
    if not notes:
        return ""
    return f'<ul>{"".join(notes)}</ul>'


def subjects_cell(subjects):
    shown = list(subjects)
    return "<br>".join(f'<code class="colligo-path">{esc(s)}</code>' for s in shown)


def severity_state(severity):
    if severity in BLOCKING_SEVERITIES:
        return State.FAILED
    if severity == "advisory":
        return State.ADVISORY
    return State.UNKNOWN


def issues_table(views):
    rows = []
    for view in views:
        for issue in view.issues:
            state = severity_state(issue.severity)
            seen = [
                f"<strong>{esc(issue.observation) or esc(issue.id)}</strong>",
                f"Declared: {esc(issue.declared)}; observed: {esc(issue.observed)}" if issue.declared or issue.observed else "",
                f'<span class="colligo-dim">Likely (a hypothesis): {esc(issue.probable_cause)}</span>' if issue.probable_cause else "",
                f"Do: {esc(issue.suggested_action)}" if issue.suggested_action else "",
            ]
            rows.append(
                [
                    esc(view.name),
                    f'<span class="{CSS_CLASS[state]}">{esc(SYMBOLS[state])} {esc(issue.severity)}</span>',
                    f'<code>{esc(issue.id)}</code>',
                    "<br>".join(part for part in seen if part),
                    subjects_cell(issue.subjects),
                ]
            )
    if not rows:
        return None
    return table(["Report", "Severity", "Assertion", "What Colligo found", "Applies to"], rows)


def not_checked_table(views):
    rows = []
    for view in views:
        for item in view.not_checked:
            rows.append(
                [esc(view.name), f"<code>{esc(item.subject)}</code>", f"<code>{esc(item.what)}</code>", esc(item.why)]
            )
    if not rows:
        return None
    return table(["Report", "Subject", "Not checked", "Why"], rows)


ACCESSION_FROM = {
    "link": "taken from the link",
    "deduced": "deduced from the file names",
    "normalized": "upper-cased from what was typed",
}


def comparison_rows(comparisons):
    rows = []
    for c in comparisons:
        rows.append(
            [
                f"<code>{esc(c.id)}</code>",
                esc(c.declared),
                esc(c.observed),
                f'<span class="{CSS_CLASS[c.state]}">{esc(SYMBOLS[c.state])} {esc(c.result)}</span>'
                + (f' <span class="colligo-dim">({esc(c.reason)})</span>' if c.result == "not checked" else ""),
            ]
        )
    return rows


def run_block(run):
    declared = (
        f"reads declared: {number(run.declared_reads) if run.declared_reads is not None else 'not declared'}; "
        f"bases declared: {number(run.declared_bases) if run.declared_bases is not None else 'not declared'}; "
        f"bases observed: {number(run.observed_bases)}"
    )
    head = (
        f"<h4><code>{esc(run.accession)}</code></h4>"
        f'<p class="colligo-dim">layout {esc(run.layout)}; source status {esc(run.status)}; {declared}</p>'
    )
    comparisons = table(
        ["Comparison", "Declared by the source", "Observed in the files", "Result"],
        comparison_rows(run.comparisons),
    )
    file_rows = [
        [
            f"<code>{esc(f.name)}</code>",
            path_cell(f.local) if f.local else '<span class="colligo-bad">✗ no local file</span>',
            esc(f.linked_by),
            number(f.size),
            f'<code>{esc(f.md5)}</code>',
            (f'<span class="{CSS_CLASS[f.md5_state]}">{esc(SYMBOLS[f.md5_state])} {esc(f.md5_check)}</span>' if f.md5_check else ""),
        ]
        for f in run.files
    ]
    files = table(
        ["File at the source", "Local file", "Linked by", "Size at the source", "MD5 at the source", "MD5 check"],
        file_rows,
        numeric=(3,),
    )
    return head + comparisons + files


def source_section(view):
    source = view.source
    origin = ACCESSION_FROM.get(source.accession_from, "")
    heading = f"<p>Report <code>{esc(view.name)}</code>: source <code>{esc(source.id)}</code>, accession <code>{esc(source.accession)}</code>"
    if origin:
        heading += f' <span class="colligo-dim">({esc(origin)})</span>'
    heading += "</p>"
    body = "".join(run_block(run) for run in source.runs)
    if source.comparisons:
        body += "<h4>Source response</h4>" + table(
            ["Comparison", "Declared by the source", "Observed in the files", "Result"],
            comparison_rows(source.comparisons),
        )
    if not source.runs and not source.comparisons:
        body += '<p class="colligo-dim">The source returned no run.</p>'
    return heading + body


def general_stats(views):
    data = {}
    for view in views:
        data[view.name] = {
            "colligo_status": GENERAL_STATS_LABELS[view.state],
            "colligo_source": f"{view.source.id}:{view.source.accession}" if view.source else "– not compared",
            "colligo_failures": view.failures,
        }
    headers = {
        "colligo_status": {
            "title": "Colligo",
            "description": "Colligo result: only “✓ verified” means Colligo verified the files",
            "scale": False,
            "cond_formatting_rules": {
                "pass": [{"s_eq": GENERAL_STATS_LABELS[State.VERIFIED]}, {"s_eq": GENERAL_STATS_LABELS[State.VERIFIED_GAPS]}],
                "warn": [{"s_eq": GENERAL_STATS_LABELS[State.ADVISORY]}],
                "fail": [{"s_eq": GENERAL_STATS_LABELS[State.FAILED]}],
            },
            "cond_formatting_colours": [{"pass": "#2e7d32"}, {"warn": "#e0a100"}, {"fail": "#c62828"}],
        },
        "colligo_source": {
            "title": "Colligo source",
            "description": "The source the files were compared with; “– not compared” means only the files themselves were checked",
            "scale": False,
        },
        "colligo_failures": {
            "title": "Colligo failures",
            "description": "Blocking problems found by Colligo, each counted once",
            "min": 0,
            "scale": "Reds",
            "format": "{:,.0f}",
        },
    }
    return data, headers


def raw_data(views):
    return {
        view.name: {
            "state": WORDS[view.state],
            "exit_code": view.exit_code,
            "source": f"{view.source.id}:{view.source.accession}" if view.source else "",
            "files": len(view.files),
            "failures": view.failures,
            "advisories": view.advisories,
        }
        for view in views
    }


def raw_files(views):
    return {
        f"{view.name} | {f.path}": {
            "report": view.name,
            "path": f.path,
            "status": f.status,
            "state": WORDS[f.state],
            "reads": "" if f.reads is None else f.reads,
            "bases": "" if f.bases is None else f.bases,
        }
        for view in views
        for f in view.files
    }
