/**
 * Refuses the commands that print the environment (CLAUDE.md rule 3, issue #540, ADR 0044).
 *
 * The tester runs pi with --approve: pi asks for nothing, and the environment holds the agents' API keys.
 * This project-local extension, which pi loads because --approve trusts the project, hooks `tool_call`,
 * the event that sees a tool call before it runs, and blocks:
 *
 * - a bash call whose command matches any pattern of ../deny-env-printing.json;
 * - a file-tool call (read, grep, find, ls, edit, write) whose path, normalised as pi normalises it (a
 *   leading `@`, a `file://` URL, Unicode spaces, `~`, `..`) and resolved against the working directory,
 *   is /proc or ~/.config/dot-agent-deck or lies under either, or whose path, glob or pattern names the
 *   deck's file of keys, daemon.env, or its directory.
 *
 * The patterns are data, pinned by test/architecture/pienvdeny_test.go, and are written to the syntax Go's
 * RE2 and JavaScript share (no lookaround, no backreference, no inline flag).
 *
 * What fails closed and what does not: pi logs an extension that fails to load and carries on without it,
 * so a load failure fails open. The top level therefore does nothing that can throw, and the factory never
 * throws: when the list cannot be found, read or parsed, or a pattern does not compile, every bash call is
 * refused with a reason that says so, and a handler that throws on a call refuses that call. It is a second
 * layer beside the rule, not a boundary: the Lima VM is the boundary (ADR 0003).
 *
 * The extension imports pi's package for types alone (`import type` is erased): the guard must not depend
 * at run time on the module whose behaviour it guards.
 */

import { readFileSync } from "node:fs";
import { homedir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import type { ExtensionAPI } from "@earendil-works/pi-coding-agent";

// The deck's file of the agents' keys (CLAUDE.md rule 3), refused by name wherever a file tool is pointed.
const deckKeyFile = "daemon.env";

// The file tools whose path, glob or pattern a call is refused for.
const fileTools = new Set(["read", "grep", "find", "ls", "edit", "write"]);

type DenyList = { reason: string; patterns: RegExp[] };

type Loaded = { list: DenyList; listPath: string } | { failure: string; listPath: string };

function loadDenyList(): Loaded {
	let listPath = "../deny-env-printing.json";

	try {
		listPath = join(dirname(fileURLToPath(import.meta.url)), "..", "deny-env-printing.json");

		const doc: unknown = JSON.parse(readFileSync(listPath, "utf8"));
		if (typeof doc !== "object" || doc === null) {
			return { failure: "the list is no JSON object", listPath };
		}

		const { reason, patterns } = doc as { reason?: unknown; patterns?: unknown };
		if (typeof reason !== "string" || reason.trim() === "") {
			return { failure: "the list gives no reason", listPath };
		}

		if (!Array.isArray(patterns) || patterns.length === 0 || !patterns.every((p) => typeof p === "string")) {
			return { failure: "the list holds no patterns, or one that is no string", listPath };
		}

		return { list: { reason, patterns: patterns.map((p: string) => new RegExp(p)) }, listPath };
	} catch (err) {
		return { failure: err instanceof Error ? err.message : String(err), listPath };
	}
}

// The deck's directory under the home directory, which holds the file of keys and whatever else the deck
// keeps there: refused whole, by resolved path and by name in a glob or pattern.
const deckKeyDirectory = ".config/dot-agent-deck";

// The spellings of that directory a path normalises or a glob names (`.config//dot-agent-deck`,
// `.config/./dot-agent-deck`, `.config/dot-agent*`), refused in a glob or pattern as the list refuses
// them in a bash command.
const deckDirectorySpelling = /\.config[/.]*\/dot-agent/;

// The Unicode spaces pi maps to a plain space before it reads a path.
const unicodeSpaces = /[  -   　]/g;

// resolvePath resolves a file tool's path as pi's resolveToCwd and normalizePath do: one leading `@`
// (pi's shorthand for a path) is dropped, a `file://` URL becomes its path, the Unicode spaces become
// plain ones, a leading `~` is the home directory, a relative path is under the working directory and
// `..` is normalised. A URL that does not convert throws, and the handler refuses the call: a fallback
// to the raw value would resolve `file:///proc/...` as a relative path and allow it.
function resolvePath(path: string, cwd: string): string {
	let expanded = path.startsWith("@") ? path.slice(1) : path;
	if (expanded.startsWith("file://")) {
		expanded = fileURLToPath(expanded);
	}

	expanded = expanded.replace(unicodeSpaces, " ");
	if (expanded === "~" || expanded.startsWith("~/")) {
		expanded = homedir() + expanded.slice(1);
	}

	return resolve(cwd, expanded);
}

// under tells whether absolute is directory or below it.
function under(absolute: string, directory: string): boolean {
	return absolute === directory || absolute.startsWith(`${directory}/`);
}

const procReason =
	"Refused: /proc holds the environment of every process, which holds the agents' API keys (CLAUDE.md, rule 3).";

const keyFileReason = `Refused: ${deckKeyFile} and ~/${deckKeyDirectory} hold the agents' API keys; never read them (CLAUDE.md, rule 3).`;

const handlerFailureReason =
	"Refused: the env-printing refusal could not check this call, so it is refused (CLAUDE.md, rule 3). Report this through dot-agent-deck work-done and stop.";

export default function (pi: ExtensionAPI) {
	const loaded = loadDenyList();

	pi.on("session_start", async (_event, ctx) => {
		try {
			if (ctx.hasUI) {
				ctx.ui.notify(
					"failure" in loaded
						? "env-printing refusal: the list could not be loaded, every bash command is refused"
						: "env-printing refusal loaded",
					"failure" in loaded ? "error" : "info",
				);
			}
		} catch {
			// A notice that cannot be shown changes nothing the refusal does.
		}
	});

	pi.on("tool_call", async (event, ctx) => {
		try {
			const input = (event.input ?? {}) as Record<string, unknown>;

			if (event.toolName === "bash") {
				if ("failure" in loaded) {
					return {
						block: true,
						reason: `Refused: the env-printing refusal list ${loaded.listPath} could not be loaded (${loaded.failure}), so every bash command is refused (CLAUDE.md, rule 3). Report this through dot-agent-deck work-done and stop.`,
					};
				}

				const command = String(input.command ?? "");
				if (loaded.list.patterns.some((re) => re.test(command))) {
					return { block: true, reason: loaded.list.reason };
				}

				return undefined;
			}

			if (fileTools.has(event.toolName)) {
				const cwd = typeof ctx?.cwd === "string" ? ctx.cwd : process.cwd();
				const keyDirectory = join(homedir(), deckKeyDirectory);

				for (const field of ["path", "glob", "pattern"]) {
					const value = input[field];
					if (typeof value !== "string") {
						continue;
					}

					if (value.includes(deckKeyFile) || value.includes(deckKeyDirectory) || deckDirectorySpelling.test(value)) {
						return { block: true, reason: keyFileReason };
					}

					if (field === "path") {
						const absolute = resolvePath(value, cwd);

						if (under(absolute, keyDirectory)) {
							return { block: true, reason: keyFileReason };
						}

						if (under(absolute, "/proc")) {
							return { block: true, reason: procReason };
						}
					} else if (value.includes("/proc")) {
						return { block: true, reason: procReason };
					}
				}
			}

			return undefined;
		} catch {
			return { block: true, reason: handlerFailureReason };
		}
	});
}
