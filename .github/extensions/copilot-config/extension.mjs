// Extension: copilot-config
// A Copilot CLI canvas to view, browse, and reconfigure gh-copilot-config
// profiles directly inside the Copilot app. It boots a loopback HTTP server
// per open instance, shells out to the `copilot-config` binary with --json,
// and streams state to the iframe over SSE.

import http from "node:http";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { readFile } from "node:fs/promises";
import { existsSync } from "node:fs";
import { execFile } from "node:child_process";

import { CanvasError, createCanvas, joinSession } from "@github/copilot-sdk/extension";

const __dirname = dirname(fileURLToPath(import.meta.url));
// .github/extensions/copilot-config -> repo root
const REPO_ROOT = join(__dirname, "..", "..", "..");

// Diff patches can be tens of MB; never let one blow up the iframe.
const DIFF_CAP = 200_000;
const MAX_BUFFER = 256 * 1024 * 1024;

// instanceId -> { server, url, state, subscribers }
const instances = new Map();

let logger = null;
function log(msg, level = "info") {
	try {
		logger?.(msg, { level, ephemeral: level === "debug" });
	} catch {}
}

// Resolve how to invoke the CLI: explicit env, then repo-local dev build,
// then the installed `gh copilot-config` extension.
function resolveBin() {
	const fromEnv = process.env.COPILOT_CONFIG_BIN;
	if (fromEnv && existsSync(fromEnv)) return { cmd: fromEnv, base: [] };
	const local = join(REPO_ROOT, "gh-copilot-config");
	if (existsSync(local)) return { cmd: local, base: [] };
	return { cmd: "gh", base: ["copilot-config"] };
}

function run(args, { json = false } = {}) {
	const { cmd, base } = resolveBin();
	const argv = [...base, ...args];
	const env = { ...process.env, CC_NO_TUI: "1" };
	return new Promise((resolve, reject) => {
		execFile(cmd, argv, { env, maxBuffer: MAX_BUFFER }, (err, stdout, stderr) => {
			if (err) {
				const detail = (stderr || stdout || err.message || "").toString().trim();
				reject(new CanvasError("cli_error", `\`${cmd} ${argv.join(" ")}\` failed: ${detail}`));
				return;
			}
			if (!json) {
				resolve((stdout || "").toString());
				return;
			}
			try {
				resolve(JSON.parse((stdout || "").toString() || "null"));
			} catch (e) {
				reject(new CanvasError("cli_parse_error", `Could not parse JSON from \`${argv.join(" ")}\`: ${e.message}`));
			}
		});
	});
}

async function loadState() {
	const [list, status] = await Promise.all([
		run(["list", "--json"], { json: true }),
		run(["status", "--json"], { json: true }),
	]);
	return { list, status, error: null, updatedAt: Date.now() };
}

function broadcast(entry) {
	const payload = `data: ${JSON.stringify(entry.state)}\n\n`;
	for (const res of entry.subscribers) {
		try {
			res.write(payload);
		} catch {}
	}
}

async function refresh(entry) {
	try {
		const next = await loadState();
		entry.state = { ...entry.state, ...next };
	} catch (err) {
		entry.state = { ...entry.state, error: err.message ?? String(err), updatedAt: Date.now() };
	}
	broadcast(entry);
	return entry.state;
}

function readBody(req) {
	return new Promise((resolve) => {
		let data = "";
		req.on("data", (c) => (data += c));
		req.on("end", () => {
			try {
				resolve(data ? JSON.parse(data) : {});
			} catch {
				resolve({});
			}
		});
	});
}

function sendJson(res, code, body) {
	res.writeHead(code, { "Content-Type": "application/json; charset=utf-8" });
	res.end(JSON.stringify(body));
}

// POST /api/* handlers. Mutations return the refreshed state; reads return data.
async function handleApi(entry, path, body) {
	switch (path) {
		case "/api/refresh":
			return refresh(entry);
		case "/api/inspect": {
			const name = body.name;
			const args = name ? ["inspect", name, "--json"] : ["inspect", "--json"];
			return run(args, { json: true });
		}
		case "/api/diff": {
			const name = body.name;
			const args = name ? ["diff", name, "--json"] : ["diff", "--json"];
			const out = await run(args, { json: true });
			if (out && typeof out.patch === "string" && out.patch.length > DIFF_CAP) {
				out.patch = out.patch.slice(0, DIFF_CAP);
				out.truncated = true;
			}
			return out;
		}
		case "/api/apply":
			requireName(body);
			await run(["apply", body.name, "--force"]);
			return refresh(entry);
		case "/api/on":
			await run(body.name ? ["on", body.name, "--force"] : ["on", "--force"]);
			return refresh(entry);
		case "/api/clean":
			await run(["clean", "--force"]);
			return refresh(entry);
		case "/api/save":
			requireName(body);
			await run(["save", body.name, "--force"]);
			return refresh(entry);
		case "/api/new": {
			requireName(body);
			const args = ["new", body.name];
			if (body.from) args.push("--from", body.from);
			await run(args);
			return refresh(entry);
		}
		case "/api/rm":
			requireName(body);
			await run(["rm", body.name, "--force"]);
			return refresh(entry);
		default:
			return null;
	}
}

