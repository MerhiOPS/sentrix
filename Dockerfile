# Sentrix server image. The default command is the demo: a seeded tenant and
# simulated hosts shipping through the real ingest path.
#   docker build -t sentrix .
#   docker run --rm -p 8080:8080 sentrix        # then open http://localhost:8080
# For real use, mount a volume and drop --demo:
#   docker run -v sentrix:/data -p 8443:8443 -p 127.0.0.1:8080:8080 sentrix \
#     serve --data /data --listen :8443 --api-listen 0.0.0.0:8080 --api-tls

FROM node:22-alpine AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY web/ ./
RUN npm run build

FROM golang:1.26 AS build
ARG VERSION=docker
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/web/dist ./web/dist
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/sentrix-server ./cmd/sentrix-server \
 && mkdir -p /out/data

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/sentrix-server /usr/bin/sentrix-server
COPY --from=build --chown=65532:65532 /out/data /data
VOLUME /data
EXPOSE 8443 8080
ENTRYPOINT ["/usr/bin/sentrix-server"]
CMD ["serve", "--data", "/data", "--demo", "--listen", ":8443", "--api-listen", "0.0.0.0:8080"]
