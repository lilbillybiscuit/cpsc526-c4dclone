
PROTO_DIR := proto
PROTO_FILES := $(wildcard $(PROTO_DIR)/*.proto)
GO_FILES := $(wildcard *.go)
PROTOC := protoc
GO := go


BIN_NAME := monitor
BIN_DIR := bin

.PHONY: all clean proto build


all: proto build


proto:
	@echo "Generating protobuf code..."
	@go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
	@go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
	@cd $(PROTO_DIR) && \
	$(PROTOC) --go_out=. \
		--go_opt=paths=source_relative \
		--go-grpc_out=. \
		--go-grpc_opt=paths=source_relative \
		*.proto


build: proto
	@echo "Building Go project..."
	@mkdir -p $(BIN_DIR)
	@$(GO) build -o $(BIN_DIR)/$(BIN_NAME) .


clean:
	@echo "Cleaning..."
	@rm -rf $(BIN_DIR)
	@rm -f $(PROTO_DIR)/*.pb.go
	@rm -f $(PROTO_DIR)/*_grpc.pb.go


run: build
	@./$(BIN_DIR)/$(BIN_NAME)


deps:
	@echo "Installing dependencies..."
	@$(GO) mod tidy
