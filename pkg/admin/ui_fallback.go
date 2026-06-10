package admin

import (
	"embed"
	"fmt"
	"html"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const adminUIDirEnv = "NUCLEUS_ADMIN_UI_DIR"

//go:embed ui_fallback/*
var fallbackUIFS embed.FS

func adminUIContentFS() fs.FS {
	if uiFS, ok := adminUIBuildFS(); ok {
		return uiFS
	}
	fsys, err := fs.Sub(fallbackUIFS, "ui_fallback")
	if err != nil {
		return os.DirFS(".")
	}
	return fsys
}

func adminUIBuildFS() (fs.FS, bool) {
	if dir := strings.TrimSpace(os.Getenv(adminUIDirEnv)); dir != "" {
		if adminUIBuildDirUsable(dir) {
			return os.DirFS(dir), true
		}
		return nil, false
	}
	for _, dir := range adminUIBuildDirCandidates() {
		if adminUIBuildDirUsable(dir) {
			return os.DirFS(dir), true
		}
	}
	return nil, false
}

func adminUIBuildDirCandidates() []string {
	cwd, err := os.Getwd()
	if err != nil || cwd == "" {
		return nil
	}

	seen := map[string]struct{}{}
	var dirs []string
	add := func(dir string) {
		cleaned := filepath.Clean(dir)
		if _, ok := seen[cleaned]; ok {
			return
		}
		seen[cleaned] = struct{}{}
		dirs = append(dirs, cleaned)
	}

	for dir := cwd; ; dir = filepath.Dir(dir) {
		add(filepath.Join(dir, "pkg", "admin", "ui", "dist"))
		add(filepath.Join(dir, "ui", "dist"))
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
	}
	return dirs
}

func adminUIBuildDirUsable(dir string) bool {
	if strings.TrimSpace(dir) == "" {
		return false
	}
	info, err := os.Stat(filepath.Join(dir, "index.html"))
	return err == nil && !info.IsDir()
}

// injectHeadMeta inserts a meta tag right after the document's opening
// <head>. When the document has no <head> (not the case for real SPA
// builds), a closed synthetic head is prepended so the output stays valid.
func injectHeadMeta(content []byte, name, value string) []byte {
	meta := fmt.Sprintf(`<meta name="%s" content="%s">`, html.EscapeString(name), html.EscapeString(value))
	contentStr := string(content)
	if strings.Contains(contentStr, "<head>") {
		return []byte(strings.Replace(contentStr, "<head>", "<head>"+meta, 1))
	}
	return []byte("<head>" + meta + "</head>\n" + contentStr)
}

func injectAdminPrefix(content []byte, prefix string) []byte {
	return injectHeadMeta(content, "nucleus-admin-prefix", NormalizePrefix(prefix))
}

// injectLoginMessage surfaces a login error/info message to the admin SPA as
// a meta tag — the same mechanism as the prefix injection — so the SPA login
// page can render feedback when a POST re-serves it (e.g. rejected
// credentials). Without it the SPA path silently dropped the message and a
// failed login was indistinguishable from "nothing happened". Empty messages
// inject nothing; an error wins over an info message.
func injectLoginMessage(content []byte, errorMsg, infoMsg string) []byte {
	name, msg := "nucleus-admin-login-info", infoMsg
	if errorMsg != "" {
		name, msg = "nucleus-admin-login-error", errorMsg
	}
	if msg == "" {
		return content
	}
	return injectHeadMeta(content, name, msg)
}
