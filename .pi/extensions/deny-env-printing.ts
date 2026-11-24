/**
 * Refuses the commands that print the environment (CLAUDE.md rule 3, issue #540, ADR 0044).
 *
 * The tester runs pi with --approve: pi asks for nothing, and the environment holds the agents' API keys.
 * This project-local extension, which pi loads because --approve trusts the project, hooks `tool_call`,
 * the event that sees a tool call before it runs, and blocks:
 *
 * - a bash call whose command matches any pattern of ../deny-env-printing.json;
 * - a read, grep, find or ls call whose path names /proc/<pid>/environ.
 *
 * The patterns are data, pinned by test/architecture/pienvdeny_test.go, and are written to the syntax Go's
 * RE2 and JavaScript share (no lookaround, no backreference). The extension fails closed: when the list
 * cannot be read or parsed, or a pattern does not compile, every bash call is refused with a reason that
 * says so. It is a second layer beside the rule, not a boundary: the Lima VM is the boundary (ADR 0003).
 */

import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { type ExtensionAPI, isToolCallEventType } from "@earendil-works/pi-coding-agent";

const listPath = join(dirname(fileURLToPath(import.meta.url)), "..", "deny-env-printing.json");

// The /proc file that holds a process's whole environment, as a path a file tool is given.
const procEnviron = /\/proc\/\S*environ/;

type DenyList = { reason: string; patterns: RegExp[] };

function loadDenyList(): DenyList | string {
	try {
		const doc: unknown = JSON.parse(readFileSync(listPath, "utf8"));
		if (typeof doc !== "object" || doc === null) {
			return "the list is no JSON object";
		}

		const { reason, patterns } = doc as { reason?: unknown; patterns?: unknown };
		if (typeof reason !== "string" || reason.trim() === "") {
			return "the list gives no reason";
		}

		if (!Array.isArray(patterns) || patterns.length === 0 || !patterns.every((p) => typeof p === "string")) {
			return "the list holds no patterns, or one that is no string";
		}

		return { reason, patterns: patterns.map((p: string) => new RegExp(p)) };
	} catch (err) {
		return err instanceof Error ? err.message : String(err);
	}
}

export default function (pi: ExtensionAPI) {
	const list = loadDenyList();

	pi.on("tool_call", async (event) => {
		if (isToolCallEventType("bash", event)) {
			if (typeof list === "string") {
				return {
					block: true,
					reason: `Refused: the env-printing refusal list ${listPath} could not be loaded (${list}), so every bash command is refused (CLAUDE.md, rule 3). Report this through dot-agent-deck work-done and stop.`,
				};
			}

			const command = String(event.input.command ?? "");
			if (list.patterns.some((re) => re.test(command))) {
				return { block: true, reason: list.reason };
			}

			return undefined;
		}

		if (
			isToolCallEventType("read", event) ||
			isToolCallEventType("grep", event) ||
			isToolCallEventType("find", event) ||
			isToolCallEventType("ls", event)
		) {
			const path = String((event.input as { path?: unknown }).path ?? "");
			if (procEnviron.test(path)) {
				return {
					block: true,
					reason:
						typeof list === "string"
							? "Refused: /proc/*/environ holds the environment, which holds the agents' API keys (CLAUDE.md, rule 3)."
							: list.reason,
				};
			}
		}

		return undefined;
	});
}
