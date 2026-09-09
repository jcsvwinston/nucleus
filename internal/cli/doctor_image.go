// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jcsvwinston/nucleus/pkg/app"
)

// checkImage reads the project's Dockerfile and reports the container
// settings that are easy to get wrong and expensive to get wrong.
//
// It judges the file, never a registry: `doctor` stays offline, so the
// evidence is the text on disk. That bounds what it can say — it cannot
// know which user a base image declares, nor whether a tag still points at
// the bytes that were reviewed — and every finding below is worded from
// what the file itself proves.
//
// A project with no Dockerfile is not a finding. Plenty of Nucleus
// applications deploy as a binary under systemd (see the deployment
// guide), and a check that answers DEGRADED for them is a check operators
// learn to skip.
func checkImage(_ *app.Config, configPath string) doctorCheckOutcome {
	dir := "."
	if p := strings.TrimSpace(configPath); p != "" {
		dir = filepath.Dir(p)
	}
	path := filepath.Join(dir, "Dockerfile")

	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return doctorInfo(fmt.Sprintf("No Dockerfile in %s; nothing to inspect (an application deployed as a binary needs none, and `nucleus new` writes one for the projects that do)", dir))
		}
		return doctorError(fmt.Sprintf("Dockerfile at %s cannot be read", path), err)
	}

	global, stages := dockerStages(parseDockerfile(string(raw)))
	if len(stages) == 0 {
		return doctorError(fmt.Sprintf("Dockerfile at %s has no FROM instruction, so it builds nothing", path), nil)
	}

	var errs, warns []string
	final := stages[len(stages)-1]

	// Who the process is. A container that runs as uid 0 turns a bug in a
	// handler into root inside the namespace, and root inside the
	// namespace is the starting position for every escape a kernel bug
	// makes possible. Declaring the user is also what an orchestrator
	// enforcing runAsNonRoot reads.
	switch user, ok := dockerFinalUser(final); {
	case ok && dockerUserIsRoot(user):
		errs = append(errs, fmt.Sprintf("the final stage runs as %q — a process compromised in this container is root inside it; add `USER 65532:65532` (a numeric id an orchestrator can enforce without resolving a name)", user))
	case !ok && dockerBaseLooksNonRoot(final.base):
		// The base tag says nonroot, so the image most likely does not run
		// as uid 0 — but that is the base image's promise, not this file's.
		warns = append(warns, fmt.Sprintf("the final stage declares no USER and inherits it from %s; add `USER 65532:65532` so the file says who the process is and a base image change cannot quietly return it to root", final.base))
	case !ok:
		errs = append(errs, fmt.Sprintf("the final stage declares no USER, so the container runs as root (nothing in %s lowers it); add `USER 65532:65532` after the COPY instructions", filepath.Base(path)))
	}

	// What ships. A tag is a name the registry can repoint at other bytes;
	// a digest is the bytes. Without one, "the image we reviewed" and "the
	// image we deploy" are two different claims.
	if ref := final.base; !dockerRefIsInternal(ref, stages) && !dockerRefIsPinned(ref) {
		warns = append(warns, fmt.Sprintf("the final stage's base %s is a mutable tag, so a rebuild can ship different bytes than the ones that were reviewed; pin it as `%s@sha256:<digest>` (docker buildx imagetools inspect %s prints the digest)", ref, ref, ref))
	}

	// A single stage means the compiler, the module cache and the source
	// tree are in the image that faces the network.
	if len(stages) == 1 {
		warns = append(warns, "this is a single-stage build, so the Go toolchain, the module cache and the source tree ship in the runtime image; add a second `FROM` and copy only the binary into it with `COPY --from=`")
	} else if src, ok := dockerCopiesWholeContext(final); ok {
		errs = append(errs, fmt.Sprintf("the final stage copies the whole build context (`%s`), so the source tree — and anything else sitting in the directory — ships in the runtime image; copy the build output instead, with `COPY --from=<stage> <artifact> <dest>`", src))
	}

	// cgo. Every driver module Nucleus publishes is pure Go, so a Nucleus
	// binary has no reason to link libc — and a dynamically linked binary
	// does not run on a base that carries none.
	if reason, ok := dockerCgoLeftOn(stages); ok {
		warns = append(warns, fmt.Sprintf("%s; every database driver Nucleus publishes is pure Go, so set `CGO_ENABLED=0` on the build to produce a static binary that runs on a base image with no libc", reason))
	}

	// Secrets. ENV survives in the image configuration and ARG defaults
	// survive in the build history: both are readable by anyone who can
	// pull the image, and deleting the line later does not remove the
	// layer that carries it.
	errs = append(errs, dockerBakedSecrets(global, stages)...)

	if _, err := os.Stat(filepath.Join(dir, ".dockerignore")); os.IsNotExist(err) {
		warns = append(warns, fmt.Sprintf("no .dockerignore next to the Dockerfile, so the whole of %s is the build context — .git (every value ever committed, including the ones a later commit removed) and a local .env included; add one that excludes them", dir))
	}

	sort.Strings(errs)
	sort.Strings(warns)

	switch {
	case len(errs) > 0:
		return doctorError(fmt.Sprintf("%d high-risk container setting(s) in %s: %s", len(errs), path, strings.Join(append(errs, warns...), " | ")), nil)
	case len(warns) > 0:
		return doctorWarning(fmt.Sprintf("%d container setting(s) to review in %s: %s", len(warns), path, strings.Join(warns, " | ")))
	}

	return doctorPass(fmt.Sprintf("%s: %d-stage build, runtime base pinned by digest, an explicit non-root USER, no whole-context COPY into the runtime stage and no literal secrets in ENV/ARG", path, len(stages)))
}

