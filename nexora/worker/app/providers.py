"""AI provider abstraction with primary/fallback support.

Only Gemini and OpenRouter are implemented, both against free tiers, per
the assessment's "no paid AI subscription" constraint. Each provider raises
a ProviderError tagged with a category so the caller (worker main loop) can
decide retry behavior without knowing provider-specific error shapes.
"""
import json
import os
import time
import requests


class ProviderError(Exception):
    def __init__(self, message: str, category: str):
        # category: "transient" (retry makes sense) or "permanent" (it won't)
        super().__init__(message)
        self.category = category


SYSTEM_PROMPT = (
    "You are a backend AI processing worker for the Nexora platform. "
    "You will be given an operation type and input, and must return ONLY "
    "a single valid JSON object with your result — no prose, no markdown "
    "fences, nothing else. If op_type is 'classify', return "
    '{"label": string, "confidence": number}. If op_type is "summarize", '
    'return {"summary": string}. Otherwise return {"result": string} with '
    "your best-effort response to the input."
)


def _build_prompt(op_type: str, input_payload: dict) -> str:
    return (
        f"op_type: {op_type}\n"
        f"input: {json.dumps(input_payload)}\n\n"
        "Respond with only the JSON object described in your instructions."
    )


def _extract_json(text: str) -> dict:
    text = text.strip()
    # Models sometimes wrap output in ```json fences despite instructions —
    # strip those defensively rather than failing the whole operation.
    if text.startswith("```"):
        text = text.strip("`")
        if text.lower().startswith("json"):
            text = text[4:]
        text = text.strip()
    try:
        return json.loads(text)
    except json.JSONDecodeError as e:
        raise ProviderError(f"model returned non-JSON output: {e}", "permanent")


class GeminiProvider:
    name = "gemini"

    def __init__(self):
        self.api_key = os.environ.get("GEMINI_API_KEY", "")
        self.model = os.environ.get("GEMINI_MODEL", "gemini-2.0-flash")

    def available(self) -> bool:
        return bool(self.api_key)

    def process(self, op_type: str, input_payload: dict) -> dict:
        url = (
            f"https://generativelanguage.googleapis.com/v1beta/models/"
            f"{self.model}:generateContent?key={self.api_key}"
        )
        body = {
            "systemInstruction": {"parts": [{"text": SYSTEM_PROMPT}]},
            "contents": [{"parts": [{"text": _build_prompt(op_type, input_payload)}]}],
            "generationConfig": {"temperature": 0.2},
        }
        try:
            resp = requests.post(url, json=body, timeout=20)
        except requests.RequestException as e:
            raise ProviderError(f"gemini network error: {e}", "transient")

        if resp.status_code == 429 or resp.status_code >= 500:
            raise ProviderError(f"gemini transient error: HTTP {resp.status_code}", "transient")
        if resp.status_code >= 400:
            raise ProviderError(f"gemini request error: HTTP {resp.status_code} {resp.text[:200]}", "permanent")

        data = resp.json()
        try:
            text = data["candidates"][0]["content"]["parts"][0]["text"]
        except (KeyError, IndexError) as e:
            raise ProviderError(f"unexpected gemini response shape: {e}", "transient")

        return _extract_json(text)


class OpenRouterProvider:
    name = "openrouter"

    def __init__(self):
        self.api_key = os.environ.get("OPENROUTER_API_KEY", "")
        self.model = os.environ.get("OPENROUTER_MODEL", "meta-llama/llama-3.1-8b-instruct:free")

    def available(self) -> bool:
        return bool(self.api_key)

    def process(self, op_type: str, input_payload: dict) -> dict:
        url = "https://openrouter.ai/api/v1/chat/completions"
        headers = {"Authorization": f"Bearer {self.api_key}", "Content-Type": "application/json"}
        body = {
            "model": self.model,
            "messages": [
                {"role": "system", "content": SYSTEM_PROMPT},
                {"role": "user", "content": _build_prompt(op_type, input_payload)},
            ],
            "temperature": 0.2,
        }
        try:
            resp = requests.post(url, headers=headers, json=body, timeout=20)
        except requests.RequestException as e:
            raise ProviderError(f"openrouter network error: {e}", "transient")

        if resp.status_code == 429 or resp.status_code >= 500:
            raise ProviderError(f"openrouter transient error: HTTP {resp.status_code}", "transient")
        if resp.status_code >= 400:
            raise ProviderError(f"openrouter request error: HTTP {resp.status_code} {resp.text[:200]}", "permanent")

        data = resp.json()
        try:
            text = data["choices"][0]["message"]["content"]
        except (KeyError, IndexError) as e:
            raise ProviderError(f"unexpected openrouter response shape: {e}", "transient")

        return _extract_json(text)


def process_with_fallback(op_type: str, input_payload: dict):
    """Tries the primary provider (Gemini), falling back to OpenRouter only
    on transient/availability failures. A permanent error (e.g. malformed
    output) from the primary is NOT retried against the fallback — if the
    input itself is the problem, switching providers won't help, and doing
    so would mask a genuine input validation issue as a provider issue.
    Returns (output_dict, provider_name_used, latency_ms).
    """
    providers = [GeminiProvider(), OpenRouterProvider()]
    last_err = None

    for provider in providers:
        if not provider.available():
            continue
        start = time.monotonic()
        try:
            output = provider.process(op_type, input_payload)
            latency_ms = int((time.monotonic() - start) * 1000)
            return output, provider.name, latency_ms
        except ProviderError as e:
            last_err = e
            if e.category == "permanent":
                raise  # don't waste the fallback on a bad input
            continue  # transient — try next provider

    if last_err:
        raise last_err
    raise ProviderError("no AI provider configured (set GEMINI_API_KEY or OPENROUTER_API_KEY)", "permanent")
