.PHONY: dev test lint build run clean proto docker-up docker-down

# Local Development Commands
dev:
	air

run:
	go run cmd/fluxgraph/main.go

test:
	go test -v -race ./...

lint:
	golangci-lint run

build:
	mkdir -p bin
	go build -o bin/fluxgraph ./cmd/fluxgraph

clean:
	rm -rf bin/
	go clean

# Protobuf Code Generation
proto:
	protoc --go_out=. --go_opt=paths=source_relative \
	       --go-grpc_out=. --go-grpc_opt=paths=source_relative \
	       api/proto/a2a.proto

# Infrastructure Commands
docker-up:
	docker-compose up -d

docker-down:
	docker-compose down
