"use client";
import { useEffect, useState } from "react";

const API_BASE = process.env.NEXT_PUBLIC_API_BASE || "http://localhost:8080";

interface Operation {
  id: string;
  tenant_id: string;
  op_type: string;
  status: string;
  error_message?: string;
  attempt_count: number;
  max_retries: number;
  created_at: string;
}

interface Attempt {
  id: string;
  attempt_number: number;
  worker_id?: string;
  provider_used?: string;
  success?: boolean;
  error_category?: string;
  error_detail?: string;
  started_at: string;
  finished_at?: string;
}

interface Tenant {
  id: string;
  name: string;
}

// The Admin Dashboard is unauthenticated by design here — see the Gateway's
// /admin/* routes and docs/architecture.md for how this would be locked
// down with an operator credential in a real deployment. It is deliberately
// out of the customer-facing navigation flow (no sidebar link) since real
// operators would reach it via a separate internal URL, not by clicking
// around the customer app.
export default function AdminDashboard() {
  const [summary, setSummary] = useState<Record<string, number>>({});
  const [tenants, setTenants] = useState<Record<string, string>>({});
  const [problemOps, setProblemOps] = useState<Operation[]>([]);
  const [expandedId, setExpandedId] = useState<string | null>(null);
  const [attempts, setAttempts] = useState<Attempt[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);

  function load() {
    Promise.all([
      fetch(`${API_BASE}/admin/summary`).then((r) => r.json()),
      fetch(`${API_BASE}/admin/tenants`).then((r) => r.json()),
      fetch(`${API_BASE}/admin/operations?status=FAILED&limit=25`).then((r) => r.json()),
      fetch(`${API_BASE}/admin/operations?status=DEAD_LETTERED&limit=25`).then((r) => r.json()),
    ])
      .then(([summaryData, tenantsData, failedData, deadData]) => {
        setSummary(summaryData.status_counts || {});
        const lookup: Record<string, string> = {};
        for (const t of (tenantsData.tenants || []) as Tenant[]) {
          lookup[t.id] = t.name;
        }
        setTenants(lookup);
        const combined = [...(failedData.operations || []), ...(deadData.operations || [])];
        combined.sort((a, b) => new Date(b.created_at).getTime() - new Date(a.created_at).getTime());
        setProblemOps(combined);
      })
      .catch(() => setError("Failed to load admin data — is the Gateway reachable?"))
      .finally(() => setLoading(false));
  }

  useEffect(() => {
    load();
    const interval = setInterval(load, 10000);
    return () => clearInterval(interval);
  }, []);

  async function toggleExpand(op: Operation) {
    if (expandedId === op.id) {
      setExpandedId(null);
      return;
    }
    setExpandedId(op.id);
    try {
      const res = await fetch(`${API_BASE}/admin/operations/${op.id}/attempts`);
      const data = await res.json();
      setAttempts(data.attempts || []);
    } catch {
      setAttempts([]);
    }
  }

  const totalOps = Object.values(summary).reduce((a, b) => a + b, 0);
  const statusOrder = ["PENDING", "QUEUED", "RUNNING", "RETRYING", "SUCCEEDED", "FAILED", "DEAD_LETTERED", "CANCELLED"];

  return (
    <div style={{ minHeight: "100vh", margin: 0, background: "#0b0d12", color: "#e6e8ec", fontFamily: "-apple-system, sans-serif", padding: "32px 40px" }}>
      <h1 style={{ marginTop: 0 }}>⚙️ Nexora Admin</h1>
      <p style={{ color: "#8b909c", fontSize: 13, marginTop: -8 }}>
        Platform-wide operator view — no tenant scoping. Not part of the customer-facing app.
      </p>

      {error && <div style={{ background: "rgba(224,82,79,0.1)", color: "#e0524f", padding: 12, borderRadius: 8 }}>{error}</div>}
      {loading && <p style={{ color: "#8b909c" }}>Loading…</p>}

      <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fit, minmax(140px, 1fr))", gap: 12, margin: "24px 0" }}>
        <div style={statCard}>
          <div style={statLabel}>Total tenants</div>
          <div style={statValue}>{Object.keys(tenants).length}</div>
        </div>
        <div style={statCard}>
          <div style={statLabel}>Total operations</div>
          <div style={statValue}>{totalOps}</div>
        </div>
        {statusOrder.map((s) =>
          summary[s] ? (
            <div style={statCard} key={s}>
              <div style={statLabel}>{s}</div>
              <div style={statValue}>{summary[s]}</div>
            </div>
          ) : null
        )}
      </div>

      <div style={card}>
        <h3 style={{ marginTop: 0 }}>Failed &amp; dead-lettered operations</h3>
        <p style={{ color: "#8b909c", fontSize: 13 }}>
          Click a row to trace it: customer → operation → attempt → failure detail.
        </p>
        {problemOps.length === 0 ? (
          <p style={{ color: "#8b909c" }}>No failed or dead-lettered operations — platform healthy.</p>
        ) : (
          <table style={{ width: "100%", borderCollapse: "collapse", fontSize: 14 }}>
            <thead>
              <tr>
                <th style={th}>Customer</th>
                <th style={th}>Operation</th>
                <th style={th}>Type</th>
                <th style={th}>Status</th>
                <th style={th}>Attempts</th>
                <th style={th}>Created</th>
              </tr>
            </thead>
            <tbody>
              {problemOps.map((op) => (
                <>
                  <tr key={op.id} onClick={() => toggleExpand(op)} style={{ cursor: "pointer" }}>
                    <td style={td}>{tenants[op.tenant_id] || op.tenant_id.slice(0, 8) + "…"}</td>
                    <td style={td}>{op.id.slice(0, 8)}…</td>
                    <td style={td}>{op.op_type}</td>
                    <td style={td}>
                      <span style={{ color: op.status === "DEAD_LETTERED" ? "#e0524f" : "#e0a622" }}>{op.status}</span>
                    </td>
                    <td style={td}>{op.attempt_count} / {op.max_retries}</td>
                    <td style={{ ...td, color: "#8b909c" }}>{new Date(op.created_at).toLocaleString()}</td>
                  </tr>
                  {expandedId === op.id && (
                    <tr key={op.id + "-detail"}>
                      <td colSpan={6} style={{ background: "#12151c", padding: 16 }}>
                        <div style={{ marginBottom: 8 }}>
                          <strong>Error:</strong> {op.error_message || "—"}
                        </div>
                        <strong>Attempt history:</strong>
                        {attempts.length === 0 ? (
                          <p style={{ color: "#8b909c" }}>No attempts recorded.</p>
                        ) : (
                          <table style={{ width: "100%", marginTop: 8, fontSize: 13 }}>
                            <thead>
                              <tr>
                                <th style={th}>#</th>
                                <th style={th}>Worker</th>
                                <th style={th}>Provider</th>
                                <th style={th}>Result</th>
                                <th style={th}>Category</th>
                                <th style={th}>Detail</th>
                              </tr>
                            </thead>
                            <tbody>
                              {attempts.map((a) => (
                                <tr key={a.id}>
                                  <td style={td}>{a.attempt_number}</td>
                                  <td style={td}>{a.worker_id || "—"}</td>
                                  <td style={td}>{a.provider_used || "—"}</td>
                                  <td style={td}>{a.success === true ? "success" : a.success === false ? "failed" : "in progress"}</td>
                                  <td style={td}>{a.error_category || "—"}</td>
                                  <td style={td}>{a.error_detail || "—"}</td>
                                </tr>
                              ))}
                            </tbody>
                          </table>
                        )}
                      </td>
                    </tr>
                  )}
                </>
              ))}
            </tbody>
          </table>
        )}
      </div>
    </div>
  );
}

const statCard: React.CSSProperties = { background: "#12151c", border: "1px solid #262b36", borderRadius: 10, padding: 16 };
const statLabel: React.CSSProperties = { color: "#8b909c", fontSize: 12 };
const statValue: React.CSSProperties = { fontSize: 22, fontWeight: 600 };
const card: React.CSSProperties = { background: "#12151c", border: "1px solid #262b36", borderRadius: 12, padding: 20 };
const th: React.CSSProperties = { textAlign: "left", color: "#8b909c", fontWeight: 500, padding: "8px 10px", borderBottom: "1px solid #262b36" };
const td: React.CSSProperties = { padding: "8px 10px", borderBottom: "1px solid #262b36" };
