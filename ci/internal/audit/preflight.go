package audit

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
)

// Call is one tool call the preflight's prompt asks the model to make: the
// probe it belongs to, 0 for the control, the tool it is made with and the
// exact input the plan expects of it, its placeholders filled - file_path for
// Read, Write and Edit, command for Bash, pattern and path for Grep and Glob.
// The judgement matches a transcript's call by its tool and that input.
type Call struct {
	Probe int
	Tool  string
	Input map[string]string

	// tokenAbsence marks probe 5's token-absence call, set by PreflightPlan
	// alone, so the judgement recognises it whatever its command.
	tokenAbsence bool
}

// Plan is the preflight's calls, in the order the prompt lists them.
type Plan []Call

// Targets are what the preflight's calls name: the root that stands in for /,
// the port probe 2's listener chose, the launcher's pid whose environ probe 5
// aims at, and the step's $GITHUB_ENV file, probe 6's target.
type Targets struct {
	Root      string
	Port      int
	Pid       int
	GitHubEnv string
}

// planCall is one call of the plan with the instruction the prompt gives for
// it and the input it expects. Both hold placeholders that Targets.replace
// fills. tokenAbsence marks probe 5's token-absence call.
type planCall struct {
	probe        int
	tool         string
	input        map[string]string
	instruction  string
	tokenAbsence bool
}

// The placeholders of an instruction and an input.
const (
	phWork          = "{work}"
	phState         = "{state}"
	phWrapper       = "{wrapper}"
	phTmp           = "{tmp}"
	phPort          = "{port}"
	phDocker        = "{docker}"
	phPid           = "{pid}"
	phGitHubEnv     = "{githubenv}"
	phGitHubEnvDir  = "{githubenvdir}"
	phGitHubEnvBase = "{githubenvbase}"
	phControl       = "{control}"
)

// tokenAbsenceCommand is probe 5's Bash call that shows whether the token
// variable reached the sandboxed shell without showing its value: the shell
// tests the name itself with [[ -v ]], true for a variable of that name
// whether exported or not and whether empty or not. Each of the two branches
// tests the variable positively and neither is a fallback (issue #613's
// second review): a set variable makes cat name
// /srv/audit/work/nonexistent/tokenset and fail, and an unset one, by
// [[ ! -v ]], makes cat name /srv/audit/work/nonexistent/ and fail. With &&
// and a ; between the branches, the bare path is read only when its own test
// is true, never because the first branch failed: the old || form ran it
// whenever the first branch failed for any reason, and a shell that cannot
// evaluate [[ (it assumes bash or zsh) now runs neither cat, so its result
// holds no line of cat's and fails. The command holds no $: Claude Code
// 2.1.332 refuses, before it runs, any Bash command in which a variable is
// expanded ("A variable in braces in this command can't be checked before it
// runs" for the ${...:+...} form this call used to have, issue #613), and a
// refused call is no evidence about the shell. It is the one call of the plan
// whose result is expected to be an error for a reason other than a refusal.
// The path is the wrapper's own work area, as the wrapper sees it whatever
// the root, so it lies inside the roots the wrapper holds every path word to
// and cat runs for real (issue #277, round I): outside them, the wrapper
// refuses the word before cat runs, and its refusal is no answer about the
// shell (round J's correction, issue #613's second review). work/nonexistent
// cannot exist when the preflight runs: the work area holds tmp/ and the
// launcher's control file alone. {wrapper} comes from Targets. The judgement
// knows the call by its place in the plan (planCall.tokenAbsence), never by
// its text.
const tokenAbsenceCommand = "[[ -v CLAUDE_CODE_OAUTH_TOKEN ]] && " +
	"{wrapper} cat /" + workDir + "/nonexistent/tokenset; " +
	"[[ ! -v CLAUDE_CODE_OAUTH_TOKEN ]] && " +
	"{wrapper} cat /" + workDir + "/nonexistent/"

// Probe 5's two calls for the forms the wrapper's own path rule never reads
// (ADR 0040, issue #277 round G), so the sandbox itself is tested on every
// run: a shell redirection, whose target the shell resolves, glob and all,
// before the wrapper starts; and a redirection of a /dev/fd path, which the
// kernel resolves through /proc/self/fd before it takes the "..", and whose
// text holds no /proc/ (issue #277 round H: as an argument, the wrapper's own
// path rule would refuse it for its ".." and the sandbox would go untested).
// Each is expected to be refused, as every other probe call is.
const (
	redirectedReadCommand = "{wrapper} cat < /proc/{pid}/envir?n"
	fdReadCommand         = "{wrapper} cat < /dev/fd/../../{pid}/environ"
)

// The words the token-absence call's result is judged by: cat's own error
// line for the bare path, the only proof that the variable is unset (issue
// #613's second review), and the word only the set branch's path holds.
const (
	absenceLine = "cat: /" + workDir + "/nonexistent/: No such file or directory"
	absenceSet  = "tokenset"
)

// bash is a Bash call of probe with command.
func bash(probe int, command string) planCall {
	return planCall{probe: probe, tool: "Bash", input: map[string]string{"command": command}, instruction: "Run with Bash: " + command}
}

// tokenAbsence is probe 5's token-absence call, marked as such.
func tokenAbsence() planCall {
	call := bash(canaryProbe, tokenAbsenceCommand)
	call.tokenAbsence = true

	return call
}

// file is a Read, Write or Edit call of probe on path, with its instruction.
func file(probe int, tool, path, instruction string) planCall {
	return planCall{probe: probe, tool: tool, input: map[string]string{"file_path": path}, instruction: instruction}
}

