"use client";
import { useEffect, useState } from "react";
import { apiFetch, ApiError, getApiKey } from "@/lib/api";
import ErrorBanner from "./components/ErrorBanner";
import StatusBadge from "./components/StatusBadge";
import Link from "next/link";

interface UsageSummary {
  total_operations: number;
  units_consumed: number;
  monthly_quota: number;
  quota_remaining: number;
}
interface Operation {
  id: string;
  op_type: string;
  status: string;
  created_at: string;
}

export default function OverviewPage() {
  const [usage, setUsage] = useState<UsageSummary | null>(null);
  const [recent, setRecent] = useState<Operation[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    if (!getApiKey()) {
      setLoading(false);
      return;
    }
    Promise.all([apiFetch("/v1/usage"), apiFetch("/v1/operations?limit=8")])
      .then(([usageData, opsData]) => {
        setUsage(usageData);
        setRecent(opsData.operations || []);
      })
      .catch((e) => setError(e instanceof ApiError ? e.message : "Failed to load overview"))
      .finally(() => setLoading(false));
  }, []);

  if (!getApiKey()) {
    return (
      <div>
        <h2>Welcome to Nexora</h2>
        <p className="muted">
          You haven't added an API key yet. Head to the{" "}
          <Link href="/api-keys" style={{ color: "var(--accent)" }}>
            API Keys page
          </Link>{" "}
          to create one and get started.
        </p>
      </div>
    );
  }

  return (
    <div>
      <h2>Overview</h2>
      <ErrorBanner message={error} />
      {loading && <p className="muted">Loading…</p>}

      {usage && (
        <div className="grid" style={{ marginBottom: 24 }}>
          <div className="card">
            <div className="stat-label">Operations this month</div>
            <div className="stat-value">{usage.total_operations}</div>
          </div>
          <div className="card">
            <div className="stat-label">Units consumed</div>
            <div className="stat-value">{usage.units_consumed}</div>
          </div>
          <div className="card">
            <div className="stat-label">Quota remaining</div>
            <div className="stat-value">{usage.quota_remaining}</div>
            <div className="muted">of {usage.monthly_quota}</div>
          </div>
        </div>
      )}

      <div className="card">
        <h3 style={{ marginTop: 0 }}>Recent operations</h3>
        {recent.length === 0 ? (
          <p className="muted">No operations yet — submit one via the API to see it here.</p>
        ) : (
          <table>
            <thead>
              <tr>
                <th>ID</th>
                <th>Type</th>
                <th>Status</th>
                <th>Created</th>
              </tr>
            </thead>
            <tbody>
              {recent.map((op) => (
                <tr key={op.id}>
                  <td>
                    <Link href={`/operations/${op.id}`} style={{ color: "var(--accent)" }}>
                      {op.id.slice(0, 8)}…
                    </Link>
                  </td>
                  <td>{op.op_type}</td>
                  <td><StatusBadge status={op.status} /></td>
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
