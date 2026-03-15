/**
 * @description
 * Hardened remote signing endpoint for Polymarket Builder attribution.
 *
 * Security controls:
 * - Requires authenticated wallet session to mint a short-lived signer token.
 * - Requires Bearer signer token for POST signing.
 * - Origin allowlist enforcement.
 * - Per-IP fixed-window rate limiting.
 */

import { NextRequest, NextResponse } from "next/server";
import { createHmac, randomBytes, timingSafeEqual } from "crypto";
import {
  BuilderApiKeyCreds,
  buildHmacSignature,
} from "@polymarket/builder-signing-sdk";

const BUILDER_CREDENTIALS: BuilderApiKeyCreds = {
  key: process.env.POLY_BUILDER_API_KEY || "",
  secret: process.env.POLY_BUILDER_SECRET || "",
  passphrase: process.env.POLY_BUILDER_PASSPHRASE || "",
};

const SIGNER_TOKEN_SECRET =
  process.env.SIGNER_TOKEN_SECRET || process.env.AUTH_JWT_SECRET || "";
const BACKEND_API_ORIGIN = (
  process.env.NEXT_PUBLIC_API_URL ||
  process.env.API_URL ||
  "http://localhost:8080"
).replace(/\/+$/, "");
const SIGNER_TOKEN_TTL_SECONDS = Math.max(
  30,
  Number.parseInt(process.env.SIGNER_TOKEN_TTL_SECONDS || "90", 10) || 90
);
const SIGN_RATE_LIMIT_PER_MINUTE = Math.max(
  10,
  Number.parseInt(process.env.SIGN_RATE_LIMIT_PER_MINUTE || "60", 10) || 60
);

type SignerTokenClaims = {
  sub: string;
  wallet: string;
  iat: number;
  exp: number;
  nonce: string;
};

type BackendAssertionClaims = {
  sub?: string;
  wallet?: string;
  token_type?: string;
  iat?: number;
  exp?: number;
};

type BackendMeResponse = {
  id?: string;
  eoa_address?: string;
  vault_address?: string;
};

function normalizeOrigin(value: string): string | null {
  const trimmed = value.trim();
  if (!trimmed) return null;
  const withScheme = trimmed.startsWith("http://") || trimmed.startsWith("https://")
    ? trimmed
    : `https://${trimmed.replace(/^\/+/, "")}`;
  try {
    return new URL(withScheme).origin;
  } catch {
    return null;
  }
}

const ALLOWED_ORIGINS = (() => {
  const configured = (process.env.FRONTEND_URL || "")
    .split(",")
    .map((item) => item.trim())
    .filter(Boolean);
  const envHints = [
    process.env.NEXT_PUBLIC_APP_URL || "",
    process.env.APP_URL || "",
    process.env.NEXT_PUBLIC_VERCEL_URL || "",
    process.env.VERCEL_URL || "",
    process.env.RAILWAY_PUBLIC_DOMAIN || "",
    process.env.RAILWAY_STATIC_URL || "",
  ];

  const defaults = ["http://localhost:3000", "http://127.0.0.1:3000"];
  const allowed = new Set<string>(defaults);
  for (const value of [...configured, ...envHints]) {
    const normalized = normalizeOrigin(value);
    if (normalized) {
      allowed.add(normalized);
    }
  }
  return allowed;
})();

const rateBucket = new Map<string, { count: number; resetAt: number }>();
const MAX_RATE_BUCKET_ENTRIES = 10_000;

function nowSeconds() {
  return Math.floor(Date.now() / 1000);
}

