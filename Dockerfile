FROM golang:1.26.4-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN mkdir /data
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /jobtracker ./cmd/jobtracker
FROM alpine:3.23
RUN apk add --no-cache poppler-utils ca-certificates
COPY --from=build /jobtracker /jobtracker
COPY --from=build --chown=65532:65532 /data /data
ENV LISTEN_ADDR=:8080 DATA_DIR=/data
USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["/jobtracker"]
