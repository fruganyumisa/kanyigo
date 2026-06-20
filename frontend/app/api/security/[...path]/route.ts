import { NextRequest } from "next/server";

const API_INTERNAL_BASE = process.env.API_INTERNAL_BASE ?? "http://localhost:8080";

type Params = { params: Promise<{ path?: string[] }> };

export async function GET(request: NextRequest, context: Params) {
  return proxySecurity(request, context);
}

export async function POST(request: NextRequest, context: Params) {
  return proxySecurity(request, context);
}

export async function DELETE(request: NextRequest, context: Params) {
  return proxySecurity(request, context);
}

async function proxySecurity(request: NextRequest, context: Params) {
  const { path = [] } = await context.params;
  const upstream = new URL(`/api/security${path.length ? `/${path.join("/")}` : ""}`, API_INTERNAL_BASE);
  request.nextUrl.searchParams.forEach((value, key) => upstream.searchParams.append(key, value));
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
    headers: { "Content-Type": response.headers.get("Content-Type") ?? "application/json" }
  });
}
