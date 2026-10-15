# syntax=docker/dockerfile:1
# cactusInv: one distroless, non-root image serving the API, the web app and the workers.

FROM node:20-alpine AS web
WORKDIR /src/web
RUN corepack enable && corepack prepare pnpm@9.15.9 --activate
COPY web/package.json web/pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile
COPY web/ ./
RUN pnpm build

FROM golang:1.26-alpine AS build
WORKDIR /src
ENV CGO_ENABLED=0 GOFLAGS=-trimpath
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/web/dist ./web/dist
ARG VERSION=dev
ARG BUILD=0
RUN go build -ldflags "-s -w -X main.version=${VERSION} -X main.build=${BUILD}" -o /out/cactusinv ./cmd/cactusinv \
 && mkdir -p /out/data/invoices /out/data/backups

FROM gcr.io/distroless/static-debian13:nonroot
COPY --from=build /out/cactusinv /cactusinv
COPY --from=build --chown=65532:65532 /out/data /data
USER 65532:65532
ENV CACTUSINV_INVOICES_DIR=/data/invoices CACTUSINV_BACKUP_DIR=/data/backups
EXPOSE 8080
VOLUME ["/data/invoices", "/data/backups"]
HEALTHCHECK --interval=30s --timeout=5s --start-period=60s CMD ["/cactusinv", "healthcheck"]
ENTRYPOINT ["/cactusinv"]
CMD ["serve"]
