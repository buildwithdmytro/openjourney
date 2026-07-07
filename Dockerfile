FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/openjourney-api ./cmd/api \
 && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/openjourney-worker ./cmd/worker \
 && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/openjourney-dispatcher ./cmd/dispatcher \
 && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/openjourney-archive ./cmd/archive \
 && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/openjourney-analytics ./cmd/analytics \
 && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/openjourney-operations ./cmd/operations

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/openjourney-api /usr/local/bin/openjourney-api
COPY --from=build /out/openjourney-worker /usr/local/bin/openjourney-worker
COPY --from=build /out/openjourney-dispatcher /usr/local/bin/openjourney-dispatcher
COPY --from=build /out/openjourney-archive /usr/local/bin/openjourney-archive
COPY --from=build /out/openjourney-analytics /usr/local/bin/openjourney-analytics
COPY --from=build /out/openjourney-operations /usr/local/bin/openjourney-operations
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/openjourney-api"]
