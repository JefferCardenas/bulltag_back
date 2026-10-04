# Etapa 1: compilar. La imagen de Go es grande, pero solo se usa para construir.
# Se usa la última versión de Go porque las versiones recientes de pgx
# exigen un Go reciente (la 5.9.x pide Go 1.25 o superior).
FROM golang:alpine AS build
WORKDIR /src
COPY . .
# "go mod tidy" descarga las dependencias (pgx) y genera go.sum.
# Se compilan dos programas: el servicio y el simulador de vacas.
RUN go mod tidy \
 && CGO_ENABLED=0 go build -o /ingest . \
 && CGO_ENABLED=0 go build -o /simulator ./cmd/simulator

# Etapa 2: imagen final mínima, solo con los binarios.
FROM gcr.io/distroless/static-debian12
COPY --from=build /ingest /ingest
COPY --from=build /simulator /simulator
EXPOSE 8080
ENTRYPOINT ["/ingest"]
