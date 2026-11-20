# ParamLens em container — para quem não tem Go instalado.
#
# Rodar a interface (só autenticação SQL; Windows/SSPI não funciona em container Linux):
#   docker build -t paramlens .
#   docker run --rm -it -p 127.0.0.1:8765:8765 paramlens
#   -> abra o link "Interface:" que aparece no console.
#   Para o SQL Server da própria máquina use o servidor "host.docker.internal".
#
# Gerar o ParamLens.exe (Windows) sem Go instalado:
#   docker build --target export --output dist .

# ---- build: testes + binários --------------------------------------------
FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
RUN go vet ./... && go test ./...
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/paramlens . \
 && CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/ParamLens.exe .

# ---- export: só o .exe (docker build --target export --output dist .) -----
FROM scratch AS export
COPY --from=build /out/ParamLens.exe /

# ---- runtime ---------------------------------------------------------------
FROM alpine:3.20
RUN adduser -D -u 10001 paramlens
USER paramlens
COPY --from=build /out/paramlens /usr/local/bin/paramlens
EXPOSE 8765
# Escuta em 0.0.0.0 DENTRO do container; publique a porta apenas em 127.0.0.1 no host.
# A porta interna precisa ser igual à publicada (o guard valida o Host).
ENTRYPOINT ["paramlens", "-bind", "0.0.0.0", "-port", "8765", "-no-browser"]
