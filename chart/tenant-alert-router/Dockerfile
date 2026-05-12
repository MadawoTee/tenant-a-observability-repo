# --- build ---
FROM golang:1.22-alpine AS build
WORKDIR /src
COPY go.mod ./
# No external deps yet; if you add some, also COPY go.sum and run `go mod download`.
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/router .
 
# --- runtime ---
# distroless static — no shell, no package manager, runs as nonroot (UID 65532)
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/router /router
EXPOSE 8080
USER 65532:65532
ENTRYPOINT ["/router"]