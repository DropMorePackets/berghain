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

# Stage 3: Final image
FROM haproxy:lts-bookworm
WORKDIR /app
COPY --from=frontend-builder /app/web/dist ./web/dist
COPY --from=backend-builder /app/berghain ./berghain
COPY examples/haproxy/haproxy.cfg ./haproxy.cfg
COPY cmd/spop/config.yaml ./config.yaml

CMD ["sh", "-c", "haproxy -f haproxy.cfg & ./berghain -config config.yaml"]