"use client";
import { useEffect, useState } from "react";
import { apiFetch, ApiError, getApiKey, setApiKey, clearApiKey } from "@/lib/api";
import ErrorBanner from "../components/ErrorBanner";

interface ApiKey {
  id: string;
  key_prefix: string;
  status: string;
  created_at: string;
}

export default function ApiKeysPage() {
  const [hasKey, setHasKey] = useState(false);
  const [manualKey, setManualKey] = useState("");
  const [keys, setKeys] = useState<ApiKey[]>([]);
  const [newRawKey, setNewRawKey] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [creating, setCreating] = useState(false);

  useEffect(() => {
    setHasKey(!!getApiKey());
  }, []);

  function loadKeys() {
    apiFetch("/v1/api-keys")
      .then((data) => setKeys(data.api_keys || []))
      .catch((e) => setError(e instanceof ApiError ? e.message : "Failed to load keys"));
  }

  useEffect(() => {
    if (hasKey) loadKeys();
  }, [hasKey]);

  function handleUseExisting() {
    if (!manualKey.trim()) return;
    setApiKey(manualKey.trim());
    setHasKey(true);
    setManualKey("");
  }

  async function handleCreate() {
    setCreating(true);
    setError(null);
    try {
      const result = await apiFetch("/v1/api-keys", { method: "POST" });
      setNewRawKey(result.raw_key);
      if (!hasKey) {
        setApiKey(result.raw_key);
        setHasKey(true);
      }
      loadKeys();
    } catch (e) {
      setError(e instanceof ApiError ? e.message : "Failed to create key");
    } finally {
      setCreating(false);
    }
  }

  async function handleRevoke(id: string) {
    try {
      await apiFetch(`/v1/api-keys/${id}`, { method: "DELETE" });
      loadKeys();
    } catch (e) {
      setError(e instanceof ApiError ? e.message : "Failed to revoke key");
    }
  }

  return (
    <div>
      <h2>API Keys</h2>
      <ErrorBanner message={error} />

      {!hasKey && (
        <div className="card">
          <h3 style={{ marginTop: 0 }}>Get started</h3>
          <p className="muted">
            You need a tenant + API key to use this dashboard. Create a tenant via the Admin
            Dashboard first, then either paste an existing key below or create a new one here
            (requires an existing key to authenticate the create call — for first-time setup,
            use the seed script in <code>docs/setup.md</code>).
          </p>
          <div style={{ display: "flex", gap: 8 }}>
            <input
              placeholder="Paste an API key…"
              value={manualKey}
              onChange={(e) => setManualKey(e.target.value)}
              style={{ flex: 1 }}
            />
            <button onClick={handleUseExisting}>Use this key</button>
          </div>
        </div>
      )}

      {hasKey && (
        <>
          <div className="card">
            <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center" }}>
              <div>
                <h3 style={{ margin: 0 }}>Your keys</h3>
                <p className="muted" style={{ marginTop: 4 }}>Secret values are never shown again after creation.</p>
              </div>
              <div style={{ display: "flex", gap: 8 }}>
                <button onClick={handleCreate} disabled={creating}>
                  {creating ? "Creating…" : "+ New API key"}
                </button>
                <button className="secondary" onClick={() => { clearApiKey(); setHasKey(false); setKeys([]); }}>
                  Sign out
                </button>
              </div>
            </div>

            {newRawKey && (
              <div className="raw-key-box">
                <strong>Save this now — it won't be shown again:</strong>
                <div style={{ marginTop: 6 }}>{newRawKey}</div>
              </div>
            )}

            <table style={{ marginTop: 16 }}>
              <thead>
                <tr><th>Prefix</th><th>Status</th><th>Created</th><th></th></tr>
              </thead>
              <tbody>
                {keys.map((k) => (
                  <tr key={k.id}>
                    <td><code>{k.key_prefix}…</code></td>
                    <td>{k.status}</td>
                    <td className="muted">{new Date(k.created_at).toLocaleString()}</td>
                    <td>
                      {k.status === "active" && (
                        <button className="danger" onClick={() => handleRevoke(k.id)}>Revoke</button>
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </>
      )}
    </div>
  );
}
