/**
 * @description
 * Remote signing endpoint for Polymarket Builder Attribution.
 * Signs requests with builder credentials server-side to keep them secure.
 * 
 * Used by BuilderConfig for order attribution and RelayClient authentication.
 */

import { NextRequest, NextResponse } from "next/server";
import {
  BuilderApiKeyCreds,
  buildHmacSignature,
} from "@polymarket/builder-signing-sdk";

const BUILDER_CREDENTIALS: BuilderApiKeyCreds = {
  key: process.env.POLY_BUILDER_API_KEY!,
  secret: process.env.POLY_BUILDER_SECRET!,
  passphrase: process.env.POLY_BUILDER_PASSPHRASE!,
};

export async function POST(request: NextRequest) {
  try {
    if (!BUILDER_CREDENTIALS.key || !BUILDER_CREDENTIALS.secret || !BUILDER_CREDENTIALS.passphrase) {
      return NextResponse.json(
        { error: "Missing builder credentials. Set POLY_BUILDER_API_KEY, POLY_BUILDER_SECRET, POLY_BUILDER_PASSPHRASE." },
        { status: 500 }
      );
    }

    const { method, path, body } = await request.json();
    const sigTimestamp = Date.now().toString();

    const signature = buildHmacSignature(
      BUILDER_CREDENTIALS.secret,
      parseInt(sigTimestamp),
      method,
      path,
      body
    );

    return NextResponse.json({
      POLY_BUILDER_SIGNATURE: signature,
      POLY_BUILDER_TIMESTAMP: sigTimestamp,
      POLY_BUILDER_API_KEY: BUILDER_CREDENTIALS.key,
      POLY_BUILDER_PASSPHRASE: BUILDER_CREDENTIALS.passphrase,
    });
  } catch (error: any) {
    console.error("Builder signing error:", error);
    return NextResponse.json(
      { error: error.message || "Failed to sign request" },
      { status: 500 }
    );
  }
}