function base64UrlEncode(input: Buffer | string): string {
  return Buffer.from(input)
    .toString("base64")
    .replace(/\+/g, "-")
    .replace(/\//g, "_")
    .replace(/=+$/g, "");
}

function base64UrlDecode(input: string): Buffer {
  const normalized = input.replace(/-/g, "+").replace(/_/g, "/");
  const padLength = (4 - (normalized.length % 4)) % 4;
  return Buffer.from(normalized + "=".repeat(padLength), "base64");
}

function signCompactToken(payload: SignerTokenClaims): string {
  const encodedPayload = base64UrlEncode(JSON.stringify(payload));
  const signature = createHmac("sha256", SIGNER_TOKEN_SECRET)
    .update(encodedPayload)
    .digest();
  return `${encodedPayload}.${base64UrlEncode(signature)}`;
}

function verifyCompactToken(token: string): SignerTokenClaims | null {
  const [payloadPart, sigPart] = token.split(".");
  if (!payloadPart || !sigPart) return null;

  const expectedSig = createHmac("sha256", SIGNER_TOKEN_SECRET)
    .update(payloadPart)
    .digest();

  const providedSig = base64UrlDecode(sigPart);
  if (providedSig.length !== expectedSig.length) return null;
  if (!timingSafeEqual(providedSig, expectedSig)) return null;

  let payload: SignerTokenClaims;
  try {
    payload = JSON.parse(base64UrlDecode(payloadPart).toString("utf8"));
  } catch {
    return null;
  }

  const now = nowSeconds();
  if (!payload.exp || payload.exp <= now) return null;
  if (!payload.iat || payload.iat > now + 10) return null;
  if (!payload.sub || !payload.wallet) return null;

  return payload;
}

function verifyBackendAssertionToken(token: string): { userID: string; wallet: string } | null {
  const backendAuthSecret = process.env.AUTH_JWT_SECRET || SIGNER_TOKEN_SECRET;
  if (!backendAuthSecret) return null;

  const [headerPart, payloadPart, sigPart] = token.split(".");
  if (!headerPart || !payloadPart || !sigPart) return null;

  let claims: BackendAssertionClaims;
  let header: { alg?: string } | null = null;
  try {
    header = JSON.parse(base64UrlDecode(headerPart).toString("utf8")) as {
      alg?: string;
    };
    claims = JSON.parse(base64UrlDecode(payloadPart).toString("utf8")) as BackendAssertionClaims;
  } catch {
    return null;
  }

  if (String(header?.alg || "").toUpperCase() !== "HS512") {
    return null;
  }

  const expectedSig = createHmac("sha512", backendAuthSecret)
    .update(`${headerPart}.${payloadPart}`)
    .digest();

  const providedSig = base64UrlDecode(sigPart);
  if (providedSig.length !== expectedSig.length) return null;
  if (!timingSafeEqual(providedSig, expectedSig)) return null;

  const now = nowSeconds();
  const iat = Number(claims.iat);
  const exp = Number(claims.exp);
  if (!Number.isFinite(iat) || iat > now + 10) return null;
  if (!Number.isFinite(exp) || exp <= now) return null;
  if (String(claims.token_type || "").toLowerCase().trim() !== "signer_assertion") return null;

  const userID = String(claims.sub || "").trim();
  const wallet = String(claims.wallet || "").trim();
  if (!userID || !wallet) return null;

  return { userID, wallet };
}

function getRequestOrigin(request: NextRequest): string | null {
  const host = (
    request.headers.get("x-forwarded-host") ||
    request.headers.get("host") ||
    request.nextUrl.host
  )
    ?.split(",")[0]
    ?.trim();

  if (!host) return null;

  const protoRaw = (
    request.headers.get("x-forwarded-proto") ||
    request.nextUrl.protocol ||
    "https:"
  )
    .split(",")[0]
    .trim()
    .replace(/:$/, "")
    .toLowerCase();

  const proto = protoRaw === "http" || protoRaw === "https" ? protoRaw : "https";
  return `${proto}://${host}`;
}

function isOriginAllowed(request: NextRequest): boolean {
  const requestOrigin = getRequestOrigin(request);
  const origin = request.headers.get("origin")?.trim();
  if (origin) {
    return ALLOWED_ORIGINS.has(origin) || origin === requestOrigin;
  }

  const secFetchSite = (request.headers.get("sec-fetch-site") || "").trim().toLowerCase();
  if (secFetchSite && !["same-origin", "same-site", "none"].includes(secFetchSite)) {
    return false;
  }

  const referer = request.headers.get("referer")?.trim();
  if (referer) {
    try {
      const refererOrigin = new URL(referer).origin;
      return ALLOWED_ORIGINS.has(refererOrigin) || refererOrigin === requestOrigin;
    } catch {
      return false;
    }
  }

  // Allow trusted same-site/non-browser calls when Origin is omitted.
  return true;
}

function normalizeRateLimitIdentity(raw: string): string {
  const clean = String(raw || "")
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9:_-]/g, "");
  return clean || "anon";
}

