FROM golang:1.17 as builder
WORKDIR /workspace
COPY go.mod go.mod
COPY go.sum go.sum
RUN go mod download
COPY *.go .
RUN CGO_ENABLED=0 go build -o whenis .

FROM gcr.io/distroless/static:nonroot
WORKDIR /
COPY --from=builder /workspace/whenis .
USER 65532:65532

ENTRYPOINT ["/whenis"]
