import logging
import os

from multiqc.base_module import BaseMultiqcModule, ModuleNoSamplesFound

from . import render
from .report import parse

log = logging.getLogger(__name__)

SUFFIX = ".colligo.json"


def report_name(filename):
    if filename.endswith(SUFFIX):
        return filename[: -len(SUFFIX)]
    return filename


def unique(name, root, taken):
    if name not in taken:
        return name
    candidate = f"{name} ({os.path.basename(os.path.normpath(root))})" if root else name
    counter = 2
    while candidate in taken:
        candidate = f"{name} ({counter})"
        counter += 1
    return candidate


class MultiqcModule(BaseMultiqcModule):
    def __init__(self):
        super().__init__(
            name="Colligo",
            anchor="colligo",
            href="https://github.com/colligo-go/colligo",
            info="checks that genomic files are the data they claim to be and agree with what the source published.",
        )

        views = []
        rejected = []
        taken = set()
        for f in self.find_log_files("colligo"):
            parsed = parse(f["fn"], f["f"])
            if parsed.rejected:
                log.warning("Colligo: %s: %s", parsed.rejected.name, parsed.rejected.reason)
                rejected.append(parsed.rejected)
                continue
            view = parsed.view
            view.name = unique(self.clean_s_name(report_name(f["fn"]), f), f.get("root", ""), taken)
            taken.add(view.name)
            self.add_data_source(f, view.name)
            views.append(view)

        if not views and not rejected:
            raise ModuleNoSamplesFound

        log.info("Found %d Colligo reports", len(views))
        self.add_software_version(None)

        if views:
            self.write_data_file(render.raw_data(views), "multiqc_colligo")
            data, headers = render.general_stats(views)
            self.general_stats_addcols(data, headers)

        self.add_section(
            name="Files",
            anchor="colligo-files",
            description="What `colligo check` concluded about each file.",
            content=render.overview(views, rejected) + (render.files_table(views) if views else "") + render.report_notes(views),
        )

        issues = render.issues_table(views)
        if issues:
            self.add_section(
                name="Problems",
                anchor="colligo-problems",
                description="Each problem once, with the files it applies to.",
                content=issues,
            )

        not_checked = render.not_checked_table(views)
        if not_checked:
            self.add_section(
                name="Not checked",
                anchor="colligo-not-checked",
                description="What Colligo did not compare. These are never shown as passed.",
                content=not_checked,
            )

        for index, view in enumerate(v for v in views if v.source):
            self.add_section(
                name=f"Source: {view.source.accession}",
                anchor=f"colligo-source-{index + 1}",
                description="The files compared with what the source declares.",
                content=render.source_section(view),
            )
