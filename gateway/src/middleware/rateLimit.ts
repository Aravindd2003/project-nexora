import type { Request, Response, NextFunction } from "express";
import type { Redis } from "ioredis";
import { sendProblem } from "../problem.js";

/**
 * Fixed-window per-tenant rate limiting, distinct from monthly quota:
 * this guards against short-term bursts ("100 requests/minute"), while
 * quota (enforced downstream in Service 3 / on operation creation) guards
 * against total monthly consumption. The two are orthogonal by design —
 * see Project spec section 14.
 */
export function rateLimitMiddleware(redis: Redis) {
  return async (req: Request, res: Response, next: NextFunction) => {
    const tenant = req.tenant!;
    const windowKey = `nexora:ratelimit:${tenant.tenantId}:${currentMinuteBucket()}`;

    const count = await redis.incr(windowKey);
    if (count === 1) {
      await redis.expire(windowKey, 60);
    }

    if (count > tenant.rateLimitRpm) {
      res.setHeader("Retry-After", "10");
      return sendProblem(
        res,
        429,
        "rate_limited",
        `Rate limit of ${tenant.rateLimitRpm} requests/minute exceeded. Retry shortly.`,
        req.requestId
      );
    }
    next();
  };
}

function currentMinuteBucket(): number {
  return Math.floor(Date.now() / 60000);
}