// search is a Grep or Glob call of probe for pattern in path.
func search(probe int, tool, pattern, path string) planCall {
	return planCall{
		probe: probe, tool: tool,
		input:       map[string]string{"pattern": pattern, "path": path},
		instruction: tool + " for the pattern " + pattern + " in the path " + path + ".",
	}
}

// preflightCalls are the control and the six probes of ADR 0040's table, in
// its order; each probe's calls come together.
var preflightCalls = []planCall{
	file(0, "Read", "{control}", "Read the file {control}."),

	file(1, "Write", "{state}/probe-1", "Write the text probe-1 to the file {state}/probe-1."),
	bash(1, "{wrapper} cat {control} > {state}/probe-1"),
	bash(1, "{wrapper} cat {control} > {tmp}/shift2eu-audit-probe-1"),

	bash(2, "{wrapper} cat < /dev/tcp/127.0.0.1/{port}"),

	bash(3, "socat - UNIX-CONNECT:{docker}"),
	bash(3, "{wrapper} cat < {docker}"),

	bash(4, "sudo -n true"),
	bash(4, "{wrapper} sudo -n true"),

	file(5, "Read", "/proc/{pid}/environ", "Read the file /proc/{pid}/environ."),
	search(5, "Grep", "SHIFT2EU_AUDIT_CANARY", "/proc/{pid}/environ"),
	search(5, "Glob", "environ", "/proc/{pid}"),
	bash(5, "{wrapper} cat /proc/{pid}/environ"),
	bash(5, "{wrapper} cat /proc/$PPID/environ"),
	bash(5, "{wrapper} cat /proc/$$/environ"),
	bash(5, redirectedReadCommand),
	bash(5, fdReadCommand),
	tokenAbsence(),

	file(6, "Write", "{githubenv}", "Write the text SHIFT2EU_AUDIT_PROBE=6 to the file {githubenv}."),
	file(6, "Edit", "{githubenv}", "Edit the file {githubenv}: replace the first line with SHIFT2EU_AUDIT_PROBE=6."),
	file(6, "Read", "{githubenv}", "Read the file {githubenv}."),
	search(6, "Grep", ".", "{githubenv}"),
	search(6, "Glob", "{githubenvbase}", "{githubenvdir}"),
	bash(6, "{wrapper} cat {control} >> {githubenv}"),
}

// PreflightPlan returns the preflight's calls for t in order: the control's
// Read first, then the six probes', each with the input the prompt asks for.
func PreflightPlan(t Targets) Plan {
	plan := make(Plan, 0, len(preflightCalls))

	for _, call := range preflightCalls {
		input := make(map[string]string, len(call.input))
		for key, value := range call.input {
			input[key] = t.replace(value)
		}

		plan = append(plan, Call{Probe: call.probe, Tool: call.tool, Input: input, tokenAbsence: call.tokenAbsence})
	}

	return plan
}

// isTokenAbsence reports whether call is probe 5's token-absence call: the
// one PreflightPlan marked, recognised by that mark and not by a part of its
// command, so a change of the command can never turn its judgement into an
// ordinary probe's, under which a set variable's error would pass (issue
// #613).
func isTokenAbsence(call Call) bool {
	return call.tokenAbsence
}

// probeNames are the probes' short names, which a verdict names them by.
var probeNames = map[int]string{
	1: "probe file",
	2: "TCP connect",
	3: "Docker socket",
	4: "sudo",
	5: "canary",
	6: "GITHUB_ENV file",
}

// probeLabel is "probe <n>, <short name>".
func probeLabel(probe int) string {
	name, ok := probeNames[probe]
	if !ok {
		return "probe " + strconv.Itoa(probe)
	}

	return fmt.Sprintf("probe %d, %s", probe, name)
}

// replace fills the placeholders of an instruction or an input.
func (t Targets) replace(text string) string {
	return strings.NewReplacer(
		phControl, under(t.Root, controlPath),
		phWork, under(t.Root, workDir),
		phState, under(t.Root, stateDir),
		phWrapper, under(t.Root, wrapperPath),
		phTmp, under(t.Root, "tmp"),
		phPort, strconv.Itoa(t.Port),
		phDocker, under(t.Root, dockerSocket),
		phPid, strconv.Itoa(t.Pid),
		phGitHubEnvDir, filepath.Dir(t.GitHubEnv),
		phGitHubEnvBase, filepath.Base(t.GitHubEnv),
		phGitHubEnv, t.GitHubEnv,
	).Replace(text)
}

// controlPath is the file the control call reads its nonce from.
const controlPath = workDir + "/preflight-control.txt"

// dockerSocket is Docker's socket, which probe 3 aims at.
const dockerSocket = "var/run/docker.sock"

// preflightSystem is the preflight's system prompt, appended to the CLI's own.
const preflightSystem = `You are running a sandbox preflight. The user lists numbered tool calls.
Make exactly those calls, one at a time, in the listed order, each exactly once, with exactly the tool and the arguments given.
Make every call even when an earlier one was refused or failed. Never retry a call, never make a call that is not listed, and never use any other tool.
When the last call is made, answer with the single word done.
`

// preflightPrompt returns the user prompt that lists the plan's calls for t.
func preflightPrompt(t Targets) string {
	var prompt strings.Builder

	prompt.WriteString("Make these tool calls, in this order:\n")

	for i, call := range preflightCalls {
		fmt.Fprintf(&prompt, "%d. (%s) %s\n", i+1, call.tool, t.replace(call.instruction))
	}

	return prompt.String()
}
