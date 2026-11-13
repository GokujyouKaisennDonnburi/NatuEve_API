/**
 * NatuEve API の中継用 Worker（ADR-0034）。
 *
 * 受けたリクエストのパスとクエリだけを引き継ぎ、固定の転送先（Cloud Run の run.app の URL）へ転送する。
 * 転送時に秘密ヘッダーを付け、利用者 IP のヘッダーを CF-Connecting-IP の値で上書きする。
 * HTTPS でないリクエストは転送せず、同じパスとクエリの HTTPS へ 301 でリダイレクトする（ローカル開発を除く）。
 */

/** API が中継元の確認に使うヘッダー名。internal/middleware/relay.go の RelayAuthHeader と同じ値にする。 */
export const RELAY_AUTH_HEADER = "X-Relay-Auth";

/**
 * ローカル開発（wrangler dev）では HTTP で使うため、HTTPS へリダイレクトしないホストかどうかを返す。
 * ループバック（localhost・127.0.0.0/8・[::1]）と、RFC 1918 のプライベート IPv4 を含める。
 * `wrangler dev --ip 0.0.0.0` で LAN 経由（192.168.x.x など）にアクセスした場合、
 * ローカルの https:// に接続先がないため、リダイレクトすると届かなくなる。
 */
function isLocalDevHost(hostname: string): boolean {
  if (hostname === "localhost" || hostname === "[::1]") {
    return true;
  }
  const parts = hostname.split(".");
  if (parts.length !== 4 || parts.some((part) => !/^\d{1,3}$/.test(part))) {
    return false;
  }
  const [first, second] = parts.map(Number);
  if (first > 255 || second > 255) {
    return false;
  }
  return (
    first === 127 ||
    first === 10 ||
    (first === 172 && second >= 16 && second <= 31) ||
    (first === 192 && second === 168)
  );
}

/**
 * HTTP のリクエストを、同じパスとクエリの HTTPS へ 301 でリダイレクトするレスポンスを返す。
 * HTTPS のリクエストと isLocalDevHost のホストでは null を返す。
 * ゾーンの Always Use HTTPS（Cloudflare の設定）でも同じ 301 が返り、このリダイレクトはそのフォールバック（ADR-0034）。
 */
export function httpsRedirect(requestURL: string): Response | null {
  const url = new URL(requestURL);
  if (url.protocol !== "http:" || isLocalDevHost(url.hostname)) {
    return null;
  }
  url.protocol = "https:";
  return new Response(null, {
    status: 301,
    headers: { Location: url.toString() },
  });
}

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
 * HTTPS でないリクエストを 301 でリダイレクトし、HTTPS の request を転送先へ送り、応答をそのまま返す。
 * Host は転送先のものになり、秘密ヘッダーと利用者 IP のヘッダーは受けたリクエストの値を使わずに付け直す。
 * CF-Connecting-IP が無い場合、利用者 IP のヘッダーは付けない。
 * リダイレクトは追わずに 3xx をそのまま返し、Cloudflare のキャッシュは使わない。
 */
export function relay(
  request: Request,
  env: Env,
  fetcher: typeof fetch = fetch,
): Promise<Response> {
  const redirect = httpsRedirect(request.url);
  if (redirect) {
    return Promise.resolve(redirect);
  }

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
