// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

// Package plugins is the executable plugin contract of Nucleus, both sides
// of it.
//
// A capability plugin is an executable named `nucleus-plugin-<provider>`.
// Asked `capabilities` (or `capabilities --json`), it lists the
// capabilities it serves (`mail.send`, `queue.publish`, `webhook.deliver`,
// or any other `domain.action`); run with no arguments, it reads one
// RequestEnvelope from stdin, writes one ResponseEnvelope to stdout and
// exits with one of the ExitCode* codes.
//
// The host side — what Nucleus runs — is DiscoverExternal and
// DiscoverAllowed, ProbeCapabilities and ExecuteRequest, behind the Host
// interface; a Policy decides which executables may run at all.
//
// The plugin side — what a plugin author writes — is Serve: a Plugin with
// one typed handler per capability, and Serve speaks the envelope and the
// exit codes for it. A complete plugin built on it, tested through the real
// runtime, is internal/fixtures/plugins/nucleus-plugin-maildir in the
// Nucleus repository. The contract itself is docs/reference/PLUGIN_SDK.md.
package plugins
