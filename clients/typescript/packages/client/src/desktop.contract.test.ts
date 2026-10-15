// Contract test for the desktop control-plane client.
//
// A fake host is built from docs/api/openapi.v1.yaml: it only answers
// operations the spec declares, rejects request bodies that violate the
// declared request schema, requires the control-plane token header on every
// request (reads included), and serves responses generated from the declared
// response schema. A client call that drifts from the spec (path, method,
// query, body shape) or a spec change the client no longer matches fails here.
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

import { Ajv2020 } from "ajv/dist/2020.js";
import { parse } from "yaml";
import { describe, expect, it, vi } from "vitest";

import { CONTROL_TOKEN_HEADER, createDesktopClient, type DesktopClient } from "./desktop.js";
import { HTTPError } from "./types.js";

type Json = Record<string, unknown>;

const specPath = fileURLToPath(new URL("../../../../../docs/api/openapi.v1.yaml", import.meta.url));
const spec = parse(readFileSync(specPath, "utf8")) as {
  paths: Record<string, Record<string, Json>>;
  components: { schemas: Record<string, Json> };
};

const ajv = new Ajv2020({ strict: false, validateFormats: false, allErrors: true });
ajv.addSchema(spec as unknown as object, "spec");

const pointer = (...parts: string[]) =>
  `spec#/${parts.map((p) => encodeURIComponent(p.replace(/~/g, "~0").replace(/\//g, "~1"))).join("/")}`;

function resolveRef(schema: Json): Json {
  const ref = schema["$ref"];
  if (typeof ref !== "string") return schema;
  const name = ref.replace("#/components/schemas/", "");
  const target = spec.components.schemas[name];
  if (!target) throw new Error(`unresolved $ref ${ref}`);
  return resolveRef(target);
}

/** The smallest instance that satisfies a schema, preferring declared enums and examples. */
function sample(raw: Json, depth = 0): unknown {
  const schema = resolveRef(raw);
  if ("example" in schema) return schema["example"];
  if ("const" in schema) return schema["const"];
  if (Array.isArray(schema["enum"])) return schema["enum"][0];
  const alternatives = (schema["oneOf"] ?? schema["anyOf"]) as Json[] | undefined;
  if (alternatives?.[0]) return sample(alternatives[0], depth + 1);
  if (Array.isArray(schema["allOf"])) {
    return Object.assign({}, ...(schema["allOf"] as Json[]).map((s) => sample(s, depth + 1)));
  }
  switch (schema["type"]) {
    case "object": {
      const props = (schema["properties"] ?? {}) as Record<string, Json>;
      const required = new Set((schema["required"] as string[] | undefined) ?? []);
      const out: Json = {};
      for (const [key, value] of Object.entries(props)) {
        if (depth < 5 || required.has(key)) out[key] = sample(value, depth + 1);
      }
      return out;
    }
    case "array":
      return depth < 5 ? [sample((schema["items"] ?? {}) as Json, depth + 1)] : [];
    case "integer":
      return typeof schema["minimum"] === "number" ? schema["minimum"] : 1;
    case "number":
      return 1;
    case "boolean":
      return true;
    case "string":
      return schema["format"] === "date-time" ? "2026-01-01T00:00:00Z" : "x";
    default:
      return {};
  }
}

interface Observed {
  operationId: string;
  method: string;
  path: string;
  headers: Record<string, string>;
  body: unknown;
  served: unknown;
}

function templateToRegExp(template: string): RegExp {
  const escaped = template.replace(/[.*+?^$()|[\]\\]/g, "\\$&").replace(/\{[^}]+\}/g, "[^/]+");
  return new RegExp(`^${escaped}$`);
}

const operations = Object.entries(spec.paths).flatMap(([template, item]) =>
  Object.entries(item)
    .filter(([method]) => ["get", "post", "put", "patch", "delete"].includes(method))
    .map(([method, op]) => ({ template, method: method.toUpperCase(), op, regexp: templateToRegExp(template) })),
);