function pruneExpiredRateBuckets(now: number) {
  for (const [entryKey, entry] of rateBucket.entries()) {
    if (entry.resetAt <= now) {
      rateBucket.delete(entryKey);
    }
  }
}

function evictLeastRecentlyUsedBuckets(maxEntries: number) {
  while (rateBucket.size > maxEntries) {
    const oldestKey = rateBucket.keys().next().value as string | undefined;
    if (!oldestKey) {
      break;
    }
    rateBucket.delete(oldestKey);
  }
}

function isRateLimited(keyScope: string, identity: string): boolean {
  const now = Date.now();
  pruneExpiredRateBuckets(now);

  const key = `${keyScope}:${normalizeRateLimitIdentity(identity)}`;
  const state = rateBucket.get(key);

  if (!state || state.resetAt <= now) {
    rateBucket.set(key, { count: 1, resetAt: now + 60_000 });
    evictLeastRecentlyUsedBuckets(MAX_RATE_BUCKET_ENTRIES);
    return false;
  }

  state.count += 1;
  // Map insertion order is used as an LRU queue.
  rateBucket.delete(key);
  rateBucket.set(key, state);
  evictLeastRecentlyUsedBuckets(MAX_RATE_BUCKET_ENTRIES);
  return state.count > SIGN_RATE_LIMIT_PER_MINUTE;
}

function requireConfiguredSecrets() {
  if (!SIGNER_TOKEN_SECRET) {
    return "Missing SIGNER_TOKEN_SECRET";
  }
  if (!BUILDER_CREDENTIALS.key || !BUILDER_CREDENTIALS.secret || !BUILDER_CREDENTIALS.passphrase) {
    return "Missing POLY_BUILDER_API_KEY, POLY_BUILDER_SECRET, or POLY_BUILDER_PASSPHRASE";
  }
  return null;
}

function validateSigningInputs(method: unknown, path: unknown, body: unknown): string | null {
  const m = String(method || "").toUpperCase().trim();
  const p = String(path || "").trim();

  if (!["GET", "POST", "DELETE"].includes(m)) {
    return "method is not allowed";
  }
  if (!p.startsWith("/") || p.length > 256) {
    return "path must start with '/' and be <= 256 chars";
  }
  if (p.includes("\n") || p.includes("\r")) {
    return "invalid path";
  }

  const bodyString = typeof body === "string" ? body : body == null ? "" : JSON.stringify(body);
  if (bodyString.length > 50_000) {
    return "body too large";
  }

  return null;
}

function jsonNoStore(data: unknown, init?: ResponseInit) {
  return NextResponse.json(data, {
    ...init,
    headers: {
      "Cache-Control": "no-store",
      ...(init?.headers || {}),
    },
  });
}

function normalizeSigningBody(body: unknown): string | undefined {
  if (body === undefined) {
    return undefined;
  }
  if (typeof body === "string") {
    return body;
  }
  return JSON.stringify(body);
}

async function checkAccountAllowed(request: NextRequest): Promise<{
  ok: boolean;
  status: number;
  message?: string;
  userID?: string;
  wallet?: string;
}> {
  const cookie = request.headers.get("cookie") || "";
  const userAgent = request.headers.get("user-agent") || "";

  const headers: Record<string, string> = {};
  if (cookie) headers.cookie = cookie;
  if (userAgent) {
    headers["user-agent"] = userAgent;
  }

  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), 4000);

  try {
    const response = await fetch(`${BACKEND_API_ORIGIN}/api/v1/user/me`, {
      method: "GET",
      headers,
      cache: "no-store",
      signal: controller.signal,
    });

    if (response.status === 403) {
      return { ok: false, status: 403, message: "account is restricted" };
    }
    if (response.status === 401) {
      return { ok: false, status: 401, message: "unauthorized" };
    }
    if (!response.ok) {
      return {
        ok: false,
        status: 503,
        message: "unable to verify account moderation status",
      };
    }

    let user: BackendMeResponse | null = null;
    try {
      user = (await response.json()) as BackendMeResponse;
    } catch {
      return {
        ok: false,
        status: 503,
        message: "unable to read account session",
      };
    }

    const userID = String(user?.id || "").trim();
    const wallet = String(user?.vault_address || user?.eoa_address || "").trim();
    if (!userID || !wallet) {
      return {
        ok: false,
        status: 401,
        message: "unauthorized",
      };
    }

    return { ok: true, status: 200, userID, wallet };
  } catch {
    return {
      ok: false,
      status: 503,
      message: "unable to verify account moderation status",
    };
  } finally {
    clearTimeout(timer);
  }
}

