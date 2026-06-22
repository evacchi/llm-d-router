FROM golang:1.26.5 as builder

WORKDIR /workspace

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -a -o wasm-epp ./examples/wasmplugin/cmd/main.go

FROM gcr.io/distroless/static:nonroot
WORKDIR /
COPY --from=builder /workspace/wasm-epp .
USER 65532:65532

ENTRYPOINT ["/wasm-epp"]
