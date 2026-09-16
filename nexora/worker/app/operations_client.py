"""Thin HTTP client for the worker's calls back into Service 2. Kept as a
separate module so main.py's control flow reads as "claim job -> start
attempt -> do work -> complete attempt" without HTTP plumbing mixed in.
"""
import os
import requests

OPERATIONS_URL = os.environ.get("OPERATIONS_URL", "http://operations:8082")


def get_operation(operation_id: str) -> dict:
    resp = requests.get(f"{OPERATIONS_URL}/internal/operations/{operation_id}", timeout=10)
    resp.raise_for_status()
    return resp.json()


def start_attempt(operation_id: str, worker_id: str, attempt_number: int) -> str:
    resp = requests.post(
        f"{OPERATIONS_URL}/internal/operations/{operation_id}/attempts/start",
        json={"worker_id": worker_id, "attempt_number": attempt_number},
        timeout=10,
    )
    resp.raise_for_status()
    return resp.json()["attempt_id"]


def complete_attempt_success(operation_id: str, attempt_id: str, output: dict, provider: str, latency_ms: int):
    resp = requests.post(
        f"{OPERATIONS_URL}/internal/operations/{operation_id}/attempts/complete",
        json={
            "attempt_id": attempt_id,
            "success": True,
            "output": output,
            "provider": provider,
            "latency_ms": latency_ms,
        },
        timeout=10,
    )
    resp.raise_for_status()


def complete_attempt_failure(operation_id: str, attempt_id: str, error_category: str, error_detail: str):
    resp = requests.post(
        f"{OPERATIONS_URL}/internal/operations/{operation_id}/attempts/complete",
        json={
            "attempt_id": attempt_id,
            "success": False,
            "error_category": error_category,
            "error_detail": error_detail[:2000],  # avoid unbounded payloads
        },
        timeout=10,
    )
    resp.raise_for_status()