/** Fake host serving the desktop spec; records every request it handled. */
function fakeHost(token = "session-token") {
  const observed: Observed[] = [];
  const impl = vi.fn(async (input: RequestInfo | URL, init?: RequestInit): Promise<Response> => {
    const url = new URL(String(input), "http://127.0.0.1:1");
    const method = (init?.method ?? "GET").toUpperCase();
    const headers = init?.headers as Record<string, string>;
    if (headers[CONTROL_TOKEN_HEADER] !== token) return new Response("control-plane token required", { status: 401 });

    const match = operations.find((o) => o.method === method && o.regexp.test(url.pathname));
    if (!match) throw new Error(`the desktop spec declares no operation for ${method} ${url.pathname}`);
    const operationId = String(match.op["operationId"]);

    const declaredQuery = new Set(
      ((match.op["parameters"] ?? []) as Json[]).filter((p) => p["in"] === "query").map((p) => String(p["name"])),
    );
    for (const name of url.searchParams.keys()) {
      if (!declaredQuery.has(name)) throw new Error(`${operationId}: query parameter ${name} is not in the spec`);
    }

    let body: unknown;
    const requestBody = match.op["requestBody"] as Json | undefined;
    if (typeof init?.body === "string") {
      body = JSON.parse(init.body);
      const jsonSchema = (requestBody?.["content"] as Record<string, Json> | undefined)?.["application/json"];
      if (!jsonSchema) throw new Error(`${operationId}: the spec declares no JSON request body`);
      const validate = ajv.compile({
        $ref: pointer("paths", match.template, match.method.toLowerCase(), "requestBody", "content", "application/json", "schema"),
      });
      if (!validate(body)) throw new Error(`${operationId}: request body violates the spec: ${ajv.errorsText(validate.errors)}`);
    } else if (requestBody?.["required"] === true) {
      throw new Error(`${operationId}: the spec requires a request body`);
    }

    const ok = (match.op["responses"] as Record<string, Json>)["200"] ?? {};
    const content = (ok["content"] ?? {}) as Record<string, { schema?: Json }>;
    const accept = headers["Accept"] ?? "application/json";
    const mediaType = content[accept] ? accept : (Object.keys(content)[0] ?? "application/json");
    observed.push({ operationId, method, path: url.pathname + url.search, headers, body, served: undefined });
    const record = observed[observed.length - 1] as Observed;

    if (mediaType === "application/json" || Object.keys(content).length === 0) {
      const schema = content["application/json"]?.schema;
      if (!schema) return new Response(null, { status: 200 });
      const served = sample(schema);
      const validate = ajv.compile({
        $ref: pointer("paths", match.template, match.method.toLowerCase(), "responses", "200", "content", "application/json", "schema"),
      });
      if (!validate(served)) throw new Error(`${operationId}: fake response violates the spec: ${ajv.errorsText(validate.errors)}`);
      record.served = served;
      return new Response(JSON.stringify(served), { status: 200, headers: { "Content-Type": "application/json" } });
    }
    return new Response(new Uint8Array([1, 2, 3]), { status: 200, headers: { "Content-Type": mediaType } });
  });
  return { observed, fetch: impl as unknown as typeof fetch, impl };
}

function newClient(host = fakeHost(), tokenProvider: () => string | Promise<string> = () => "session-token") {
  return { host, client: createDesktopClient({ baseUrl: "http://127.0.0.1:53211", tokenProvider, fetch: host.fetch }) };
}

