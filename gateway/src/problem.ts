import type { Response } from "express";

/** RFC 7807 Problem Details, matching the shape every backend service uses,
 * so gateway-originated errors (auth, rate limit) look identical to
 * service-originated ones from the client's point of view. */
export function sendProblem(
  res: Response,
  status: number,
  title: string,
  detail: string,
  requestId: string
) {
  res.status(status).json({
    type: "about:blank",
    title,
    status,
    detail,
    request_id: requestId,
  });
}