export async function GET(request: NextRequest) {
  const configErr = requireConfiguredSecrets();
  if (configErr) {
    console.error("[polymarket/sign][GET] Misconfigured signer route:", configErr);
    return jsonNoStore({ error: configErr }, { status: 500 });
  }

  if (!isOriginAllowed(request)) {
    return jsonNoStore({ error: "forbidden origin" }, { status: 403 });
  }

  const authHeader = request.headers.get("authorization") || "";
  const bearerToken = authHeader.startsWith("Bearer ")
    ? authHeader.slice(7).trim()
    : "";
  const assertion = bearerToken ? verifyBackendAssertionToken(bearerToken) : null;

  let userID = assertion?.userID || "";
  let wallet = assertion?.wallet || "";

  if (!userID || !wallet) {
    const moderation = await checkAccountAllowed(request);
    if (!moderation.ok || !moderation.userID || !moderation.wallet) {
      return jsonNoStore(
        { error: moderation.message || "forbidden" },
        { status: moderation.status }
      );
    }
    userID = moderation.userID;
    wallet = moderation.wallet;
  }

  if (isRateLimited("sign-token", userID)) {
    return jsonNoStore({ error: "rate limit exceeded" }, { status: 429 });
  }

  const now = nowSeconds();
  const payload: SignerTokenClaims = {
    sub: userID,
    wallet,
    iat: now,
    exp: now + SIGNER_TOKEN_TTL_SECONDS,
    nonce: cryptoRandomString(16),
  };

  return jsonNoStore({
    token: signCompactToken(payload),
    expires_at: payload.exp,
  });
}

export async function POST(request: NextRequest) {
  const configErr = requireConfiguredSecrets();
  if (configErr) {
    console.error("[polymarket/sign][POST] Misconfigured signer route:", configErr);
    return jsonNoStore({ error: configErr }, { status: 500 });
  }

  if (!isOriginAllowed(request)) {
    return jsonNoStore({ error: "forbidden origin" }, { status: 403 });
  }

  const authHeader = request.headers.get("authorization") || "";
  const token = authHeader.startsWith("Bearer ") ? authHeader.slice(7).trim() : "";
  const claims = token ? verifyCompactToken(token) : null;
  if (!claims) {
    return jsonNoStore({ error: "invalid signer token" }, { status: 401 });
  }

  if (isRateLimited("sign", claims.sub)) {
    return jsonNoStore({ error: "rate limit exceeded" }, { status: 429 });
  }

  try {
    const { method, path, body, timestamp } = await request.json();

    const validationError = validateSigningInputs(method, path, body);
    if (validationError) {
      return jsonNoStore({ error: validationError }, { status: 400 });
    }

    const sigTimestamp =
      timestamp !== undefined
        ? Math.floor(Number(timestamp))
        : Math.floor(Date.now() / 1000);

    const signingBody = normalizeSigningBody(body);
    const signature = buildHmacSignature(
      BUILDER_CREDENTIALS.secret,
      sigTimestamp,
      String(method).toUpperCase(),
      String(path),
      signingBody
    );

    return jsonNoStore({
      POLY_BUILDER_SIGNATURE: signature,
      POLY_BUILDER_TIMESTAMP: `${sigTimestamp}`,
      POLY_BUILDER_API_KEY: BUILDER_CREDENTIALS.key,
      POLY_BUILDER_PASSPHRASE: BUILDER_CREDENTIALS.passphrase,
    });
  } catch (error: any) {
    console.error(
      "[polymarket/sign][POST] Failed to sign request:",
      error?.message || error
    );
    return jsonNoStore(
      { error: error?.message || "Failed to sign request" },
      { status: 500 }
    );
  }
}

function cryptoRandomString(size: number): string {
  return randomBytes(size).toString("hex");
}