// Every JSON route the client covers, with the spec operation it must hit.
const routes: Array<[string, string, (c: DesktopClient) => Promise<unknown>]> = [
  ["getSettingsState", "settings.state", (c) => c.settings.state()],
  ["getMeetingSettings", "settings.meeting", (c) => c.settings.meeting()],
  ["patchMeetingSettings", "settings.updateMeeting", (c) => c.settings.updateMeeting({ enabled: true, batchMinutes: 5 })],
  ["getOverlayState", "overlay.state", (c) => c.overlay.state()],
  ["getModes", "modes.list", (c) => c.modes.list()],
  ["startMode", "modes.start", (c) => c.modes.start("dictation")],
  ["stopMode", "modes.stop", (c) => c.modes.stop("dictation")],
  ["listRecordingSessions", "recordings.list", (c) => c.recordings.list({ kind: "meeting", limit: 5, includeSegments: true })],
  ["getRecordingSession", "recordings.get", (c) => c.recordings.get(7)],
  ["createRecordingSession", "recordings.create", (c) => c.recordings.create({ kind: "meeting", title: "Standup", open: true })],
  ["startRecordingSessionCapture", "recordings.start", (c) => c.recordings.start(7)],
  ["pauseRecordingSessionCapture", "recordings.pause", (c) => c.recordings.pause(7)],
  ["resumeRecordingSessionCapture", "recordings.resume", (c) => c.recordings.resume(7)],
  ["stopRecordingSessionCapture", "recordings.stop", (c) => c.recordings.stop(7)],
  ["finishRecordingSession", "recordings.finish", (c) => c.recordings.finish(7, { summary: "Done" })],
  ["getRecordingSessionCaptureState", "recordings.state", (c) => c.recordings.state(7)],
  ["getRecordingSessionNotes", "recordings.notes", (c) => c.recordings.notes(7)],
  ["saveRecordingSessionNotes", "recordings.saveNotes", (c) => c.recordings.saveNotes(7, { contentMd: "- a" })],
  ["listRecordingSessionEnhancements", "recordings.reviews", (c) => c.recordings.reviews(7)],
  ["startRecordingSessionEnhancement", "recordings.startReview", (c) => c.recordings.startReview(7, { language: "de" })],
  ["listRecordingSessionSnapshots", "recordings.snapshots", (c) => c.recordings.snapshots(7)],
  ["getRecordingSessionSnapshot", "recordings.snapshot", (c) => c.recordings.snapshot(7, 3)],
  ["captureRecordingSessionSnapshot", "recordings.captureSnapshot", (c) => c.recordings.captureSnapshot(7, "slide 4")],
  ["openRecordingSessionNotebook", "recordings.open", (c) => c.recordings.open(7)],
  ["closeRecordingSessionNotebook", "recordings.close", (c) => c.recordings.close(7)],
  ["getRecordingSessionSummary", "recordings.summary", (c) => c.recordings.summary(7)],
  ["listMeetingSummaryBatches", "recordings.summaryBatches", (c) => c.recordings.summaryBatches(7)],
  ["searchRecordingSessions", "recordings.search", (c) => c.recordings.search("budget", { kind: "meeting", limit: 10 })],
  ["listRecordingSessionImports", "recordings.imports", (c) => c.recordings.imports(7)],
  ["runTeamsPreflight", "recordings.runPreflight", (c) => c.recordings.runPreflight(7)],
  ["getTeamsPreflight", "recordings.preflight", (c) => c.recordings.preflight(7)],
  ["pasteMeetingTranscript", "recordings.pasteTranscript", (c) => c.recordings.pasteTranscript(7, "WEBVTT", true)],
  ["listNotebooks", "notebooks.list", (c) => c.notebooks.list()],
  ["getNotebook", "notebooks.get", (c) => c.notebooks.get(2)],
  ["createNotebook", "notebooks.create", (c) => c.notebooks.create({ displayName: "Weekly" })],
  ["updateNotebook", "notebooks.update", (c) => c.notebooks.update(2, { displayName: "Renamed" })],
  ["deleteNotebook", "notebooks.delete", (c) => c.notebooks.delete(2)],
  ["listNotebookDestinations", "notebooks.destinations", (c) => c.notebooks.destinations()],
  ["getFeatures", "features", (c) => c.features()],
  ["getAppVersion", "app.version", (c) => c.app.version()],
  ["getFoundryAuthStatus", "foundry.status", (c) => c.foundry.status()],
  ["startFoundryLogin", "foundry.login", (c) => c.foundry.login("device_code", "tenant-1")],
  ["getFoundryLoginStatus", "foundry.loginStatus", (c) => c.foundry.loginStatus()],
  ["foundryLogout", "foundry.logout", (c) => c.foundry.logout()],
  ["getMicrosoft365Integration", "microsoft365.status", (c) => c.microsoft365.status()],
  ["patchMicrosoft365Integration", "microsoft365.update", (c) => c.microsoft365.update({ importTeamsTranscripts: true })],
  ["connectMicrosoft365", "microsoft365.connect", (c) => c.microsoft365.connect("transcripts")],
  ["disconnectMicrosoft365", "microsoft365.disconnect", (c) => c.microsoft365.disconnect()],
  ["exportPrivacyData", "privacy.export", (c) => c.privacy.export()],
  ["deletePrivacyData", "privacy.delete", (c) => c.privacy.delete()],
];

