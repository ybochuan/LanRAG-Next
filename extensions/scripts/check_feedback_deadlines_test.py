import unittest

from check_feedback_deadlines import check_answer


class FeedbackDeadlineTest(unittest.TestCase):
    def test_recognizes_reviewed_term_and_missing_rules_separately(self):
        self.assertEqual(
            check_answer("已审核期限为 5 个工作日。来源未规定周末和节假日处理，无法确定具体日期。", True),
            {"reviewed_term": True, "grounded_schedule": True},
        )
        self.assertEqual(
            check_answer("来源未规定具体日期。", True),
            {"reviewed_term": False, "grounded_schedule": True},
        )

    def test_rejects_historical_wrong_answers(self):
        for claim in (
            "如果第 5 个工作日恰逢周末，则提交期限顺延至下一个工作日。",
            "从活动结束后次日起算，顺延至第 5 个工作日。",
            "周末和法定节假日不计入期限内。",
            "第 1 个工作日为下周一。",
            "起算点：活动结束之日。",
        ):
            with self.subTest(claim=claim):
                self.assertEqual(
                    check_answer("期限为 5 个工作日。" + claim, False),
                    {"reviewed_term": True, "grounded_schedule": False},
                )

    def test_requires_uncertainty_when_question_requests_schedule(self):
        answer = "期限为五个工作日。"
        self.assertTrue(check_answer(answer, False)["grounded_schedule"])
        self.assertFalse(check_answer(answer, True)["grounded_schedule"])

    def test_rejects_old_term_as_current_answer_or_option(self):
        self.assertFalse(check_answer("提交期限是5个自然日。勘误写的是5个工作日。", False)["reviewed_term"])
        self.assertFalse(check_answer("5个工作日。按原文：5个自然日。", False)["reviewed_term"])
        self.assertTrue(check_answer("生效期限是5个工作日。原文曾写5个自然日。", False)["reviewed_term"])

    def test_preserves_relative_source_wording_and_explicit_uncertainty(self):
        self.assertTrue(check_answer("期限从活动结束后起算。", False)["grounded_schedule"])
        self.assertTrue(check_answer("没有写明具体从哪一天起算。", False)["grounded_schedule"])
        self.assertFalse(check_answer("期限从活动结束后次日起算。", False)["grounded_schedule"])
