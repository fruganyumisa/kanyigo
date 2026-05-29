import { NextRequest } from "next/server";

const API_INTERNAL_BASE = process.env.API_INTERNAL_BASE ?? "http://localhost:8080";

export async function GET(request: NextRequest) {
  const upstream = new URL("/api/logs", API_INTERNAL_BASE);
  request.nextUrl.searchParams.forEach((value, key) => {
    upstream.searchParams.append(key, value);
  });

  const response = await fetch(upstream, {
    headers: {
      Cookie: request.headers.get("Cookie") ?? ""
    },
    cache: "no-store"
  });

  return new Response(response.body, {
    status: response.status,
    headers: {
      "Content-Type": response.headers.get("Content-Type") ?? "application/json"
    }
  });
}
