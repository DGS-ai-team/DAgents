"""案例资源引用校验。"""

from __future__ import annotations

from fastapi import HTTPException

from manage.cases.models import CaseResources
from manage.externaltools.store import ExternalToolPackageStore
from manage.plugins.store import PluginPackageStore
from manage.skills.store import SkillPackageStore


def validate_case_resources(
    resources: CaseResources,
    *,
    skills_store: SkillPackageStore | None,
    plugins_store: PluginPackageStore | None,
    externaltools_store: ExternalToolPackageStore | None,
) -> None:
    if skills_store and resources.skill_ids:
        known = skills_store.published_ids()
        missing = [sid for sid in resources.skill_ids if sid not in known]
        if missing:
            raise HTTPException(
                status_code=422,
                detail=f"unknown skill_ids (not in published catalog): {', '.join(missing)}",
            )
    if plugins_store and resources.plugin_ids:
        known = plugins_store.published_ids()
        missing = [pid for pid in resources.plugin_ids if pid not in known]
        if missing:
            raise HTTPException(
                status_code=422,
                detail=f"unknown plugin_ids (not in published catalog): {', '.join(missing)}",
            )
    if externaltools_store and resources.externaltool_ids:
        known = externaltools_store.published_ids()
        missing = [tid for tid in resources.externaltool_ids if tid not in known]
        if missing:
            raise HTTPException(
                status_code=422,
                detail=f"unknown externaltool_ids (not in published catalog): {', '.join(missing)}",
            )