describe("desktop control-plane client against the OpenAPI spec", () => {
  it.each(routes)("%s: %s sends the declared request with the token and returns the served body", async (operationId, _name, call) => {
    const { host, client } = newClient();
    const result = await call(client);

    expect(host.observed).toHaveLength(1);
    const request = host.observed[0] as Observed;
    expect(request.operationId).toBe(operationId);
    expect(request.headers[CONTROL_TOKEN_HEADER]).toBe("session-token");
    expect(result).toEqual(request.served);
  });

  it("sends the token on GET and on every request", async () => {
    const { host, client } = newClient();
    await client.settings.state();
    await client.recordings.list();
    await client.modes.start("assist");

    expect(host.observed.map((r) => r.method)).toEqual(["GET", "GET", "POST"]);
    expect(host.observed.every((r) => r.headers[CONTROL_TOKEN_HEADER] === "session-token")).toBe(true);
  });

  it("exposes the policy lock list from the settings state", async () => {
    const host = fakeHost();
    const original = host.impl.getMockImplementation();
    host.impl.mockImplementation(async (input, init) => {
      const response = await (original as NonNullable<typeof original>)(input, init);
      const state = (await response.json()) as Json;
      return new Response(JSON.stringify({ ...state, policyLockedFields: ["cloud.enabled", "privacy.network_scope"] }));
    });
    const { client } = newClient(host);

    await expect(client.settings.lockedFields()).resolves.toEqual(["cloud.enabled", "privacy.network_scope"]);
  });

  it("serializes list filters as the spec's query parameters", async () => {
    const { host, client } = newClient();
    await client.recordings.list({ kind: "dictation", limit: 3, includeSegments: false });

    expect(host.observed[0]?.path).toBe("/api/v1/recording-sessions?kind=dictation&limit=3&include_segments=false");
  });

  it("loads a snapshot image through fetch into a blob URL", async () => {
    const { host, client } = newClient();
    const image = await client.recordings.snapshotImage(7, 3);

    expect(image.url.startsWith("blob:")).toBe(true);
    const request = host.observed[0] as Observed;
    expect(request.operationId).toBe("getRecordingSessionSnapshotImage");
    expect(request.headers[CONTROL_TOKEN_HEADER]).toBe("session-token");
    const revoke = vi.spyOn(URL, "revokeObjectURL");
    image.revoke();
    expect(revoke).toHaveBeenCalledWith(image.url);
    revoke.mockRestore();
  });

  it("asks the privacy erasure for confirmation and the export for a ZIP", async () => {
    const { host, client } = newClient();
    await client.privacy.delete();
    const archive = await client.privacy.exportArchive();

    expect(host.observed[0]?.body).toEqual({ scope: "install", confirm: true });
    expect(host.observed[1]?.body).toEqual({ scope: "install", format: "zip" });
    expect(archive.size).toBe(3);
  });

  it("resolves the token lazily, once, and again after the host rejects it", async () => {
    const provider = vi.fn(async () => "session-token");
    const { host, client } = newClient(fakeHost(), provider);
    expect(provider).not.toHaveBeenCalled();

    await client.features();
    await client.features();
    expect(provider).toHaveBeenCalledTimes(1);

    const stale = createDesktopClient({ baseUrl: "http://127.0.0.1:1", tokenProvider: provider, fetch: host.fetch });
    provider.mockResolvedValueOnce("stale-token");
    await expect(stale.features()).rejects.toBeInstanceOf(HTTPError);
    await expect(stale.features()).resolves.toBeDefined();
    expect(provider).toHaveBeenCalledTimes(3);
  });

  it("fails closed when the provider yields no token", async () => {
    const { host, client } = newClient(fakeHost(), () => "  ");

    await expect(client.features()).rejects.toThrow(/no token/);
    expect(host.impl).not.toHaveBeenCalled();
  });

  it("surfaces non-2xx answers as HTTPError with the status", async () => {
    const { client } = newClient(fakeHost("other-token"));

    await expect(client.features()).rejects.toMatchObject({ name: "HTTPError", status: 401 });
  });
});
