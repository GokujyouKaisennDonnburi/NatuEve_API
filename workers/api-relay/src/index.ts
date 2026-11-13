/**
 * NatuEve API の中継用 Worker（ADR-0034）。
 *
 * 受けたリクエストのパスとクエリだけを引き継ぎ、固定の転送先（Cloud Run の run.app の URL）へ転送する。
 * 転送時に秘密ヘッダーを付け、利用者 IP のヘッダーを CF-Connecting-IP の値で上書きする。
 * HTTPS でないリクエストは転送せず、同じパスとクエリの HTTPS へ 301 でリダイレクトする（ローカル開発を除く）。
 */

/** API が中継元の確認に使うヘッダー名。internal/middleware/relay.go の RelayAuthHeader と同じ値にする。 */
export const RELAY_AUTH_HEADER = "X-Relay-Auth";

/** ローカル開発（wrangler dev）では HTTP で使うため、HTTPS へリダイレクトしないホスト。 */
const HTTP_LOCAL_HOSTS = new Set(["localhost", "127.0.0.1", "[::1]"]);

/**
 * HTTP のリクエストを、同じパスとクエリの HTTPS へ 301 でリダイレクトするレスポンスを返す。
 * HTTPS のリクエストと HTTP_LOCAL_HOSTS のホストでは null を返す。
 * ゾーンの Always Use HTTPS（Cloudflare の設定）でも同じ 301 が返り、このリダイレクトはそのフォールバック（ADR-0034）。
 */
export function httpsRedirect(requestURL: string): Response | null {
  const url = new URL(requestURL);
  if (url.protocol !== "http:" || HTTP_LOCAL_HOSTS.has(url.hostname)) {
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
