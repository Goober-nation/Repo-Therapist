"""Project internal grading details into the compact Documentation wire payload."""

from .contracts import validate_documentation_payload
from .grading import CHECKS


def documentation_payload(result: dict) -> dict:
    facts = []
    for check in result["checks"]:
        for deduction in check["deductions"]:
            facts.append({
                "id": f"DOC-{len(facts) + 1:04d}",
                "subcategory": CHECKS[check["check_id"]].group,
                "code": deduction["code"],
                "facts": deduction["facts"],
                "evidence": deduction["evidence"],
            })
    payload = {
        "grade": result["score"]["value"],
        "subcategories": [{"id": item["id"], "grade": item["score"]}
                          for item in result["subcategories"]],
        "facts": facts,
    }
    validate_documentation_payload(payload)
    return payload
