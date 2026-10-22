import { describe, expect, it, vi } from "vitest";
import { RELAY_AUTH_HEADER, relay, upstreamURL } from "../src/index";

const ORIGIN = "https://natueve-api-xxxx.a.run.app";
const SECRET = "0123456789abcdef0123456789abcdef-secret";
const IP_HEADER = "X-NatuEve-Client-IP";

const env: Env = {
  ORIGIN_URL: ORIGIN,
  RELAY_SECRET: SECRET,
  CLIENT_IP_HEADER: IP_HEADER,
};

/** relay を実行し、fetcher が受け取った URL と init を返す。 */
async function run(request: Request) {
  const fetcher = vi.fn(
    async (_url: URL | string | Request, _init?: RequestInit) =>
      new Response("ok"),
  );
  await relay(request, env, fetcher as unknown as typeof fetch);
  const [url, init] = fetcher.mock.calls[0];
  return { url: url as URL, init: init as RequestInit & { headers: Headers } };
}

describe("upstreamURL", () => {
  const tests: {
    name: string;
    request: string;
    origin: string;
    want: string;
  }[] = [
    {
      name: "パスとクエリを引き継ぐ",
      request: "https://natueve-api.natuportal.org/events?q=a&page=2",
      origin: ORIGIN,
      want: `${ORIGIN}/events?q=a&page=2`,
    },
    {
      name: "クエリなし",
      request: "https://natueve-api.natuportal.org/events",
      origin: ORIGIN,
      want: `${ORIGIN}/events`,
    },
    {
      name: "ホスト・ポート・スキームに関係なく転送先のオリジンになる",
      request: "http://evil.example:8080/events?x=1",
      origin: ORIGIN,
      want: `${ORIGIN}/events?x=1`,
    },
    {
      name: "パスが // で始まっても転送先のホストのまま",
      request: "https://natueve-api.natuportal.org//evil.example/x",
      origin: ORIGIN,
      want: `${ORIGIN}//evil.example/x`,
    },
    {
      name: "ORIGIN_URL のパスとクエリは置き換わる",
      request: "https://natueve-api.natuportal.org/events?q=1",
      origin: `${ORIGIN}/base?a=b`,
      want: `${ORIGIN}/events?q=1`,
    },
    {
      name: "ORIGIN_URL のクエリは受けたリクエストにクエリが無ければ消える",
      request: "https://natueve-api.natuportal.org/events",
      origin: `${ORIGIN}/base?a=b`,
      want: `${ORIGIN}/events`,
    },
  ];

  for (const tt of tests) {
    it(tt.name, () => {
      const got = upstreamURL(tt.request, tt.origin);
      expect(got.toString()).toBe(tt.want);
      expect(got.host).toBe(new URL(tt.origin).host);
    });
  }
});

describe("relay", () => {
  it("redirect は manual、cache は no-store、method はそのまま", async () => {
    const { url, init } = await run(
      new Request("https://natueve-api.natuportal.org/events?q=1", {
        method: "PUT",
        body: "x",
      }),
    );
    expect(url.toString()).toBe(`${ORIGIN}/events?q=1`);
    expect(init.redirect).toBe("manual");
    expect(init.cache).toBe("no-store");
    expect(init.method).toBe("PUT");
  });

  it("POST の body が転送される", async () => {
    const { init } = await run(
      new Request("https://natueve-api.natuportal.org/events", {
        method: "POST",
        body: '{"a":1}',
        headers: { "Content-Type": "application/json" },
      }),
    );
    expect(init.method).toBe("POST");
    expect(await new Response(init.body).text()).toBe('{"a":1}');
  });

  it("利用者が付けた X-Relay-Auth は env の値で上書きされる", async () => {
    const { init } = await run(
      new Request("https://natueve-api.natuportal.org/events", {
        headers: { [RELAY_AUTH_HEADER]: "forged" },
      }),
    );
    expect(init.headers.get(RELAY_AUTH_HEADER)).toBe(SECRET);
  });

  it("CLIENT_IP_HEADER は CF-Connecting-IP の値になる（利用者が付けた値は上書きされる）", async () => {
    const { init } = await run(
      new Request("https://natueve-api.natuportal.org/events", {
        headers: {
          "CF-Connecting-IP": "198.51.100.7",
          [IP_HEADER]: "203.0.113.99",
        },
      }),
    );
    expect(init.headers.get(IP_HEADER)).toBe("198.51.100.7");
  });

  it("CF-Connecting-IP が無いとき、利用者が付けた CLIENT_IP_HEADER は送られない", async () => {
    const { init } = await run(
      new Request("https://natueve-api.natuportal.org/events", {
        headers: { [IP_HEADER]: "203.0.113.99" },
      }),
    );
    expect(init.headers.has(IP_HEADER)).toBe(false);
  });

  // Node の Request では Host が禁止ヘッダーとして扱われ得るため、
  // Host を付けた Request を作れる場合だけ「転送されない」ことを検証する。
  // 付けられない場合は、Host が最初から含まれないことの確認にとどまる。
  it("Host は転送されない", async () => {
    const request = new Request("https://natueve-api.natuportal.org/events", {
      headers: { Host: "evil.example" },
    });
    const { init } = await run(request);
    expect(init.headers.has("Host")).toBe(false);
  });

  it("Authorization・Content-Type・Origin はそのまま転送される", async () => {
    const { init } = await run(
      new Request("https://natueve-api.natuportal.org/events", {
        method: "POST",
        body: "{}",
        headers: {
          Authorization: "Bearer token",
          "Content-Type": "application/json",
          Origin: "https://natueve.natuportal.org",
        },
      }),
    );
    expect(init.headers.get("Authorization")).toBe("Bearer token");
    expect(init.headers.get("Content-Type")).toBe("application/json");
    expect(init.headers.get("Origin")).toBe("https://natueve.natuportal.org");
  });

  it("fetcher の応答がそのまま返る", async () => {
    const upstream = new Response(null, {
      status: 302,
      headers: { Location: "https://example.com/next" },
    });
    const fetcher = vi.fn(async () => upstream);
    const res = await relay(
      new Request("https://natueve-api.natuportal.org/events"),
      env,
      fetcher as unknown as typeof fetch,
    );
    expect(res).toBe(upstream);
    expect(res.status).toBe(302);
    expect(res.headers.get("Location")).toBe("https://example.com/next");
  });
});
