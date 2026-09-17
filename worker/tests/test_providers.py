"""Unit tests for the worker's AI-provider layer.

These test pure logic only (JSON extraction, error classification) with no
network calls, so they run instantly and need no live AI provider or API
key — appropriate for the parts of provider.py that don't depend on an
external service.

Run with:  python -m unittest discover -s tests
"""
import json
import os
import sys
import unittest

sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "app"))

from providers import _extract_json, ProviderError  # noqa: E402


class TestExtractJson(unittest.TestCase):
    def test_plain_json_object(self):
        result = _extract_json('{"label": "billing", "confidence": 0.9}')
        self.assertEqual(result, {"label": "billing", "confidence": 0.9})

    def test_json_wrapped_in_markdown_fence(self):
        # Models sometimes ignore the "no markdown fences" instruction —
        # this must be handled gracefully rather than failing the operation.
        text = '```json\n{"label": "billing"}\n```'
        result = _extract_json(text)
        self.assertEqual(result, {"label": "billing"})

    def test_json_wrapped_in_plain_fence(self):
        text = '```\n{"label": "billing"}\n```'
        result = _extract_json(text)
        self.assertEqual(result, {"label": "billing"})

    def test_json_with_surrounding_whitespace(self):
        result = _extract_json('  \n  {"label": "billing"}  \n  ')
        self.assertEqual(result, {"label": "billing"})

    def test_invalid_json_raises_permanent_provider_error(self):
        # Malformed model output is a "permanent" failure category: retrying
        # against a different provider or re-attempting won't fix output the
        # model itself produced incorrectly for this input.
        with self.assertRaises(ProviderError) as ctx:
            _extract_json("this is not json at all")
        self.assertEqual(ctx.exception.category, "permanent")

    def test_empty_string_raises_permanent_provider_error(self):
        with self.assertRaises(ProviderError) as ctx:
            _extract_json("")
        self.assertEqual(ctx.exception.category, "permanent")


class TestProviderErrorClassification(unittest.TestCase):
    """These document and verify the retry-eligibility contract that the
    Operations service depends on: only "transient" errors get retried
    (see services/operations completeAttempt handler), so provider code
    must classify errors correctly or operations will either retry
    hopelessly against permanent failures, or give up too early on
    recoverable ones.
    """

    def test_provider_error_carries_category(self):
        err = ProviderError("simulated timeout", "transient")
        self.assertEqual(str(err), "simulated timeout")
        self.assertEqual(err.category, "transient")

    def test_permanent_category_is_distinct_from_transient(self):
        transient = ProviderError("rate limited", "transient")
        permanent = ProviderError("bad request", "permanent")
        self.assertNotEqual(transient.category, permanent.category)


if __name__ == "__main__":
    unittest.main()
