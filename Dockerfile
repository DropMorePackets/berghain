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
RUN go build -o berghain ./cmd/spop \
    && go build -o feedupdater ./cmd/feedupdater

# Stage 3: Final image
FROM haproxy:lts-bookworm
WORKDIR /app
COPY --from=frontend-builder /app/web/dist ./web/dist
COPY --from=backend-builder /app/berghain ./berghain
COPY --from=backend-builder /app/feedupdater ./feedupdater
RUN mkdir -p ./examples/haproxy/state
COPY examples/haproxy/berghain.cfg ./examples/haproxy/berghain.cfg
COPY examples/haproxy/entrypoint.sh ./examples/haproxy/entrypoint.sh
COPY examples/haproxy/maps ./examples/haproxy/maps
COPY --chown=haproxy:haproxy examples/haproxy/state/reputation.map ./examples/haproxy/state/reputation.map
COPY examples/haproxy/haproxy.cfg ./haproxy.cfg
COPY cmd/spop/config.yaml ./config.yaml

CMD ["sh", "examples/haproxy/entrypoint.sh"]
