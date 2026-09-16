"use client";
import { useEffect, useState } from "react";
import { useParams } from "next/navigation";
import { apiFetch, ApiError } from "@/lib/api";
import ErrorBanner from "../../components/ErrorBanner";
import StatusBadge from "../../components/StatusBadge";

interface Operation {
  id: string;
  tenant_id: string;
  op_type: string;
  status: string;
  input: unknown;
  output?: unknown;
  error_message?: string;
  attempt_count: number;
  max_retries: number;
  created_at: string;
  started_at?: string;
  completed_at?: string;
}

export default function OperationDetailPage() {
  const params = useParams();
  const id = params.id as string;
  const [op, setOp] = useState<Operation | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [cancelling, setCancelling] = useState(false);

  function load() {
    apiFetch(`/v1/operations/${id}`)
      .then(setOp)
      .catch((e) => setError(e instanceof ApiError ? e.message : "Failed to load operation"));
  }

  useEffect(() => {
    load();
    // Simple polling for real-time-ish updates (SSE/WebSocket bonus omitted
    // here for scope — see docs/architecture.md).
    const interval = setInterval(load, 3000);
    return () => clearInterval(interval);
  }, [id]);

  async function handleCancel() {
    setCancelling(true);
    try {
      await apiFetch(`/v1/operations/${id}/cancel`, { method: "POST" });
      load();
    } catch (e) {
      setError(e instanceof ApiError ? e.message : "Failed to cancel");
    } finally {
      setCancelling(false);
    }
  }

  if (error) return <ErrorBanner message={error} />;
  if (!op) return <p className="muted">Loading…</p>;

  const canCancel = ["PENDING", "QUEUED", "RUNNING", "RETRYING"].includes(op.status);

  return (
    <div>
      <h2>Operation {op.id.slice(0, 8)}…</h2>
      <div className="card">
        <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", marginBottom: 12 }}>
          <StatusBadge status={op.status} />
          {canCancel && (
            <button className="danger" onClick={handleCancel} disabled={cancelling}>
              {cancelling ? "Cancelling…" : "Cancel operation"}
            </button>
          )}
        </div>
        <table>
          <tbody>
            <tr><th>Type</th><td>{op.op_type}</td></tr>
            <tr><th>Attempts</th><td>{op.attempt_count} / {op.max_retries} max</td></tr>
            <tr><th>Created</th><td>{new Date(op.created_at).toLocaleString()}</td></tr>
            {op.started_at && <tr><th>Started</th><td>{new Date(op.started_at).toLocaleString()}</td></tr>}
            {op.completed_at && <tr><th>Completed</th><td>{new Date(op.completed_at).toLocaleString()}</td></tr>}
          </tbody>
        </table>
      </div>

      <div className="card">
        <h3 style={{ marginTop: 0 }}>Input</h3>
        <pre style={{ whiteSpace: "pre-wrap", fontSize: 13 }}>{JSON.stringify(op.input, null, 2)}</pre>
      </div>

      {op.output != null && (
        <div className="card">
          <h3 style={{ marginTop: 0 }}>Output</h3>
          <pre style={{ whiteSpace: "pre-wrap", fontSize: 13 }}>{JSON.stringify(op.output, null, 2)}</pre>
        </div>
      )}

      {op.error_message && (
        <div className="card">
          <h3 style={{ marginTop: 0 }}>Why it failed</h3>
          <p style={{ color: "var(--danger)" }}>{op.error_message}</p>
        </div>
      )}
    </div>
  );
}
