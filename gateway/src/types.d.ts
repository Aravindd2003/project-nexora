export interface TenantContext {
  tenantId: string;
  tier: string;
  rateLimitRpm: number;
  monthlyQuota: number;
  maxConcurrentOps: number;
}

declare global {
  namespace Express {
    interface Request {
      tenant?: TenantContext;
      requestId: string;
    }
  }
}
export {};
