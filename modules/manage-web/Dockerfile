FROM golang:1.27.1-alpine AS build

WORKDIR /src
COPY go.mod ./
COPY cmd ./cmd
COPY internal ./internal
COPY web ./web

RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/manafield-manage-web-web ./cmd/manafield-manage-web-web

FROM scratch

COPY --from=build /out/manafield-manage-web /manafield-manage-web

USER 65532:65532
EXPOSE 8080

ENTRYPOINT ["/manafield-manage-web"]