// dockerInstruction is one logical line of a Dockerfile: the verb upper-cased,
// the rest of the line with continuations joined, and where it started.
type dockerInstruction struct {
	verb string
	args string
	line int
}

// dockerStage is one FROM and the instructions that follow it.
type dockerStage struct {
	base   string
	alias  string
	instrs []dockerInstruction
}

// parseDockerfile splits a Dockerfile into logical instructions. It is not a
// builder: it joins backslash continuations and drops whole-line comments —
// which are the only comments a Dockerfile has, a `#` inside an instruction
// being literal text — and leaves the arguments as written, because every
// judgement below reads them as the operator wrote them.
func parseDockerfile(src string) []dockerInstruction {
	var out []dockerInstruction
	var buf strings.Builder
	start, continued := 0, false

	emit := func() {
		text := strings.TrimSpace(buf.String())
		buf.Reset()
		if text == "" {
			return
		}
		verb, args, _ := strings.Cut(text, " ")
		out = append(out, dockerInstruction{verb: strings.ToUpper(verb), args: strings.TrimSpace(args), line: start})
	}

	for n, rawLine := range strings.Split(src, "\n") {
		line := strings.TrimSpace(rawLine)
		if strings.HasPrefix(line, "#") {
			// A comment line does not interrupt a continuation.
			continue
		}
		if line == "" && !continued {
			continue
		}
		if !continued {
			start = n + 1
		} else if buf.Len() > 0 {
			buf.WriteByte(' ')
		}
		if strings.HasSuffix(line, "\\") {
			buf.WriteString(strings.TrimSpace(strings.TrimSuffix(line, "\\")))
			continued = true
			continue
		}
		buf.WriteString(line)
		continued = false
		emit()
	}
	emit()
	return out
}

// dockerStages splits the instruction list at each FROM: the instructions
// before the first one (global ARGs) and one entry per stage.
func dockerStages(instrs []dockerInstruction) (global []dockerInstruction, stages []dockerStage) {
	for _, in := range instrs {
		if in.verb != "FROM" {
			if len(stages) == 0 {
				global = append(global, in)
				continue
			}
			stages[len(stages)-1].instrs = append(stages[len(stages)-1].instrs, in)
			continue
		}
		base, alias := parseDockerFrom(in.args)
		stages = append(stages, dockerStage{base: base, alias: alias})
	}
	return global, stages
}

