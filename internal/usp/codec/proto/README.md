# USP Protobuf Schemas

Vendored Broadband Forum TR-369 USP protobuf schemas. These are the wire-contract authority for every USP message cpe-labs produces or consumes.

## Source

Copied verbatim from herder-labs/herder (formerly ispx-ltd/OpenACS) at commit `e2f5e0388fdabf86c5a92647d7b48859bf396cbb`, path `backend/internal/usp/codec/proto/`.

Vendoring tracks the controller's version of the schema, not BBF upstream directly, so wire-compat lockstep with the reference controller is preserved if BBF revises the spec.

## What is rewritten vs verbatim

**Every line is byte-for-byte identical to upstream except `option go_package`.** Upstream points at two separate Go packages (`.../usp;usp` and `.../usp_record;usp_record`). cpe-labs consolidates both files into one combined Go package, `uspproto`, by rewriting both `option go_package` values to:

```
option go_package = "github.com/herder-labs/cpe-labs/internal/usp/codec/proto;uspproto";
```

This is the only allowed rewrite. Refresh runs (re-copying from upstream after a controller version bump) must re-apply this rewrite explicitly; nothing else may diverge.

## Regenerating Go bindings

```
make proto-gen
```

requires `protoc` (libprotoc 3.21+) and `protoc-gen-go` (`go install google.golang.org/protobuf/cmd/protoc-gen-go@latest`) on `PATH`. Generated `*.pb.go` files are committed to the repository; CI does not run `proto-gen`.

## Refresh procedure

1. Bump the commit SHA in this README to the new upstream revision.
2. Copy the two `.proto` files from `<upstream>/backend/internal/usp/codec/proto/` over the vendored copies.
3. Re-apply the `option go_package` rewrite to both files (the two `sed` lines from the spec, or by hand).
4. Run `make proto-gen`.
5. Run `go vet ./...` and `go test ./internal/usp/...` to confirm nothing downstream broke.
6. Commit the schemas + regenerated bindings + bumped SHA together.
