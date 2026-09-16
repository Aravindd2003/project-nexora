import { randomUUID } from "node:crypto";
import type { Request, Response, NextFunction } from "express";

/**
 * Assigns a unique request ID to every inbound request and echoes it back
 * on the response. This ID is forwarded to every downstream service so an
 * operator can trace one customer request across the whole platform
 * (Gateway -> Service -> Worker -> Webhook).
 */
export function requestId(req: Request, res: Response, next: NextFunction) {
  const id = (req.headers["x-request-id"] as string) || randomUUID();
  req.requestId = id;
  res.setHeader("X-Request-ID", id);
  next();
}
