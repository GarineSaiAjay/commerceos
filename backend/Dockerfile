FROM golang:1.26 AS build

WORKDIR /src

# Pre-copy go.mod/go.sum to cache dependency downloads.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
# CGO_ENABLED=0: the runtime stage below is a minimal distroless image
# with no C library at all, so the binary must be fully static. None of
# this project's dependencies (pgx, go-redis, razorpay-go) need cgo, so
# this was already effectively true -- this makes it explicit and
# enforced at build time instead of relying on that staying true.
RUN CGO_ENABLED=0 go build -o /out/server ./cmd/server/main.go

# Previously FROM golang:1.26 for the runtime stage too -- shipping the
# entire ~800MB Go SDK (compiler, stdlib source, build cache, package
# manager) in the runtime image for a program that is just one static
# binary. distroless/static is ~2MB and ships nothing but CA
# certificates and tzdata: no shell, no compiler, no package manager --
# which also shrinks the attack surface for a container that handles
# Razorpay payment webhooks. CA certs are required here: the backend
# makes outbound HTTPS calls to the Razorpay API and, optionally, an
# OpenRouter-compatible LLM endpoint. ":nonroot" runs the server as an
# unprivileged UID (65532) rather than root.
FROM gcr.io/distroless/static-debian12:nonroot

WORKDIR /app
COPY --from=build /out/server ./server

ENV DATABASE_URL=""
ENV REDIS_ADDR="localhost:6379"
ENV API_GATEWAY_PORT="8080"
ENV COMMERCE_PORT="8081"
ENV AGENT_API_PORT="8082"
ENV DASHBOARD_PORT="8083"

EXPOSE 8080 8081 8082 8083

CMD ["/app/server"]
