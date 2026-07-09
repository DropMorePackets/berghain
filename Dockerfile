# Use a multi-stage build to separate frontend and backend builds

# Stage 1: Build the frontend
FROM node:18 AS frontend-builder
WORKDIR /app/web
COPY web/package.json web/package-lock.json ./
RUN npm install
COPY web/ ./
RUN npm run build

# Stage 2: Build the backend
FROM golang:1.24 AS backend-builder
WORKDIR /app
COPY . .
RUN go build -o berghain ./cmd/spop
RUN go build -o feedupdater ./cmd/feedupdater

# Stage 3: Final image
FROM haproxy:lts-bookworm
WORKDIR /app
COPY --from=frontend-builder /app/web/dist ./web/dist
COPY --from=backend-builder /app/berghain ./berghain
COPY --from=backend-builder /app/feedupdater ./feedupdater
# The whole example haproxy tree (cfg + maps + errors) so the image is self-contained.
COPY examples/haproxy/ ./examples/haproxy/
COPY examples/haproxy/haproxy.cfg ./haproxy.cfg
COPY cmd/spop/config.yaml ./config.yaml

# -L names HAProxy's local peer so it matches the `peer haproxy_local` entry.
# feedupdater serves live IP reputation to HAProxy over the peers protocol.
CMD ["sh", "-c", "haproxy -f haproxy.cfg -L haproxy_local & ./feedupdater -peer-listen 127.0.0.1:10001 -maps-dir examples/haproxy/maps -interval 6h & ./berghain -config config.yaml"]