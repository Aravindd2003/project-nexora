import type { Request, Response } from "express";
import fetch from "node-fetch";
import { sendProblem } from "./problem.js";

/**
 * Forwards a request to an internal service, injecting verified tenant
 * context as headers. Any X-Tenant-ID the client itself sent is
 * deliberately overwritten here — tenant identity comes ONLY from
 * req.tenant, which was set by authMiddleware after validating the API
 * key. This is what "never trust tenant IDs provided directly in request
 * bodies" (spec 6.2) actually enforces in code.
 */
export async function proxyTo(baseUrl: string, targetPath: string) {
  return async (req: Request, res: Response) => {
    const tenant = req.tenant!;
    const url = `${baseUrl}${targetPath}`;

    const headers: Record<string, string> = {
      "Content-Type": "application/json",
      "X-Request-ID": req.requestId,
      "X-Tenant-ID": tenant.tenantId,
      "X-Monthly-Quota": JSON.stringify(tenant.monthlyQuota),
    };
    const idemKey = req.headers["idempotency-key"];
    if (typeof idemKey === "string") {
      headers["Idempotency-Key"] = idemKey;
    }

    try {
      const upstream = await fetch(url, {
        method: req.method,
        headers,
        body: ["GET", "HEAD"].includes(req.method) ? undefined : JSON.stringify(req.body),
      });

      const text = await upstream.text();
      res.status(upstream.status);
      res.setHeader("Content-Type", upstream.headers.get("content-type") || "application/json");
      res.send(text);
    } catch (err) {
      sendProblem(res, 503, "upstream_unavailable", `Failed to reach upstream service: ${(err as Error).message}`, req.requestId);
    }
  };
}
