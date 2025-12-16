# Build stage
FROM golang:1.24-alpine AS build

WORKDIR /app
RUN apk add --no-cache git

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN go build -o game-api ./cmd/game-api

# Runtime stage
FROM alpine:3.20

WORKDIR /app
RUN apk add --no-cache ca-certificates

COPY --from=build /app/game-api /usr/local/bin/game-api

EXPOSE 8081
ENV APP_PORT=8081

CMD ["game-api"]
