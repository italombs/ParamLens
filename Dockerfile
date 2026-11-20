# ParamLens in a container — for machines without Go.
#
# Run the UI (SQL authentication only; Windows/SSPI does not work in a Linux container):
#   docker build -t paramlens .
#   docker run --rm -it -p 127.0.0.1:8765:8765 paramlens
#   -> open the "Interface:" link printed on the console.
#   For the SQL Server running on the host, use "host.docker.internal" as the server.
#
# Build ParamLens.exe (Windows) and ParamLens.html (offline version) without Go installed:
#   docker build --target export --output dist .

# ---- build: tests + binaries ---------------------------------------------
FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
RUN go vet ./... && go test ./...
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/paramlens . \
 && CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/ParamLens.exe . \
 && /out/paramlens -write-html /out/ParamLens.html

# ---- export: .exe + HTML version (docker build --target export --output dist .) ----
FROM scratch AS export
COPY --from=build /out/ParamLens.exe /out/ParamLens.html /

# ---- runtime ---------------------------------------------------------------
FROM alpine:3.20
RUN adduser -D -u 10001 paramlens
USER paramlens
COPY --from=build /out/paramlens /usr/local/bin/paramlens
EXPOSE 8765
# Listens on 0.0.0.0 INSIDE the container; publish the port on 127.0.0.1 only.
# The inner port must match the published one (the guard checks Host).
ENTRYPOINT ["paramlens", "-bind", "0.0.0.0", "-port", "8765", "-no-browser"]
