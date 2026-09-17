import type { Request, Response, NextFunction } from "express";
import fetch from "node-fetch";
import { sendProblem } from "../problem.js";

const USAGE_WEBHOOKS_URL = process.env.USAGE_WEBHOOKS_URL!;

/**
 * Enforces the MONTHLY QUOTA — distinct from the per-minute rate limit
 * (rateLimit.ts). A tenant can be well within their rate limit and still
 * be quota-exhausted for the month; these are orthogonal controls per the
 * spec (section 14), so this runs as a separate check rather than being
 * folded into rateLimitMiddleware.
 *
 * Only applied to operation-creation requests, and fails open (allows the
 * request through with a logged warning) if the usage service is
 * unreachable — a transient dependency outage on a non-critical check
 * should not take down the whole write path.
 */
export function quotaGuard() {
  return async (req: Request, res: Response, next: NextFunction) => {
    const tenant = req.tenant!;
    try {
      const resp = await fetch(`${USAGE_WEBHOOKS_URL}/internal/usage/consumption/${tenant.tenantId}`, {
        headers: { "X-Request-ID": req.requestId },
      });
      if (!resp.ok) {
        console.warn(`quotaGuard: consumption lookup failed with ${resp.status}, failing open`);
        return next();
      }
      const { units_consumed } = (await resp.json()) as { units_consumed: number };

      if (units_consumed >= tenant.monthlyQuota) {
        return sendProblem(
          res,
          429,
          "quota_exhausted",
          `Monthly quota of ${tenant.monthlyQuota} processing units has been consumed. Quota resets next billing cycle.`,
          req.requestId
        );
      }
      next();
    } catch (err) {
      console.warn(`quotaGuard: error checking consumption, failing open: ${(err as Error).message}`);
      next();
    }
  };
}
