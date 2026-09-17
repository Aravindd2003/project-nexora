"""AI Processing Worker main loop.

Consumes job envelopes from the Redis queue that Service 2 (Operations &
Processing) pushes to, fetches the full operation from Service 2, runs it
through the AI provider chain (Gemini -> OpenRouter fallback), and reports
the outcome back. The worker never talks to Postgres directly — Service 2
remains the single source of truth for operation state, which is what lets
multiple worker replicas run safely without coordinating with each other.
"""
import json
import os
import socket
import time
import redis

from providers import process_with_fallback, ProviderError
import operations_client as ops

REDIS_URL = os.environ.get("REDIS_URL", "redis://redis:6379")
MAIN_QUEUE_KEY = "nexora:queue:main"
WORKER_ID = f"worker-{socket.gethostname()}-{os.getpid()}"

BRPOP_TIMEOUT_SECONDS = 5


def connect_redis_with_retry(url: str, attempts: int = 10, delay: float = 2.0):
    last_err = None
    for i in range(attempts):
        try:
            client = redis.Redis.from_url(url, decode_responses=True)
            client.ping()
            return client
        except redis.RedisError as e:
            last_err = e
            print(f"worker: redis not ready (attempt {i+1}/{attempts}): {e}", flush=True)
            time.sleep(delay)
    raise last_err


def process_job(job: dict):
    operation_id = job["operation_id"]

    try:
        operation = ops.get_operation(operation_id)
    except Exception as e:
        print(f"worker: failed to fetch operation {operation_id}: {e}", flush=True)
        return

    if operation.get("status") in ("SUCCEEDED", "FAILED", "DEAD_LETTERED", "CANCELLED"):
        # Operation already reached a terminal state (e.g. customer
        # cancelled it after it was enqueued but before we picked it up).
        # Terminal Immutability means we must not process it further.
        print(f"worker: skipping {operation_id}, already terminal ({operation['status']})", flush=True)
        return

    attempt_number = operation.get("attempt_count", 0) + 1
    try:
        attempt_id = ops.start_attempt(operation_id, WORKER_ID, attempt_number)
    except Exception as e:
        print(f"worker: failed to start attempt for {operation_id}: {e}", flush=True)
        return

    op_type = operation["op_type"]
    input_payload = json.loads(operation["input"]) if isinstance(operation["input"], str) else operation["input"]

    try:
        output, provider, latency_ms = process_with_fallback(op_type, input_payload)
        ops.complete_attempt_success(operation_id, attempt_id, output, provider, latency_ms)
        print(f"worker: {operation_id} succeeded via {provider} ({latency_ms}ms)", flush=True)
    except ProviderError as e:
        ops.complete_attempt_failure(operation_id, attempt_id, e.category, str(e))
        print(f"worker: {operation_id} failed ({e.category}): {e}", flush=True)
    except Exception as e:
        # Unexpected worker-side crash mid-processing — treat as transient
        # so it gets a retry rather than silently vanishing.
        ops.complete_attempt_failure(operation_id, attempt_id, "transient", f"worker exception: {e}")
        print(f"worker: {operation_id} unexpected error: {e}", flush=True)


def main():
    print(f"worker: starting as {WORKER_ID}", flush=True)
    r = connect_redis_with_retry(REDIS_URL)
    print("worker: connected to redis, polling queue...", flush=True)

    while True:
        item = r.brpop(MAIN_QUEUE_KEY, timeout=BRPOP_TIMEOUT_SECONDS)
        if item is None:
            continue  # timeout elapsed, loop back (lets the process stay responsive to signals)
        _, raw_job = item
        try:
            job = json.loads(raw_job)
        except json.JSONDecodeError:
            print(f"worker: dropping malformed job envelope: {raw_job[:200]}", flush=True)
            continue
        process_job(job)


if __name__ == "__main__":
    main()
