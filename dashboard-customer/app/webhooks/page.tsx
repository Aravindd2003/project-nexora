"use client";
import { useEffect, useState } from "react";
import { apiFetch, ApiError, getApiKey } from "@/lib/api";
import ErrorBanner from "../components/ErrorBanner";

interface Webhook {
  id: string;
  url: string;
  is_active: boolean;
  created_at: string;
}

export default function WebhooksPage() {
  const [hooks, setHooks] = useState<Webhook[]>([]);
  const [url, setUrl] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [newSecret, setNewSecret] = useState<string | null>(null);
  const [creating, setCreating] = useState(false);

  function load() {
    if (!getApiKey()) return;
    apiFetch("/v1/webhooks")
      .then((data) => setHooks(data.webhooks || []))
      .catch((e) => setError(e instanceof ApiError ? e.message : "Failed to load webhooks"));
  }

  useEffect(load, []);

  async function handleCreate() {
    if (!url.trim()) return;
    setCreating(true);
    try {
      const result = await apiFetch("/v1/webhooks", { method: "POST", body: JSON.stringify({ url: url.trim() }) });
      setNewSecret(result.secret);
      setUrl("");
      load();
    } catch (e) {
      setError(e instanceof ApiError ? e.message : "Failed to create webhook");
    } finally {
      setCreating(false);
    }
  }

  if (!getApiKey()) {
    return <p className="muted">Add an API key on the API Keys page first.</p>;
  }

  return (
    <div>
      <h2>Webhooks</h2>
      <ErrorBanner message={error} />

      <div className="card">
        <h3 style={{ marginTop: 0 }}>Register an endpoint</h3>
        <p className="muted">
          Nexora delivers <code>operation.succeeded</code>, <code>operation.failed</code>, and{" "}
          <code>operation.dead_lettered</code> events here, signed with HMAC-SHA256.
        </p>
        <div style={{ display: "flex", gap: 8 }}>
          <input
            placeholder="https://your-app.example.com/webhooks/nexora"
            value={url}
            onChange={(e) => setUrl(e.target.value)}
            style={{ flex: 1 }}
          />
          <button onClick={handleCreate} disabled={creating}>{creating ? "Adding…" : "Add endpoint"}</button>
        </div>
        {newSecret && (
          <div className="raw-key-box">
            <strong>Signing secret (save this now):</strong>
            <div style={{ marginTop: 6 }}>{newSecret}</div>
          </div>
        )}
      </div>

      <div className="card">
        <h3 style={{ marginTop: 0 }}>Registered endpoints</h3>
        {hooks.length === 0 ? (
          <p className="muted">No webhook endpoints yet.</p>
        ) : (
          <table>
            <thead><tr><th>URL</th><th>Status</th><th>Created</th></tr></thead>
            <tbody>
              {hooks.map((h) => (
                <tr key={h.id}>
                  <td>{h.url}</td>
                  <td>{h.is_active ? "Active" : "Inactive"}</td>
                  <td className="muted">{new Date(h.created_at).toLocaleString()}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
    </div>
  );
}
