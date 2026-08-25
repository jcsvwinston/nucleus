package nucleustest_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/app"
	"github.com/jcsvwinston/nucleus/pkg/nucleus"
	"github.com/jcsvwinston/nucleus/pkg/nucleustest"
)

type recorder struct {
	testing.TB
	logs []string
}

func (r *recorder) Logf(format string, args ...any) {
	r.logs = append(r.logs, fmt.Sprintf(format, args...))
}
func (r *recorder) Helper() {}

func TestStartApp_WarnsWhenEnvRedirectsDatabase(t *testing.T) {
	t.Setenv("NUCLEUS_DATABASES__DEFAULT__URL", "postgres://dev-machine/app")

	cfg := app.DefaultConfig()
	cfg.Databases = nucleustest.TempSQLite(t)
	rec := &recorder{TB: t}
	srv := nucleustest.StartApp(rec, nucleus.App{Config: cfg})
	t.Cleanup(srv.Stop)

	joined := strings.Join(rec.logs, "\n")
	if !strings.Contains(joined, "NUCLEUS_DATABASES__") {
		t.Errorf("the kit must say when the environment can redirect the test database, got: %q", joined)
	}
}