// parseDockerFrom reads the image reference and the AS alias out of a FROM,
// skipping the flags (--platform) that may precede the reference.
func parseDockerFrom(args string) (base, alias string) {
	fields := strings.Fields(args)
	for i := 0; i < len(fields); i++ {
		f := fields[i]
		if strings.HasPrefix(f, "--") {
			continue
		}
		if base == "" {
			base = f
			continue
		}
		if strings.EqualFold(f, "AS") && i+1 < len(fields) {
			alias = fields[i+1]
			break
		}
	}
	return base, alias
}

// dockerFinalUser returns the user the last USER of a stage sets, and
// whether the stage sets one at all.
func dockerFinalUser(stage dockerStage) (string, bool) {
	user, found := "", false
	for _, in := range stage.instrs {
		if in.verb == "USER" {
			user, found = strings.Trim(strings.TrimSpace(in.args), `"'`), true
		}
	}
	return user, found
}

// dockerUserIsRoot reports whether a USER value is uid 0, by either spelling.
func dockerUserIsRoot(user string) bool {
	name, _, _ := strings.Cut(user, ":")
	name = strings.TrimSpace(name)
	return name == "root" || name == "0"
}

// dockerBaseLooksNonRoot reports whether the base image's tag claims a
// non-root default user, which is what the distroless and Chainguard
// images spell in the tag itself. It is the base image's claim, not this
// file's, which is why it only softens a finding and never removes one.
func dockerBaseLooksNonRoot(ref string) bool {
	lower := strings.ToLower(ref)
	tag, _, _ := strings.Cut(lower, "@")
	return strings.Contains(tag, ":nonroot") || strings.Contains(tag, "-nonroot")
}

// dockerRefIsPinned reports whether an image reference names a digest.
// A reference built from a build argument is not judged: what it resolves
// to is the caller's, and guessing would produce a finding the file cannot
// support.
func dockerRefIsPinned(ref string) bool {
	return strings.Contains(ref, "@sha256:") || strings.Contains(ref, "$")
}

// dockerRefIsInternal reports whether a FROM names something that is not
// pulled from a registry: `scratch`, or an earlier stage of this same file.
func dockerRefIsInternal(ref string, stages []dockerStage) bool {
	if strings.EqualFold(ref, "scratch") {
		return true
	}
	for _, s := range stages {
		if s.alias != "" && strings.EqualFold(s.alias, ref) {
			return true
		}
	}
	return false
}

// dockerCopiesWholeContext reports a COPY or ADD in the stage that takes
// the whole build context — the runtime half of a multi-stage build has a
// build stage to copy from and no reason to reach for the context.
func dockerCopiesWholeContext(stage dockerStage) (string, bool) {
	for _, in := range stage.instrs {
		if in.verb != "COPY" && in.verb != "ADD" {
			continue
		}
		fields := strings.Fields(in.args)
		fromStage := false
		var sources []string
		for i, f := range fields {
			if strings.HasPrefix(f, "--") {
				if strings.HasPrefix(strings.ToLower(f), "--from=") {
					fromStage = true
				}
				continue
			}
			if i == len(fields)-1 {
				break // the destination
			}
			sources = append(sources, strings.Trim(f, `"'`))
		}
		if fromStage {
			continue
		}
		for _, src := range sources {
			if src == "." || src == "./" || src == "/" {
				return in.verb + " " + in.args, true
			}
		}
	}
	return "", false
}

