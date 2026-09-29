# Backend
# Regenerate the OpenAPI/Swagger docs (docs/docs.go, docs/swagger.json,
# docs/swagger.yaml) from the swaggo ("// @...") annotations on customer HTTP handlers.
# System-admin handlers must not carry Swagger annotations: these files are public.
#
# Requires swag at the version pinned in go.mod:
#   go install github.com/swaggo/swag/cmd/swag@v1.16.6
.PHONY: docs
docs:
	swag init -g cmd/server/main.go -o docs --templateDelims "[[,]]"
	go test ./docs

.PHONY: test
test:
	go test ./...

.PHONY: build
build:
	go build ./...
