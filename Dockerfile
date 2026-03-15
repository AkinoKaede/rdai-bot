FROM --platform=$TARGETPLATFORM golang:1.26-alpine AS build

RUN apk add --no-cache build-base

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=1 go build -o /out/rdai-bot .

FROM --platform=$TARGETPLATFORM alpine:3.21

RUN apk add --no-cache ca-certificates

WORKDIR /app

COPY --from=build /out/rdai-bot /usr/local/bin/rdai-bot

EXPOSE 8080
ENV SQLITE_PATH=/data/rdai-bot.db
VOLUME ["/data"]

ENTRYPOINT ["/usr/local/bin/rdai-bot"]
