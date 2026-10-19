/**
 * NatuEve API の中継用 Worker（ADR-0034）。
 *
 * 受けたリクエストのパスとクエリだけを引き継ぎ、固定の転送先（Cloud Run の run.app の URL）へ転送する。
 * 転送時に秘密ヘッダーを付け、利用者 IP のヘッダーを CF-Connecting-IP の値で上書きする。
 */

/** API が中継元の確認に使うヘッダー名。internal/middleware/relay.go の RelayAuthHeader と同じ値にする。 */
export const RELAY_AUTH_HEADER = "X-Relay-Auth";

/**
 * 転送先の URL を返す。オリジンは originURL に固定し、requestURL からはパスとクエリだけを引き継ぐ。
 * パスが "//" で始まっても転送先のホストは変わらない。
 */
export function upstreamURL(requestURL: string, originURL: string): URL {
  const incoming = new URL(requestURL);
  const target = new URL(originURL);
  target.pathname = incoming.pathname;
  target.search = incoming.search;
  return target;
}

/**
 * request を転送先へ送り、応答をそのまま返す。
 * Host は転送先のものになり、秘密ヘッダーと利用者 IP のヘッダーは受けたリクエストの値を使わずに付け直す。
 * CF-Connecting-IP が無い場合、利用者 IP のヘッダーは付けない。
 * リダイレクトは追わずに 3xx をそのまま返し、Cloudflare のキャッシュは使わない。
 */
export function relay(
  request: Request,
  env: Env,
  fetcher: typeof fetch = fetch,
): Promise<Response> {
  const headers = new Headers(request.headers);
  headers.delete("Host");
  headers.set(RELAY_AUTH_HEADER, env.RELAY_SECRET);
  headers.delete(env.CLIENT_IP_HEADER);
  const clientIP = request.headers.get("CF-Connecting-IP");
  if (clientIP) {
    headers.set(env.CLIENT_IP_HEADER, clientIP);
  }

  return fetcher(upstreamURL(request.url, env.ORIGIN_URL), {
    method: request.method,
    headers,
    body: request.body,
    redirect: "manual",
    cache: "no-store",
  });
}

export default {
  fetch(request, env) {
    return relay(request, env);
  },
} satisfies ExportedHandler<Env>;
