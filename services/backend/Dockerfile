FROM golang:1.26.1-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/api ./cmd/api \
 && CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/worker ./cmd/worker \
 && CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/scheduler ./cmd/scheduler \
 && CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/migrate ./cmd/migrate
RUN mkdir -p /data/objects && chown -R 65532:65532 /data
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build --chown=65532:65532 /data/ /data/
COPY --from=build /out/ /app/
COPY --from=build /usr/local/go/lib/time/zoneinfo.zip /zoneinfo.zip
ENV ZONEINFO=/zoneinfo.zip PORT=8080
EXPOSE 8080
CMD ["/app/api"]
