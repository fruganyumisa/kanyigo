import { NextRequest } from "next/server";

const API_INTERNAL_BASE = process.env.API_INTERNAL_BASE ?? "http://localhost:8080";

export async function GET(request: NextRequest) {
  return proxyUsers(request);
}

export async function POST(request: NextRequest) {
  return proxyUsers(request);
}

async function proxyUsers(request: NextRequest) {
  const upstream = new URL("/api/users", API_INTERNAL_BASE);

  const response = await fetch(upstream, {
    method: request.method,
    body: request.method === "GET" ? undefined : await request.text(),
    headers: {
      "Content-Type": request.headers.get("Content-Type") ?? "application/json",
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
