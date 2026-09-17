import type { Request, Response, NextFunction } from "express";
import fetch from "node-fetch";
import type { Redis } from "ioredis";
import { sendProblem } from "../problem.js";

const CUSTOMER_ACCESS_URL = process.env.CUSTOMER_ACCESS_URL!;
const VALIDATION_CACHE_TTL_SECONDS = 30;

interface ValidateResponse {
  tenant_id: string;
  tier: string;
  rate_limit_rpm: number;
  monthly_quota: number;
  max_concurrent_ops: number;
}

/**
 * Authenticates every public request via API key and injects tenant
 * context onto req.tenant. Downstream services trust X-Tenant-ID ONLY
 * because it is set here, after verification — a customer's request body
 * or headers are never treated as tenant identity.
 *
 * Validation results are cached briefly in Redis so a burst of requests
 * from one customer doesn't hammer Service 1 with a lookup per request;
 * the short TTL keeps a revoked key from staying valid for long.
 */
export function authMiddleware(redis: Redis) {
  return async (req: Request, res: Response, next: NextFunction) => {
    const authHeader = req.headers["authorization"];
    const apiKey =
      typeof authHeader === "string" && authHeader.startsWith("Bearer ")
        ? authHeader.slice("Bearer ".length)
        : (req.headers["x-api-key"] as string | undefined);

    if (!apiKey) {
      return sendProblem(res, 401, "missing_credentials", "Provide an API key via Authorization: Bearer <key> or X-API-Key.", req.requestId);
    }

    const cacheKey = `nexora:authcache:${apiKey}`;
    try {
      const cached = await redis.get(cacheKey);
      if (cached) {
        const v: ValidateResponse = JSON.parse(cached);
        applyTenantContext(req, v);
        return next();
      }

      const resp = await fetch(`${CUSTOMER_ACCESS_URL}/internal/auth/validate`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ api_key: apiKey }),
      });

      if (resp.status !== 200) {
        return sendProblem(res, 401, "invalid_api_key", "The provided API key is invalid or revoked.", req.requestId);
      }

      const v = (await resp.json()) as ValidateResponse;
      await redis.set(cacheKey, JSON.stringify(v), "EX", VALIDATION_CACHE_TTL_SECONDS);
      applyTenantContext(req, v);
      next();
    } catch (err) {
      return sendProblem(res, 503, "upstream_unavailable", "Authentication service is temporarily unavailable.", req.requestId);
    }
  };
}

function applyTenantContext(req: Request, v: ValidateResponse) {
  req.tenant = {
    tenantId: v.tenant_id,
    tier: v.tier,
    rateLimitRpm: v.rate_limit_rpm,
    monthlyQuota: v.monthly_quota,
    maxConcurrentOps: v.max_concurrent_ops,
  };
}
