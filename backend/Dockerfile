FROM golang:1.26 AS build

WORKDIR /src

# Pre-copy go.mod/go.sum to cache dependency downloads.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN go build -o /out/server ./cmd/server/main.go

FROM golang:1.26

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