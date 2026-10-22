FROM node:22-alpine AS ui
WORKDIR /ui
COPY ui/package.json ui/package-lock.json ./
RUN npm ci
COPY ui/ ./
RUN npm run build

FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=ui /ui/dist ./ui/dist
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/nori .

FROM scratch
COPY --from=build /out/nori /nori
EXPOSE 8080
ENTRYPOINT ["/nori"]
CMD ["-data", "/data/nori", "-backup-dir", "/data/backups", "-backup-every", "1h"]
