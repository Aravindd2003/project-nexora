"use client";
import { useEffect, useState } from "react";
import Link from "next/link";
import { apiFetch, ApiError, getApiKey } from "@/lib/api";
import ErrorBanner from "../components/ErrorBanner";
import StatusBadge from "../components/StatusBadge";

interface Operation {
  id: string;
  op_type: string;
  status: string;
  created_at: string;
  attempt_count: number;
}

const STATUS_OPTIONS = ["", "PENDING", "QUEUED", "RUNNING", "RETRYING", "SUCCEEDED", "FAILED", "DEAD_LETTERED", "CANCELLED"];

export default function OperationsPage() {
  const [ops, setOps] = useState<Operation[]>([]);
  const [status, setStatus] = useState("");
  const [search, setSearch] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);

  function load() {
    if (!getApiKey()) {
      setLoading(false);
      return;
    }
    setLoading(true);
    const qs = status ? `?status=${status}&limit=50` : "?limit=50";
    apiFetch(`/v1/operations${qs}`)
      .then((data) => setOps(data.operations || []))
      .catch((e) => setError(e instanceof ApiError ? e.message : "Failed to load operations"))
      .finally(() => setLoading(false));
  }

  useEffect(load, [status]);

  const filtered = search
    ? ops.filter((o) => o.id.includes(search) || o.op_type.includes(search))
    : ops;

  if (!getApiKey()) {
    return <p className="muted">Add an API key on the API Keys page to view operations.</p>;
  }

  return (
    <div>
      <h2>Operations</h2>
      <ErrorBanner message={error} />

      <div style={{ display: "flex", gap: 12, marginBottom: 16 }}>
        <select value={status} onChange={(e) => setStatus(e.target.value)}>
          {STATUS_OPTIONS.map((s) => (
            <option key={s} value={s}>{s || "All statuses"}</option>
          ))}
        </select>
        <input
          placeholder="Search by ID or type…"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          style={{ flex: 1 }}
        />
        <button className="secondary" onClick={load}>Refresh</button>
      </div>

      <div className="card">
        {loading ? (
          <p className="muted">Loading…</p>
        ) : filtered.length === 0 ? (
          <p className="muted">No operations match this filter.</p>
        ) : (
          <table>
            <thead>
              <tr>
                <th>ID</th><th>Type</th><th>Status</th><th>Attempts</th><th>Created</th>
              </tr>
            </thead>
            <tbody>
              {filtered.map((op) => (
                <tr key={op.id}>
                  <td><Link href={`/operations/${op.id}`} style={{ color: "var(--accent)" }}>{op.id.slice(0, 8)}…</Link></td>
                  <td>{op.op_type}</td>
                  <td><StatusBadge status={op.status} /></td>
                  <td>{op.attempt_count}</td>
                  <td className="muted">{new Date(op.created_at).toLocaleString()}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
    </div>
  );
}
