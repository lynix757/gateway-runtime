# syntax=docker/dockerfile:1.7

FROM golang:1.27.1-bookworm AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOMAXPROCS=2 go build -p=1 -trimpath -ldflags="-s -w" -o /out/gateway-runtime ./cmd/gateway-runtime

FROM scratch
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /out/gateway-runtime /gateway-runtime

USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["/gateway-runtime"]
