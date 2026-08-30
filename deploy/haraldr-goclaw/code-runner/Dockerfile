FROM golang:1.26-bookworm AS build
WORKDIR /src
COPY deploy/haraldr-goclaw/code-runner/go.mod deploy/haraldr-goclaw/code-runner/main.go ./
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags='-s -w' -o /out/haraldr-code-runner .

FROM golang:1.26-bookworm AS deps
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

FROM golang:1.26-bookworm
COPY --from=build /out/haraldr-code-runner /usr/local/bin/haraldr-code-runner
COPY --from=deps /go/pkg/mod /opt/go/pkg/mod
USER 65532:65532
ENTRYPOINT ["/usr/local/bin/haraldr-code-runner"]
