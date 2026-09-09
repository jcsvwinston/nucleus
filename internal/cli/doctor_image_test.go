// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

// `nucleus doctor --check image` reads the project's Dockerfile. Every case
// here is a file that BUILDS: valid syntax, a working image at the end of
// it, and a container nobody should put on a network. That is the line the
// check draws — `docker build` says whether the file works, this says
// whether what it produces is safe to run.
package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeProject writes a Dockerfile (and, unless empty, a .dockerignore and a
// config file) into a fresh directory and returns the config path, which is
// how the check is told where the project is.
func writeProject(t *testing.T, dockerfile string) string {
	t.Helper()
	dir := t.TempDir()
	if dockerfile != "" {
		if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte(dockerfile), 0o600); err != nil {
			t.Fatalf("write Dockerfile: %v", err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, ".dockerignore"), []byte(".git\n"), 0o600); err != nil {
		t.Fatalf("write .dockerignore: %v", err)
	}
	path := filepath.Join(dir, "nucleus.yml")
	if err := os.WriteFile(path, []byte("port: 8080\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

// hardened is the shape the scaffold writes: two stages, a digest-pinned
// runtime base, a static build and an explicit non-root user.
const hardened = `FROM golang:1.26.6-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/app .

FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab
WORKDIR /app
COPY --from=build /out/app /app/app
COPY nucleus.yml /app/nucleus.yml
USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["/app/app"]
`

func TestCheckImage_HardenedDockerfilePasses(t *testing.T) {
	out := checkImage(nil, writeProject(t, hardened))
	if out.status != doctorStatusPass {
		t.Fatalf("a hardened Dockerfile must pass, got %q (%s)", out.status, out.message)
	}
}

// A project that deploys as a plain binary is not a degraded project.
func TestCheckImage_NoDockerfileIsNotAFinding(t *testing.T) {
	out := checkImage(nil, writeProject(t, ""))
	if out.status != doctorStatusInfo {
		t.Fatalf("a project with no Dockerfile must report info, got %q (%s)", out.status, out.message)
	}
}

func TestCheckImage_Findings(t *testing.T) {
	for _, tc := range []struct {
		name       string
		dockerfile string
		wantStatus doctorStatus
		wantIn     string
	}{
		{
			name: "no USER means root",
			dockerfile: `FROM golang:1.26.6-alpine AS build
RUN CGO_ENABLED=0 go build -o /out/app .

FROM debian:12@sha256:aaaa
COPY --from=build /out/app /app/app
ENTRYPOINT ["/app/app"]
`,
			wantStatus: doctorStatusError,
			wantIn:     "runs as root",
		},
		{
			name: "USER root is root spelled out",
			dockerfile: `FROM golang:1.26.6-alpine AS build
RUN CGO_ENABLED=0 go build -o /out/app .

FROM debian:12@sha256:aaaa
COPY --from=build /out/app /app/app
USER root
ENTRYPOINT ["/app/app"]
`,
			wantStatus: doctorStatusError,
			wantIn:     `runs as "root"`,
		},
		{
			// The tag claims a non-root default, so the container is
			// probably not root — but the file does not say so, and a base
			// image bump can change it silently.
			name: "a nonroot base with no USER is softer than root",
			dockerfile: `FROM golang:1.26.6-alpine AS build
RUN CGO_ENABLED=0 go build -o /out/app .

FROM gcr.io/distroless/static-debian12:nonroot@sha256:aaaa
COPY --from=build /out/app /app/app
ENTRYPOINT ["/app/app"]
`,
			wantStatus: doctorStatusWarning,
			wantIn:     "declares no USER and inherits it",
		},
		{
			name: "a mutable runtime tag is not the reviewed image",
			dockerfile: `FROM golang:1.26.6-alpine AS build
RUN CGO_ENABLED=0 go build -o /out/app .

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/app /app/app
USER 65532:65532
`,
			wantStatus: doctorStatusWarning,
			wantIn:     "is a mutable tag",
		},
		{
			name: "cgo left on does not run on a base without libc",
			dockerfile: `FROM golang:1.26.6-alpine AS build
RUN go build -trimpath -o /out/app .

FROM gcr.io/distroless/static-debian12:nonroot@sha256:aaaa
COPY --from=build /out/app /app/app
USER 65532:65532
`,
			wantStatus: doctorStatusWarning,
			wantIn:     "does not set CGO_ENABLED",
		},
		{
			name: "CGO_ENABLED=1 is named for what it is",
			dockerfile: `FROM golang:1.26.6-alpine AS build
ENV CGO_ENABLED=1
RUN go build -o /out/app .

FROM gcr.io/distroless/static-debian12:nonroot@sha256:aaaa
COPY --from=build /out/app /app/app
USER 65532:65532
`,
			wantStatus: doctorStatusWarning,
			wantIn:     "CGO_ENABLED=1",
		},
		{
			name: "a single stage ships the toolchain and the source",
			dockerfile: `FROM golang:1.26.6-alpine@sha256:aaaa
WORKDIR /src
COPY . .
RUN CGO_ENABLED=0 go build -o /app/app .
USER 65532:65532
ENTRYPOINT ["/app/app"]
`,
			wantStatus: doctorStatusWarning,
			wantIn:     "single-stage build",
		},
		{
			name: "the runtime stage copying the context ships the source",
			dockerfile: `FROM golang:1.26.6-alpine AS build
RUN CGO_ENABLED=0 go build -o /out/app .

FROM gcr.io/distroless/static-debian12:nonroot@sha256:aaaa
COPY --from=build /out/app /app/app
COPY . /app
USER 65532:65532
`,
			wantStatus: doctorStatusError,
			wantIn:     "copies the whole build context",
		},
		{
			name: "a credential in ENV is in the layer forever",
			dockerfile: `FROM golang:1.26.6-alpine AS build
RUN CGO_ENABLED=0 go build -o /out/app .

FROM gcr.io/distroless/static-debian12:nonroot@sha256:aaaa
ENV NUCLEUS_JWT_SECRET=K7f2Qx9LmZp4Rw8TvN3aHy6BdG1sJc5E
COPY --from=build /out/app /app/app
USER 65532:65532
`,
			wantStatus: doctorStatusError,
			wantIn:     "bakes a credential into the image",
		},
		{
			name: "an ARG default is in the build history too",
			dockerfile: `ARG REGISTRY_TOKEN=ghp_realtokenvalue
FROM golang:1.26.6-alpine AS build
RUN CGO_ENABLED=0 go build -o /out/app .

FROM gcr.io/distroless/static-debian12:nonroot@sha256:aaaa
COPY --from=build /out/app /app/app
USER 65532:65532
`,
			wantStatus: doctorStatusError,
			wantIn:     "REGISTRY_TOKEN",
		},
		{
			// Naming where a value comes from is the fix, not the defect.
			name: "a variable reference is not a baked secret",
			dockerfile: `FROM golang:1.26.6-alpine AS build
RUN CGO_ENABLED=0 go build -o /out/app .

FROM gcr.io/distroless/static-debian12:nonroot@sha256:aaaa
ARG DB_PASSWORD
ENV NUCLEUS_DB_PASSWORD=$DB_PASSWORD
COPY --from=build /out/app /app/app
USER 65532:65532
`,
			wantStatus: doctorStatusPass,
			wantIn:     "no literal secrets",
		},
		{
			// A continuation is one instruction, and the check has to read
			// it as one or it misses the CGO_ENABLED in front of the build.
			name: "a continued RUN is read as one line",
			dockerfile: `FROM golang:1.26.6-alpine AS build
RUN CGO_ENABLED=0 \
    go build \
        -trimpath \
        -o /out/app .

FROM gcr.io/distroless/static-debian12:nonroot@sha256:aaaa
COPY --from=build /out/app /app/app
USER 65532:65532
`,
			wantStatus: doctorStatusPass,
			wantIn:     "2-stage build",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := checkImage(nil, writeProject(t, tc.dockerfile))
			if out.status != tc.wantStatus {
				t.Fatalf("status = %s (%s), want %s", out.status, out.message, tc.wantStatus)
			}
			if !strings.Contains(out.message, tc.wantIn) {
				t.Errorf("message = %q, want it to contain %q", out.message, tc.wantIn)
			}
		})
	}
}

// A missing .dockerignore is judged from the directory, not from the file.
func TestCheckImage_MissingDockerignoreIsAFinding(t *testing.T) {
	configPath := writeProject(t, hardened)
	if err := os.Remove(filepath.Join(filepath.Dir(configPath), ".dockerignore")); err != nil {
		t.Fatal(err)
	}
	out := checkImage(nil, configPath)
	if out.status != doctorStatusWarning {
		t.Fatalf("status = %s (%s), want warning", out.status, out.message)
	}
	if !strings.Contains(out.message, ".dockerignore") {
		t.Errorf("message must name the missing file, got %q", out.message)
	}
}

// TestDoctorCheckImage_OnGeneratedScaffold is the one that matters: the
// Dockerfile `nucleus new` writes must come out clean, through the command
// itself, in both renderings. A scaffold the project's own check complains
// about would teach everyone who runs it to ignore the check.
func TestDoctorCheckImage_OnGeneratedScaffold(t *testing.T) {
	for _, tmpl := range []string{"api", "mvc", "suite"} {
		t.Run(tmpl, func(t *testing.T) {
			outDir := t.TempDir()
			var stdout, stderr bytes.Buffer
			args := []string{"imagecheck", "--out", outDir, "--offline", "--template", tmpl}
			if err := runNew(args, strings.NewReader(""), &stdout, &stderr); err != nil {
				t.Fatalf("runNew(%s): %v\nstderr: %s", tmpl, err, stderr.String())
			}
			projectDir := filepath.Join(outDir, "imagecheck")
			for _, name := range []string{"Dockerfile", ".dockerignore"} {
				if _, err := os.Stat(filepath.Join(projectDir, name)); err != nil {
					t.Fatalf("%s: the scaffold must write %s: %v", tmpl, name, err)
				}
			}
			configPath := filepath.Join(projectDir, "nucleus.yml")

			var textOut, textErr bytes.Buffer
			if err := runDoctor([]string{"--check", "image", "--config", configPath, "--verbose"}, strings.NewReader(""), &textOut, &textErr); err != nil {
				t.Fatalf("%s: doctor --check image on a fresh scaffold: %v\n%s", tmpl, err, textOut.String())
			}
			if !strings.Contains(textOut.String(), "✓ image") {
				t.Errorf("%s: the check must report a pass, got:\n%s", tmpl, textOut.String())
			}

			var jsonOut, jsonErr bytes.Buffer
			if err := runDoctor([]string{"--check", "image", "--config", configPath, "--json"}, strings.NewReader(""), &jsonOut, &jsonErr); err != nil {
				t.Fatalf("%s: doctor --check image --json on a fresh scaffold: %v\n%s", tmpl, err, jsonOut.String())
			}
			var report doctorReport
			if err := json.Unmarshal(jsonOut.Bytes(), &report); err != nil {
				t.Fatalf("%s: decode doctor report: %v\n%s", tmpl, err, jsonOut.String())
			}
			if report.OverallStatus != "healthy" || len(report.Results) != 1 || report.Results[0].Status != string(doctorStatusPass) {
				t.Errorf("%s: JSON report must carry one passing image result, got %+v", tmpl, report)
			}

			// The generated file is the one the docs describe: a static
			// build on a digest-pinned distroless base, run as 65532.
			body := readFile(t, filepath.Join(projectDir, "Dockerfile"))
			for _, want := range []string{"CGO_ENABLED=0", "-trimpath", "@sha256:", "USER 65532:65532", "gcr.io/distroless/static-debian12"} {
				if !strings.Contains(body, want) {
					t.Errorf("%s: generated Dockerfile is missing %q:\n%s", tmpl, want, body)
				}
			}
			// The mvc and suite templates boot a default-deny enforcer that
			// reads rbac_policy.csv; an image without it does not start.
			wantsPolicy := tmpl != "api"
			if got := strings.Contains(body, "COPY rbac_policy.csv"); got != wantsPolicy {
				t.Errorf("%s: generated Dockerfile copies rbac_policy.csv = %v, want %v:\n%s", tmpl, got, wantsPolicy, body)
			}
		})
	}
}

// An unknown --check name still fails, so a typo is not silently a no-op.
func TestDoctorCheckImage_IsRegistered(t *testing.T) {
	var out, errOut bytes.Buffer
	if err := runDoctor([]string{"--check", "image", "--json"}, strings.NewReader(""), &out, &errOut); err != nil {
		t.Fatalf("doctor --check image must be a known check: %v", err)
	}
	var report doctorReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("decode doctor report: %v\n%s", err, out.String())
	}
	if report.TotalChecks != 1 || report.Results[0].Name != "image" {
		t.Fatalf("doctor --check image must run exactly the image check, got %+v", report)
	}
}
