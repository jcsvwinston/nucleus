module github.com/jcsvwinston/nucleus/admin/proto

go 1.26.3

// Real require blocks (connectrpc, google.golang.org/protobuf) are added by
// the proto generator in `make proto` and committed alongside the generated
// stubs. In Phase 1 the module only declares its identity.

require (
	connectrpc.com/connect v1.19.2
	google.golang.org/protobuf v1.36.11
)