function requireName(body) {
	if (!body || typeof body.name !== "string" || !body.name.trim()) {
		throw new CanvasError("input_invalid", "A non-empty `name` is required.");
	}
}

async function startServer() {
	const subscribers = new Set();
	const entry = { server: null, url: null, subscribers, state: { list: null, status: null, error: null, updatedAt: null } };

	const server = http.createServer(async (req, res) => {
		try {
			if (req.method === "GET" && (req.url === "/" || req.url === "/index.html")) {
				const html = await readFile(join(__dirname, "index.html"));
				res.writeHead(200, { "Content-Type": "text/html; charset=utf-8" });
				res.end(html);
				return;
			}
			if (req.method === "GET" && req.url === "/events") {
				res.writeHead(200, {
					"Content-Type": "text/event-stream",
					"Cache-Control": "no-cache",
					Connection: "keep-alive",
				});
				res.write(`data: ${JSON.stringify(entry.state)}\n\n`);
				subscribers.add(res);
				req.on("close", () => subscribers.delete(res));
				return;
			}
			if (req.method === "POST" && req.url?.startsWith("/api/")) {
				const body = await readBody(req);
				const result = await handleApi(entry, req.url, body);
				if (result === null) {
					sendJson(res, 404, { error: "unknown endpoint" });
					return;
				}
				sendJson(res, 200, result);
				return;
			}
			res.writeHead(404);
			res.end();
		} catch (err) {
			const message = err instanceof Error ? err.message : String(err);
			log(`request error: ${message}`, "error");
			sendJson(res, 500, { error: message });
		}
	});

	await new Promise((r) => server.listen(0, "127.0.0.1", r));
	const { port } = server.address();
	entry.server = server;
	entry.url = `http://127.0.0.1:${port}/`;
	await refresh(entry);
	return entry;
}

function getEntry(instanceId) {
	const entry = instances.get(instanceId);
	if (!entry) {
		throw new CanvasError("canvas_instance_not_found", `No open canvas for instanceId=${instanceId}. Call open_canvas first.`);
	}
	return entry;
}

const nameInput = {
	type: "object",
	properties: { name: { type: "string", description: "Profile name." } },
	required: ["name"],
};

const canvas = createCanvas({
	id: "copilot-config",
	displayName: "Copilot Config",
	description:
		"View, browse, and reconfigure gh-copilot-config profiles. Lists profiles, shows active/drift status, inspects per-surface inventory, and applies/cleans/saves config.",
	actions: [
		{
			name: "refresh",
			description: "Reload the profile list and active/drift status.",
			handler: async ({ instanceId }) => refresh(getEntry(instanceId)),
		},
		{
			name: "apply_profile",
			description: "Apply a named profile to the live Copilot config.",
			inputSchema: nameInput,
			handler: async ({ instanceId, input }) => {
				requireName(input);
				await run(["apply", input.name, "--force"]);
				return refresh(getEntry(instanceId));
			},
		},
		{
			name: "inspect_profile",
			description: "Return a profile's inventory grouped by surface and feature.",
			inputSchema: {
				type: "object",
				properties: { name: { type: "string", description: "Profile name (default: active)." } },
			},
			handler: async ({ input }) => {
				const name = input?.name;
				const args = name ? ["inspect", name, "--json"] : ["inspect", "--json"];
				return run(args, { json: true });
			},
		},
		{
			name: "clean",
			description: "Reset live Copilot config to vanilla (apply the empty 'clean' profile).",
			handler: async ({ instanceId }) => {
				await run(["clean", "--force"]);
				return refresh(getEntry(instanceId));
			},
		},
	],
	open: async ({ instanceId }) => {
		let entry = instances.get(instanceId);
		if (!entry) {
			entry = await startServer();
			instances.set(instanceId, entry);
		}
		const active = entry.state?.status?.active ?? null;
		return {
			url: entry.url,
			title: "Copilot Config",
			status: active ? `Active: ${active}` : "No active profile",
		};
	},
	onClose: async ({ instanceId }) => {
		const entry = instances.get(instanceId);
		if (!entry) return;
		instances.delete(instanceId);
		for (const res of entry.subscribers) {
			try {
				res.end();
			} catch {}
		}
		await new Promise((r) => entry.server.close(() => r()));
	},
});

const session = await joinSession({ canvases: [canvas] });
logger = (msg, opts) => session.log(msg, opts);
log("copilot-config canvas ready", "debug");