// dockerCgoLeftOn reports whether a Go build in the file compiles with cgo
// available. CGO_ENABLED reaches the compiler either on the RUN line or
// through an ENV/ARG earlier in the same stage, so both are read.
func dockerCgoLeftOn(stages []dockerStage) (string, bool) {
	for _, stage := range stages {
		stageValue := ""
		for _, in := range stage.instrs {
			switch in.verb {
			case "ENV", "ARG":
				if v, ok := dockerAssignment(in.args, "CGO_ENABLED"); ok {
					stageValue = v
				}
			case "RUN":
				if !dockerRunsGoBuild(in.args) {
					continue
				}
				value := stageValue
				if v, ok := dockerAssignment(in.args, "CGO_ENABLED"); ok {
					value = v
				}
				switch value {
				case "0":
					continue
				case "":
					return fmt.Sprintf("the Go build on line %d does not set CGO_ENABLED, so the toolchain's default applies and the binary links libc as soon as a dependency reaches for cgo", in.line), true
				default:
					return fmt.Sprintf("the Go build on line %d runs with CGO_ENABLED=%s, so the binary is dynamically linked and needs a matching libc in the runtime image", in.line, value), true
				}
			}
		}
	}
	return "", false
}

// dockerRunsGoBuild reports whether a RUN line compiles Go code.
func dockerRunsGoBuild(args string) bool {
	return strings.Contains(args, "go build") || strings.Contains(args, "go install")
}

// dockerAssignment reads NAME=value out of an instruction's arguments,
// whether it is an ENV/ARG line or an inline assignment in front of a
// command. It returns the value with surrounding quotes removed.
func dockerAssignment(args, name string) (string, bool) {
	for _, f := range strings.Fields(args) {
		key, value, ok := strings.Cut(f, "=")
		if ok && strings.EqualFold(strings.TrimSpace(key), name) {
			return strings.Trim(value, `"'`), true
		}
	}
	return "", false
}

// dockerSecretNames are the substrings that mark an environment variable as
// carrying a credential. Matching on the name is deliberate: judging the
// value would mean guessing what a secret looks like, and would flag every
// password-shaped string that is not one.
var dockerSecretNames = []string{"SECRET", "PASSWORD", "PASSWD", "TOKEN", "APIKEY", "API_KEY", "ACCESS_KEY", "PRIVATE_KEY", "CREDENTIAL"}

// dockerBakedSecrets reports ENV and ARG instructions that give a
// credential-shaped variable a literal value.
//
// It reads every stage, not just the last: an ARG default is in the build
// history and an ENV is in the layer, and neither is removed by a later
// instruction that unsets it. A value that only references another variable
// ($DB_PASSWORD) is not a finding — it names where the value comes from
// without carrying one.
func dockerBakedSecrets(global []dockerInstruction, stages []dockerStage) []string {
	var findings []string
	seen := map[string]bool{}

	inspect := func(in dockerInstruction) {
		if in.verb != "ENV" && in.verb != "ARG" {
			return
		}
		for _, f := range strings.Fields(in.args) {
			key, value, ok := strings.Cut(f, "=")
			if !ok {
				continue
			}
			key = strings.TrimSpace(key)
			value = strings.Trim(strings.TrimSpace(value), `"'`)
			if value == "" || strings.Contains(value, "$") {
				continue
			}
			upper := strings.ToUpper(key)
			secret := false
			for _, marker := range dockerSecretNames {
				if strings.Contains(upper, marker) {
					secret = true
					break
				}
			}
			if !secret || seen[key] {
				continue
			}
			seen[key] = true
			findings = append(findings, fmt.Sprintf("%s %s on line %d bakes a credential into the image — `docker history` prints it to anyone who can pull, and removing the line later does not remove the layer; pass it at run time (`-e %s=…`, or your orchestrator's secret) or, if the build itself needs it, with `RUN --mount=type=secret`", in.verb, key, in.line, key))
		}
	}

	for _, in := range global {
		inspect(in)
	}
	for _, stage := range stages {
		for _, in := range stage.instrs {
			inspect(in)
		}
	}
	return findings
}
