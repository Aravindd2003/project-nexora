import type { Request, Response, NextFunction } from "express";
import type { Redis } from "ioredis";
import { sendProblem } from "../problem.js";

/**
 * Enforces per-tenant max concurrent RUNNING operations at admission time,
 * using a Redis counter that Service 2 increments/decrements as operations
 * enter and leave RUNNING. The Gateway only checks the limit here; Service 2
 * owns incrementing/decrementing (see its attempts/start and
 * attempts/complete handlers) so the two never drift out of sync from a
 * single source of truth problem.
 *
 * Only applied to operation-creation requests.
 */
export function concurrencyGuard(redis: Redis) {
  return async (req: Request, res: Response, next: NextFunction) => {
    const tenant = req.tenant!;
    const key = `nexora:concurrency:${tenant.tenantId}`;
    const current = parseInt((await redis.get(key)) || "0", 10);

    if (current >= tenant.maxConcurrentOps) {
      return sendProblem(
        res,
        429,
        "concurrency_limit_exceeded",
        `Maximum of ${tenant.maxConcurrentOps} concurrent operations reached. The operation will need to be retried once capacity frees up.`,
        req.requestId
      );
    }
    next();
  };
}
