import { assertEquals } from "jsr:@std/assert@1.0.19";
import {
  createPublicKey,
  createVerify,
  generateKeyPairSync,
} from "node:crypto";
import { signAppJWT } from "./github.ts";

function decodeBase64url(s: string): string {
  const padded = s.replace(/-/g, "+").replace(/_/g, "/");
  return atob(padded.padEnd(padded.length + (4 - padded.length % 4) % 4, "="));
}

// GitHub's own "Generate a private key" button downloads PKCS#1 ("RSA
// PRIVATE KEY") PEMs, not PKCS#8 — this is exactly the format a real
// downloaded key arrives in (confirmed against a real GitHub App key;
// an earlier version of signAppJWT used a JWT library that flatly
// rejected this format, which would have forced every operator to run
// openssl by hand before the key GitHub actually gives them was usable
// at all).
function generatePKCS1KeyPair() {
  return generateKeyPairSync("rsa", {
    modulusLength: 2048,
    publicKeyEncoding: { type: "spki", format: "pem" },
    privateKeyEncoding: { type: "pkcs1", format: "pem" },
  });
}

Deno.test("signAppJWT produces a valid RS256 JWT from a real PKCS#1 ('RSA PRIVATE KEY') PEM", () => {
  const { privateKey, publicKey } = generatePKCS1KeyPair();
  if (!privateKey.startsWith("-----BEGIN RSA PRIVATE KEY-----")) {
    throw new Error("test fixture isn't actually PKCS#1 — fix the test itself");
  }

  const jwt = signAppJWT("123456", privateKey);
  const parts = jwt.split(".");
  assertEquals(parts.length, 3);

  const header = JSON.parse(decodeBase64url(parts[0]));
  assertEquals(header.alg, "RS256");
  assertEquals(header.typ, "JWT");

  const payload = JSON.parse(decodeBase64url(parts[1]));
  assertEquals(payload.iss, "123456");
  if (typeof payload.iat !== "number" || typeof payload.exp !== "number") {
    throw new Error("payload missing iat/exp");
  }
  if (payload.exp - payload.iat > 700) {
    throw new Error("JWT TTL exceeds GitHub's own 10-minute cap");
  }

  // The signature must actually verify against the matching public key
  // — not just "looks like a JWT."
  const verifier = createVerify("RSA-SHA256");
  verifier.update(`${parts[0]}.${parts[1]}`);
  verifier.end();
  const sigBytes = Uint8Array.from(
    decodeBase64url(parts[2]),
    (c) => c.charCodeAt(0),
  );
  const ok = verifier.verify(createPublicKey(publicKey), sigBytes);
  if (!ok) {
    throw new Error("JWT signature does not verify against the public key");
  }
});

Deno.test("signAppJWT also accepts a PKCS#8 ('PRIVATE KEY') PEM", () => {
  const { privateKey } = generateKeyPairSync("rsa", {
    modulusLength: 2048,
    publicKeyEncoding: { type: "spki", format: "pem" },
    privateKeyEncoding: { type: "pkcs8", format: "pem" },
  });
  if (!privateKey.startsWith("-----BEGIN PRIVATE KEY-----")) {
    throw new Error("test fixture isn't actually PKCS#8 — fix the test itself");
  }
  const jwt = signAppJWT("123456", privateKey);
  assertEquals(jwt.split(".").length, 3);
});
