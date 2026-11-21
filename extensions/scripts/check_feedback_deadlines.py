"""Check reviewed deadline answers in a feedback-materials model run.

The fixture establishes five working days only. It supplies no counting start,
calendar, rollover rule or exact due date. This gate is specific to that fixture.
"""

import argparse
import json
import re
from pathlib import Path


DEADLINE = re.compile(r"(?:5|五)\s*个?\s*工作日")
OLD_DEADLINE = re.compile(r"(?:5|五)\s*个?\s*自然日")
TREAT_AS_OPTION = re.compile(r"按(?:制度)?原文\s*[：:]|(?:如果|若).{0,12}确认适用.{0,5}勘误")
SCHEDULE_SUBJECT = re.compile(r"周末|周[一二三四五六日天]|节假日|调休|起算|顺延|截止日期|最迟提交日期|具体日期")
UNSPECIFIED = re.compile(r"未(?:明确|载明|规定|说明|提供|给出|指定|写明)|没有(?:明确|规定|说明|提供|给出|载明|写明)|无(?:法确定|明文|依据|法核实)|不能(?:确定|推算|计算|判断)|需(?:要)?(?:另行|进一步)?(?:确认|核实)|以.{0,12}为准")
UNSUPPORTED = re.compile(
    r"(?:周末|周[六日天]|节假日).{0,14}(?:不计|不算|不纳入|排除|不含|包含|计入)|"
    r"(?:排除|跳过|不含).{0,8}(?:周末|节假日)|"
    r"(?:会|要|可|应|需|则|须|通常|将|自动|直接|就)\s*顺延|"
    r"顺延(?:至|到)|(?:从|自).{0,20}(?:起算|开始计算)|"
    r"起算(?:点|日)\s*[：:]\s*(?:活动结束(?:之日|当天|当日|次日)|当天|当日|次日)|"
    r"仅计算.{0,15}工作日|第\s*[一二三四五1-5]\s*个工作日.{0,16}为(?:下?周|[0-9])|"
    r"(?:20[0-9]{2}[-/])?[0-9]{1,2}月[0-9]{1,2}[日号]"
)
CLAUSE = re.compile(r"[^。！？；\n]+")
SOURCE_RESTATEMENT = re.compile(r"(?:从|自)[“「]?活动结束后[”」]?(?:起算|开始计算)")


def check_answer(answer, check_schedule):
    text = re.sub(r"<(?:kb|ref)\b[^>]*>", "", answer).replace("*", "")
    clauses = CLAUSE.findall(text)
    unsupported = any(
        UNSUPPORTED.search(SOURCE_RESTATEMENT.sub("", clause)) and not UNSPECIFIED.search(clause)
        for clause in clauses
    )
    acknowledges_unknown = any(
        SCHEDULE_SUBJECT.search(clause) and UNSPECIFIED.search(clause)
        for clause in clauses
    )
    current = DEADLINE.search(text)
    old = OLD_DEADLINE.search(text)
    return {
        "reviewed_term": current is not None and (old is None or current.start() < old.start())
        and not TREAT_AS_OPTION.search(text),
        "grounded_schedule": not unsupported and (not check_schedule or acknowledges_unknown),
    }


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("results", type=Path)
    parser.add_argument("--include-fixture-checks", action="store_true",
                        help="Regrade deadline checks against the saved answers and keep other fixture checks.")
    args = parser.parse_args()
    report = json.loads(args.results.read_text(encoding="utf-8"))
    original = {"quick-before-activation", "agent-cooling", "quick-withdrawn-same-session", "agent-withdrawn"}
    required = {f"quick-active-{i}" for i in range(1, 6)} | {"agent-active", "agent-reverse-active"} | original
    if any(case["case"] == "agent-autonomous-active" for case in report["cases"]):
        required.add("agent-autonomous-active")
    cases = {case["case"]: case for case in report["cases"] if case["case"] in required}
    results = {}
    for name in sorted(required):
        if name not in cases:
            results[name] = {"case_present": False}
            continue
        case = cases[name]
        if name in original:
            results[name] = {"grounded_schedule": check_answer(case["answer"], False)["grounded_schedule"]}
            continue
        if name == "quick-active-4":
            results[name] = {
                "grounded_other_clause": "财务共享中心" in case["answer"]
                and not DEADLINE.search(case["answer"])
                and case.get("correction_anywhere") is False,
            }
            continue
        results[name] = check_answer(case["answer"], bool(SCHEDULE_SUBJECT.search(case["question"])))
    checks = {}
    if args.include_fixture_checks:
        checks = dict(report["checks"])
        if not checks:
            parser.error("the saved run did not record any fixture checks")
        for name, result in results.items():
            for key, value in result.items():
                check = "grounded original schedule" if name in original and key == "grounded_schedule" else key
                if name + " " + check in checks:
                    checks[name + " " + check] = value
    print(json.dumps({"deadlines": results, "fixture_checks": checks}, indent=2))
    return 0 if all(all(result.values()) for result in results.values()) and all(checks.values()) else 1


if __name__ == "__main__":
    raise SystemExit(main())
